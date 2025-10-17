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
	"time"

	pb "github.com/enfein/mieru/v3/pkg/metrics/metricspb"
	"google.golang.org/protobuf/proto"
)

const (
	rollUpInterval       = 1000
	rollUpToSecond       = 2 * time.Second
	rollUpSecondToMinute = 120 * time.Second
	rollUpMinuteToHour   = 120 * time.Minute
	rollUpHourToDay      = 8 * 24 * time.Hour // provide accurate hourly data for at least 7 days
)

// Counter holds a named int64 value that can't decrease.
//
// If time series is enabled, user can query history updates
// in a given time range.
type Counter struct {
	name  string
	value int64

	timeSeries bool
	history    []*pb.History

	mu sync.Mutex
	op uint64 // number of operations
}

var _ Metric = &Counter{}

func (c *Counter) Name() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.op++
	return c.name
}

func (c *Counter) Type() MetricType {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.op++
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
	return c.addWithTime(delta, time.Now())
}

func (c *Counter) Load() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.op++

	return c.value
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
	c.mu.Lock()
	defer c.mu.Unlock()
	c.op++

	t1Idx := sort.Search(
		len(c.history),
		func(i int) bool { return time.UnixMilli(c.history[i].GetTimeUnixMilli()).After(t1) },
	)
	t2Idx := sort.Search(
		len(c.history),
		func(i int) bool { return time.UnixMilli(c.history[i].GetTimeUnixMilli()).After(t2) },
	)
	var sum int64
	for i := t1Idx; i < t2Idx; i++ {
		sum += c.history[i].GetDelta()
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
	c.op++

	if len(c.history) == 0 {
		return time.Time{}
	}
	if c.history[len(c.history)-1].TimeUnixMilli == nil {
		return time.Time{}
	}
	return time.UnixMilli(*c.history[len(c.history)-1].TimeUnixMilli)
}

func (c *Counter) addWithTime(delta int64, time time.Time) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.op++

	if delta == 0 {
		return c.value
	}

	c.value += delta
	if c.timeSeries {
		if c.history == nil {
			c.history = make([]*pb.History, 0)
		}
		h := &pb.History{
			TimeUnixMilli: proto.Int64(time.UnixMilli()),
			Delta:         proto.Int64(delta),
			RollUp:        pb.RollUpLabel_NO_ROLL_UP.Enum(),
		}
		c.history = append(c.history, h)
		c.rollUp()
	}
	return c.value
}

// rollUp aggregates the history.
// This method MUST be called only when holding the mu lock.
func (c *Counter) rollUp() {
	if c.op%rollUpInterval != 0 {
		return
	}

	c.doRollUp(pb.RollUpLabel_NO_ROLL_UP, pb.RollUpLabel_ROLL_UP_TO_SECOND, rollUpToSecond, time.Second)
	c.doRollUp(pb.RollUpLabel_ROLL_UP_TO_SECOND, pb.RollUpLabel_ROLL_UP_TO_SECOND, rollUpToSecond, time.Second)

	c.doRollUp(pb.RollUpLabel_ROLL_UP_TO_SECOND, pb.RollUpLabel_ROLL_UP_TO_MINUTE, rollUpSecondToMinute, time.Minute)
	c.doRollUp(pb.RollUpLabel_ROLL_UP_TO_MINUTE, pb.RollUpLabel_ROLL_UP_TO_MINUTE, rollUpSecondToMinute, time.Minute)

	c.doRollUp(pb.RollUpLabel_ROLL_UP_TO_MINUTE, pb.RollUpLabel_ROLL_UP_TO_HOUR, rollUpMinuteToHour, time.Hour)
	c.doRollUp(pb.RollUpLabel_ROLL_UP_TO_HOUR, pb.RollUpLabel_ROLL_UP_TO_HOUR, rollUpMinuteToHour, time.Hour)

	c.doRollUp(pb.RollUpLabel_ROLL_UP_TO_HOUR, pb.RollUpLabel_ROLL_UP_TO_DAY, rollUpHourToDay, 24*time.Hour)
	c.doRollUp(pb.RollUpLabel_ROLL_UP_TO_DAY, pb.RollUpLabel_ROLL_UP_TO_DAY, rollUpHourToDay, 24*time.Hour)
}

func (c *Counter) doRollUp(fromLabel, toLabel pb.RollUpLabel, rollUpDuration, truncateDuration time.Duration) {
	newHistory := make([]*pb.History, 0)
	var last *pb.History
	for _, h := range c.history {
		// case 1: h should not be rolled up
		if h.GetRollUp() != fromLabel {
			newHistory = append(newHistory, h)
			continue
		}
		t := time.UnixMilli(h.GetTimeUnixMilli())
		if time.Since(t) <= rollUpDuration {
			if last != nil {
				newHistory = append(newHistory, last)
				last = nil
			}
			newHistory = append(newHistory, h)
			continue
		}

		// case 2: h should be rolled up
		t = t.Truncate(truncateDuration)
		if last == nil {
			last = &pb.History{
				TimeUnixMilli: proto.Int64(t.UnixMilli()),
				Delta:         proto.Int64(h.GetDelta()),
				RollUp:        toLabel.Enum(),
			}
		} else {
			if last.GetTimeUnixMilli() == t.UnixMilli() {
				*last.Delta = *last.Delta + h.GetDelta()
			} else {
				newHistory = append(newHistory, last)
				last = &pb.History{
					TimeUnixMilli: proto.Int64(t.UnixMilli()),
					Delta:         proto.Int64(h.GetDelta()),
					RollUp:        toLabel.Enum(),
				}
			}
		}
	}
	if last != nil {
		newHistory = append(newHistory, last)
	}
	c.history = newHistory
}
