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

package protocol

import (
	"sync"
	"time"

	"github.com/enfein/mieru/v3/pkg/rng"
)

var (
	// scheduleIdleTime determines when a underlay is considered idle.
	// It takes 60 seconds to 120 seconds in different machines.
	scheduleIdleTime = time.Duration(60+rng.FixedIntPerHost(61)) * time.Second
)

// ScheduleController controls scheduling a new client session to a underlay.
type ScheduleController struct {
	pending          int // number of pending sessions going to be scheduled
	lastScheduleTime time.Time
	disableTime      time.Time // if set, scheduling to the underlay is disabled after this time
	mu               sync.Mutex
}

// IncPending increases the number of pending sessions by 1.
// The number of pending sessions can't increase if the scheduler is disabled.
func (c *ScheduleController) IncPending() (ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.disableTime.IsZero() && time.Since(c.disableTime) > 0 {
		return false
	}
	c.pending++
	c.lastScheduleTime = time.Now()
	return true
}

// DecPending decreases the number of pending sessions by 1.
func (c *ScheduleController) DecPending() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending--
	c.lastScheduleTime = time.Now()
}

// DisableTime returns the underlay disable time.
func (c *ScheduleController) DisableTime() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.disableTime
}

// IsDisabled returns true if scheduling new sessions to the underlay is disabled.
func (c *ScheduleController) IsDisabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.disableTime.IsZero() && time.Since(c.disableTime) > 0
}

// Idle returns true if the scheduling has been disabled for the given interval.
func (c *ScheduleController) Idle() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.disableTime.IsZero() && time.Since(c.lastScheduleTime) > scheduleIdleTime && time.Since(c.disableTime) > scheduleIdleTime
}

// TryDisableIdle tries to disable scheduling new sessions on idle underlay.
func (c *ScheduleController) TryDisableIdle() (ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.disableTime.IsZero() {
		return false // already disabled
	}
	if c.pending > 0 {
		return false
	}
	if !c.lastScheduleTime.IsZero() && time.Since(c.lastScheduleTime) < scheduleIdleTime {
		return false
	}
	c.disableTime = time.Now()
	return true
}

// SetRemainingTime unconditionally disables the scheduler after the given duration.
// Do nothing if the scheduler has already been disabled.
func (c *ScheduleController) SetRemainingTime(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d < 0 || !c.disableTime.IsZero() {
		return
	}
	c.disableTime = time.Now().Add(d)
}
