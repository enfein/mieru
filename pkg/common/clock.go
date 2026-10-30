// Copyright (C) 2026  mieru authors
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

package common

import (
	"sync/atomic"
	"time"
)

// Clock returns real time by default and can be controlled in tests.
// Its zero value is ready to use. All methods are safe for concurrent use.
// A Clock must not be copied after first use.
type Clock struct {
	now atomic.Pointer[time.Time]
}

// Now returns the current time. A nil Clock also returns real time.
func (c *Clock) Now() time.Time {
	if c != nil {
		if now := c.now.Load(); now != nil {
			return *now
		}
	}
	return time.Now()
}

// SetTime freezes the clock at now for testing. Time stays fixed until another
// call to SetTime, Advance, or Reset.
func (c *Clock) SetTime(now time.Time) {
	c.now.Store(&now)
}

// Advance moves the frozen time by d for testing. A negative d moves it backward.
// It panics if SetTime has not been called or the clock has been reset.
func (c *Clock) Advance(d time.Duration) {
	for {
		now := c.now.Load()
		if now == nil {
			panic("Can't advance a real clock. Call SetTime() first.")
		}
		next := now.Add(d)
		if c.now.CompareAndSwap(now, &next) {
			return
		}
	}
}

// Reset restores real time after testing.
func (c *Clock) Reset() {
	c.now.Store(nil)
}
