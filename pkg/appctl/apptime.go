// Copyright (C) 2025  mieru authors
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

package appctl

import (
	"sync"
	"time"
)

var (
	appStartTime     time.Time
	appStartTimeOnce sync.Once
)

// RecordAppStartTime record the app start time.
// This function does nothing after the first use.
func RecordAppStartTime() {
	appStartTimeOnce.Do(func() {
		appStartTime = time.Now()
	})
}

// Elapsed returns the how long the app has been started.
// It panics if the start time is not recorded.
func Elapsed() time.Duration {
	if appStartTime.IsZero() {
		panic("app start time is not recorded")
	}
	return time.Since(appStartTime).Truncate(time.Microsecond)
}
