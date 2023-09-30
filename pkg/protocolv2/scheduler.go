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

package protocolv2

import (
	"sync"
	"time"

	"github.com/enfein/mieru/pkg/rng"
	"github.com/enfein/mieru/pkg/util"
)

var (
	// scheduleIdleTime determines when an idle underlay should be closed.
	// It may take 5 seconds to 15 seconds in different machines.
	scheduleIdleTime = time.Duration(5+rng.FixedInt(11)) * time.Second
)

// ScheduleController controls scheduling a new client session to a underlay.
type ScheduleController struct {
	pending          int // number of pending sessions going to be scheduled
	lastScheduleTime time.Time
	disable          bool // if scheduling to the underlay is disabled
	disableTime      time.Time
	mu               sync.Mutex
}

// IncPending increases the number of pending sessions by 1.
func (c *ScheduleController) IncPending() (ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.disable {
		return false
	}
	c.pending++
	c.lastScheduleTime = time.Now()
	return true
}

// DecPending decreases the number of pending sessions by 1.
func (c *ScheduleController) DecPending() (ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.disable {
		return false
	}
	c.pending--
	c.lastScheduleTime = time.Now()
	return true
}

// IsDisabled returns true if scheduling new sessions to the underlay is disabled.
func (c *ScheduleController) IsDisabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.disable
}

// Idle returns true if the scheduling has been disabled for the given interval time.
func (c *ScheduleController) Idle() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.disable && time.Since(c.disableTime) > scheduleIdleTime
}

// TryDisable tries to disable scheduling new sessions.
func (c *ScheduleController) TryDisable() (ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending > 0 {
		return false
	}
	if util.IsZeroTime(c.lastScheduleTime) {
		c.lastScheduleTime = time.Now()
	}
	if time.Since(c.lastScheduleTime) < scheduleIdleTime {
		return false
	}
	if c.disable {
		return true
	}
	c.disable = true
	c.disableTime = time.Now()
	return true
}
