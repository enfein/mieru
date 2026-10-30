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
	"testing"
	"time"
)

func TestClockRealTime(t *testing.T) {
	var clock Clock
	tests := []struct {
		name  string
		clock *Clock
	}{
		{"nil", nil},
		{"zero value", &clock},
		{"reset", &clock},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "reset" {
				clock.SetTime(time.Time{})
				clock.Reset()
			}
			before := time.Now()
			now := tt.clock.Now()
			after := time.Now()
			if now.Before(before) || now.After(after) {
				t.Errorf("Now() = %v, want time between %v and %v", now, before, after)
			}
		})
	}
}

func TestClockControlledTime(t *testing.T) {
	var clock, other Clock
	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	other.SetTime(start)
	tests := []struct {
		name string
		set  time.Time
	}{
		{"zero time", time.Time{}},
		{"wall time", start},
		{"monotonic time", time.Now()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock.SetTime(tt.set)
			want := tt.set
			for _, d := range []time.Duration{0, time.Hour, -time.Minute, time.Nanosecond} {
				clock.Advance(d)
				want = want.Add(d)
				for i := 0; i < 2; i++ {
					if got := clock.Now(); got != want {
						t.Errorf("Now() = %v, want %v", got, want)
					}
				}
			}
			if got := other.Now(); got != start {
				t.Errorf("other.Now() = %v, want %v", got, start)
			}
		})
	}
}

func TestClockAdvanceRealTime(t *testing.T) {
	for _, reset := range []bool{false, true} {
		func() {
			var clock Clock
			if reset {
				clock.SetTime(time.Time{})
				clock.Reset()
			}
			defer func() {
				if recover() == nil {
					t.Error("Advance() with real time did not panic")
				}
			}()
			clock.Advance(time.Second)
		}()
	}
}
