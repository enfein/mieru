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

	"github.com/enfein/mieru/pkg/log"
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
}

func TestRollUp(t *testing.T) {
	log.SetOutputToTest(t)
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
	t.Logf("Length of history is %d", len(c.history))
	if c.value != total {
		t.Fatalf("Got counter value %d, want %d", c.value, total)
	}
	var sum int64
	for _, r := range c.history {
		sum += r.delta
	}
	if sum != total {
		t.Errorf("History sum up to %d, want %d", sum, total)
	}
}
