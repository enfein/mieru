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
)

const (
	rollUpInterval       = 100
	rollUpSecondToMinute = 120 * time.Second
	rollUpMinuteToHour   = 120 * time.Minute
	rollUpHourToDay      = 48 * time.Hour
)

type rollUpLabel uint8

const (
	noRollUp rollUpLabel = iota
	toMinute
	toHour
	toDay
)

// Counter holds a named int64 value that can't decrease.
//
// If time series is enabled, user can query history updates
// in a given time range.
type Counter struct {
	name  string
	value int64

	timeSeries bool
	history    []record

	mu sync.Mutex
	op uint64 // number of operations
}

var _ Metric = &Counter{}

func (c *Counter) Name() string {
	c.op++
	return c.name
}

func (c *Counter) Type() MetricType {
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
		func(i int) bool { return c.history[i].time.After(t1) },
	)
	t2Idx := sort.Search(
		len(c.history),
		func(i int) bool { return c.history[i].time.After(t2) },
	)
	var sum int64
	for i := t1Idx; i < t2Idx; i++ {
		sum += c.history[i].delta
	}
	return sum
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
			c.history = make([]record, 0)
		}
		r := record{time: time, delta: delta, label: noRollUp}
		c.history = append(c.history, r)
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
	c.doRollUp(noRollUp, toMinute, rollUpSecondToMinute, time.Minute)
	c.doRollUp(toMinute, toMinute, rollUpSecondToMinute, time.Minute)
	c.doRollUp(toMinute, toHour, rollUpMinuteToHour, time.Hour)
	c.doRollUp(toHour, toHour, rollUpMinuteToHour, time.Hour)
	c.doRollUp(toHour, toDay, rollUpHourToDay, 24*time.Hour)
	c.doRollUp(toDay, toDay, rollUpHourToDay, 24*time.Hour)
}

func (c *Counter) doRollUp(fromLabel, toLabel rollUpLabel, rollUpDuration, truncateDuration time.Duration) {
	newHistory := make([]record, 0)
	var last *record
	for _, r := range c.history {
		if r.label != fromLabel {
			newHistory = append(newHistory, r)
			continue
		}
		if time.Since(r.time) <= rollUpDuration {
			if last != nil {
				newHistory = append(newHistory, *last)
				last = nil
			}
			newHistory = append(newHistory, r)
			continue
		}
		if last == nil {
			last = &record{
				time:  r.time.Truncate(truncateDuration),
				delta: r.delta,
				label: toLabel,
			}
		} else {
			t := r.time.Truncate(truncateDuration)
			if last.time.Equal(t) {
				last.delta += r.delta
			} else {
				newHistory = append(newHistory, *last)
				last = &record{
					time:  r.time.Truncate(truncateDuration),
					delta: r.delta,
					label: toLabel,
				}
			}
		}
	}
	if last != nil {
		newHistory = append(newHistory, *last)
	}
	c.history = newHistory
}

type record struct {
	time  time.Time
	delta int64
	label rollUpLabel
}
