// Copyright (C) 2023  mieru authors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package metrics

import (
	mrand "math/rand"
	"testing"
	"time"
)

func TestCounter(t *testing.T) {
	c := &Counter{name: "counter", timeSeries: true}

	if c.Name() != "counter" {
		t.Errorf("Name() = %v, want %v", c.Name(), "counter")
	}
	if c.Type() != COUNTER_TIME_SERIES {
		t.Errorf("Type() = %v, want %v", c.Type(), COUNTER_TIME_SERIES)
	}

	c.addWithTime(10, time.Date(2012, time.January, 1, 0, 0, 0, 0, time.UTC))
	c.addWithTime(10, time.Date(2018, time.January, 1, 0, 0, 0, 0, time.UTC))
	c.addWithTime(10, time.Date(2022, time.January, 1, 0, 0, 0, 0, time.UTC))
	if c.Load() != 30 {
		t.Errorf("Load() = %v, want %v", c.Load(), 30)
	}
	testcases := []struct {
		t1    time.Time
		t2    time.Time
		value int64
	}{
		{
			time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC),
			0,
		},
		{
			time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2021, time.January, 1, 0, 0, 0, 0, time.UTC),
			0,
		},
		{
			time.Date(2010, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC),
			20,
		},
		{
			time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC),
			10,
		},
	}
	for _, tc := range testcases {
		value := c.DeltaBetween(tc.t1, tc.t2)
		if value != tc.value {
			t.Errorf("DeltaBetween() = %v, want %v", value, tc.value)
		}
	}
	lastUpdateTime := time.Date(2022, time.January, 1, 0, 0, 0, 0, time.UTC)
	if c.LastUpdateTime().Unix() != lastUpdateTime.Unix() {
		t.Errorf("LastUpdateTime() = %v, want %v", c.LastUpdateTime(), lastUpdateTime)
	}
}

func TestRollUp(t *testing.T) {
	c := &Counter{name: "counter", timeSeries: true}
	timestamps := make([]int64, 1000000)
	now := time.Now().UnixMilli()
	for i := 1000000 - 1; i >= 0; i-- {
		now -= mrand.Int63n(1000)
		timestamps[i] = now
	}
	var total int64
	for i := 0; i < 1000000; i++ {
		v := mrand.Int63n(1000)
		c.addWithTime(v, time.UnixMilli(timestamps[i]))
		total += v
	}
	if c.Load() != total {
		t.Fatalf("Got counter value %d, want %d", c.Load(), total)
	}
	var sum int64
	for i, h := range c.history {
		t.Logf("Length of history tier %d is %d", i, len(h))
		for _, b := range h {
			sum += b.delta
		}
	}
	if sum != total {
		t.Errorf("History sum up to %d, want %d", sum, total)
	}
}

func TestCompact(t *testing.T) {
	base := time.Date(2024, time.January, 10, 0, 0, 0, 0, time.UTC)
	testcases := []struct {
		name string
		at   time.Duration // add time relative to base
		now  time.Duration // compaction time relative to base
		tier int
		want time.Duration // bucket start relative to base
	}{
		{"second", 1500 * time.Millisecond, 2 * time.Second, 0, time.Second},
		{"second retained", 1500 * time.Millisecond, 121 * time.Second, 0, time.Second},
		{"to minute", 61500 * time.Millisecond, 182 * time.Second, 1, time.Minute},
		{"to hour", 61 * time.Minute, 182 * time.Minute, 2, time.Hour},
		{"to day", 25 * time.Hour, 10 * 24 * time.Hour, 3, 24 * time.Hour},
		{"previous day", -1500 * time.Millisecond, 0, 0, -2 * time.Second},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Counter{name: "counter", timeSeries: true}
			c.addWithTime(7, base.Add(tc.at))
			c.compact(base.Add(tc.now).UnixMilli())
			for i, h := range c.history {
				if i != tc.tier && len(h) != 0 {
					t.Fatalf("tier %d has %d buckets, want 0", i, len(h))
				}
			}
			h := c.history[tc.tier]
			want := bucket{start: base.Add(tc.want).UnixMilli(), delta: 7}
			if len(h) != 1 || h[0] != want {
				t.Errorf("tier %d = %v, want [%v]", tc.tier, h, want)
			}
		})
	}
}

func TestHistoryIsBounded(t *testing.T) {
	c := &Counter{name: "counter", timeSeries: true}
	start := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	const days = 10
	for ts := start; ts.Before(start.Add(days * 24 * time.Hour)); ts = ts.Add(5 * time.Second) {
		c.addWithTime(1, ts)
	}
	end := start.Add(days * 24 * time.Hour)
	limits := []int{121 + 1, 121 + 1, 8*24 + 1, days + 1}
	var sum int64
	for i, h := range c.history {
		if len(h) > limits[i] {
			t.Errorf("tier %d has %d buckets, want <= %d", i, len(h), limits[i])
		}
		for j := 1; j < len(h); j++ {
			if h[j].start <= h[j-1].start {
				t.Fatalf("tier %d is not ordered at %d", i, j)
			}
		}
		for _, b := range h {
			sum += b.delta
		}
	}
	if sum != c.Load() {
		t.Errorf("History sum up to %d, want %d", sum, c.Load())
	}
	// 1 update per 5 seconds. The window can miss at most one hour bucket.
	got := c.DeltaBetween(end.Add(-24*time.Hour), end)
	if want := int64(24 * 720); got > want || got < want-720 {
		t.Errorf("DeltaBetween() = %d, want within [%d, %d]", got, want-720, want)
	}
}

func BenchmarkCounterAdd(b *testing.B) {
	c := &Counter{name: "counter"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c.Add(100)
	}
}

func BenchmarkCounterAddTimeSeries(b *testing.B) {
	c := &Counter{name: "counter", timeSeries: true}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c.Add(100)
	}
}

func BenchmarkCounterAddTimeSeriesParallel(b *testing.B) {
	c := &Counter{name: "counter", timeSeries: true}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Add(100)
		}
	})
}
