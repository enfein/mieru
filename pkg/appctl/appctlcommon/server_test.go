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

package appctlcommon

import (
	"strings"
	"testing"
	"time"
)

func TestParseServerTCPIdleTimeout(t *testing.T) {
	for _, c := range []struct {
		value   string
		want    time.Duration
		wantErr string
	}{
		{"", 0, ""},
		{"60s", time.Minute, ""},
		{"1m", time.Minute, ""},
		{"600s", 10 * time.Minute, ""},
		{"15m", 15 * time.Minute, ""},
		{"1h", time.Hour, ""},
		{"1m0.5s", time.Minute + 500*time.Millisecond, ""},
		{"0", 0, "less than 1 minute"},
		{"0s", 0, "less than 1 minute"},
		{"-1m", 0, "less than 1 minute"},
		{"59.999999999s", 0, "less than 1 minute"},
		{"60", 0, "is invalid"},
		{"invalid", 0, "is invalid"},
		{" 1m", 0, "is invalid"},
		{"999999999999999999h", 0, "is invalid"},
	} {
		t.Run(c.value, func(t *testing.T) {
			got, err := ParseServerTCPIdleTimeout(c.value)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("error = %v, want %q", err, c.wantErr)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("got (%v, %v), want (%v, nil)", got, err, c.want)
			}
		})
	}
}
