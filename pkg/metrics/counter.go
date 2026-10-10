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
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/enfein/mieru/v3/pkg/mathext"
	pb "github.com/enfein/mieru/v3/pkg/metrics/metricspb"
	"google.golang.org/protobuf/proto"
)

// compactIntervalMs is the minimum milliseconds between two history compactions.
const compactIntervalMs = 1000

// tiers defines the resolution of time series history, from the finest
// to the coarsest. A bucket older than retain is merged into the next tier.
var tiers = [...]struct {
	width  int64 // bucket width in milliseconds
	retain int64 // in milliseconds; not used by the last tier
	label  pb.RollUpLabel
}{
	{time.Second.Milliseconds(), (120 * time.Second).Milliseconds(), pb.RollUpLabel_ROLL_UP_TO_SECOND},
	{time.Minute.Milliseconds(), (120 * time.Minute).Milliseconds(), pb.RollUpLabel_ROLL_UP_TO_MINUTE},
	// Provide accurate hourly data for at least 7 days.
	{time.Hour.Milliseconds(), (8 * 24 * time.Hour).Milliseconds(), pb.RollUpLabel_ROLL_UP_TO_HOUR},
	{(24 * time.Hour).Milliseconds(), 0, pb.RollUpLabel_ROLL_UP_TO_DAY},
}

// bucket is the sum of deltas in [start, start + tier width).
type bucket struct {
	start int64 // unix milli
	delta int64
}

// Counter holds a named int64 value that can't decrease.
//
// If time series is enabled, user can query history updates
// in a given time range.
type Counter struct {
	name  string
	value atomic.Int64

	timeSeries bool

	// mu protects the fields below. A time series counter also
	// updates value while holding mu, so value matches history.
	mu          sync.Mutex
	history     [len(tiers)][]bucket // each tier is ordered by start
	lastUpdate  int64                // unix milli
	nextCompact int64                // unix milli
}

var _ Metric = &Counter{}

func (c *Counter) Name() string {
	return c.name
}

func (c *Counter) Type() MetricType {
	if c.timeSeries {
		return COUNTER_TIME_SERIES
	} else {
		return COUNTER
	}
}

func (c *Counter) Add(delta int64) int64 {
	if delta < 0 {
		panic("Can't add a negative value to Counter")
	}
	if !c.timeSeries {
		return c.value.Add(delta)
	}
	return c.addWithTime(delta, time.Now())
}

func (c *Counter) Load() int64 {
	return c.value.Load()
}

func (c *Counter) Store(val int64) {
	panic("Store() is not supported by Counter")
}

// DeltaBetween returns the increment between t1 and t2.
func (c *Counter) DeltaBetween(t1, t2 time.Time) int64 {
	if t2.Before(t1) {
		panic("t2 must be later than t1")
	}
	if !c.timeSeries {
		panic(fmt.Sprintf("%s is not a time series Counter", c.name))
	}
	from, to := t1.UnixMilli(), t2.UnixMilli()
	c.mu.Lock()
	defer c.mu.Unlock()

	var sum int64
	for _, h := range c.history {
		i := sort.Search(len(h), func(i int) bool { return h[i].start > from })
		for ; i < len(h) && h[i].start <= to; i++ {
			sum += h[i].delta
		}
	}
	return sum
}

// LastUpdateTime returns the last time when the history is updated.
func (c *Counter) LastUpdateTime() time.Time {
	if !c.timeSeries {
		panic(fmt.Sprintf("%s is not a time series Counter", c.name))
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.lastUpdate == 0 {
		return time.Time{}
	}
	return time.UnixMilli(c.lastUpdate)
}

func (c *Counter) addWithTime(delta int64, t time.Time) int64 {
	if delta == 0 {
		return c.value.Load()
	}
	if !c.timeSeries {
		return c.value.Add(delta)
	}
	ms := t.UnixMilli()
	c.mu.Lock()
	defer c.mu.Unlock()

	c.record(0, ms, delta)
	c.lastUpdate = mathext.Max(c.lastUpdate, ms)
	if ms >= c.nextCompact {
		c.compact(ms)
		c.nextCompact = ms + compactIntervalMs
	}
	return c.value.Add(delta)
}

// historyPB returns the value and a time ordered copy of the history.
func (c *Counter) historyPB() (int64, []*pb.History) {
	c.mu.Lock()
	defer c.mu.Unlock()

	n := 0
	for _, h := range c.history {
		n += len(h)
	}
	records := make([]*pb.History, 0, n)
	for i := len(tiers) - 1; i >= 0; i-- {
		for _, b := range c.history[i] {
			records = append(records, &pb.History{
				TimeUnixMilli: proto.Int64(b.start),
				Delta:         proto.Int64(b.delta),
				RollUp:        tiers[i].label.Enum(),
			})
		}
	}
	return c.value.Load(), records
}

// loadHistory replaces the history with the protobuf records.
// NO_ROLL_UP records are merged into the finest tier.
func (c *Counter) loadHistory(records []*pb.History) {
	var loaded [len(tiers)][]bucket
	for _, r := range records {
		i := 0
		for j := range tiers {
			if tiers[j].label == r.GetRollUp() {
				i = j
				break
			}
		}
		loaded[i] = append(loaded[i], bucket{start: r.GetTimeUnixMilli(), delta: r.GetDelta()})
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.history = [len(tiers)][]bucket{}
	c.lastUpdate = 0
	c.nextCompact = 0
	for i, h := range loaded {
		sort.SliceStable(h, func(a, b int) bool { return h[a].start < h[b].start })
		for _, b := range h {
			c.record(i, b.start, b.delta)
			c.lastUpdate = mathext.Max(c.lastUpdate, b.start)
		}
	}
}

// record adds delta to the bucket of tier i that covers ms. If ms is
// before the last bucket (e.g. clock moved backwards), delta is added
// to the last bucket to keep the tier ordered.
// This method MUST be called only when holding the mu lock.
func (c *Counter) record(i int, ms, delta int64) {
	start := floorTo(ms, tiers[i].width)
	h := c.history[i]
	if n := len(h); n > 0 && start <= h[n-1].start {
		h[n-1].delta += delta
		return
	}
	c.history[i] = append(h, bucket{start: start, delta: delta})
}

// compact moves expired buckets to the next tier.
// Only the expired prefix of each tier is visited.
// This method MUST be called only when holding the mu lock.
func (c *Counter) compact(now int64) {
	for i := 0; i < len(tiers)-1; i++ {
		h := c.history[i]
		n := 0
		for n < len(h) && now-h[n].start > tiers[i].retain {
			c.record(i+1, h[n].start, h[n].delta)
			n++
		}
		if n > 0 {
			c.history[i] = h[:copy(h, h[n:])]
		}
	}
}

// floorTo rounds ms down to a multiple of width.
func floorTo(ms, width int64) int64 {
	r := ms % width
	if r < 0 {
		r += width
	}
	return ms - r
}
