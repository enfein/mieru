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

package congestion

import (
	"testing"
	"time"
)

func TestUpdateRTT(t *testing.T) {
	s := NewRTTStats()

	// First measurement.
	s.UpdateRTT(300 * time.Millisecond)
	if s.MinRTT() != 300*time.Millisecond {
		t.Errorf("MinRTT() = %v, want %v", s.MinRTT(), 300*time.Millisecond)
	}
	if s.LatestRTT() != 300*time.Millisecond {
		t.Errorf("LatestRTT() = %v, want %v", s.LatestRTT(), 300*time.Millisecond)
	}
	if s.SmoothedRTT() != 300*time.Millisecond {
		t.Errorf("SmoothedRTT() = %v, want %v", s.SmoothedRTT(), 300*time.Millisecond)
	}
	if s.MeanDeviation() != 150*time.Millisecond {
		t.Errorf("MeanDeviation() = %v, want %v", s.MeanDeviation(), 150*time.Millisecond)
	}

	// Second measurement.
	s.UpdateRTT(200 * time.Millisecond)
	if s.MinRTT() != 200*time.Millisecond {
		t.Errorf("MinRTT() = %v, want %v", s.MinRTT(), 200*time.Millisecond)
	}
	if s.LatestRTT() != 200*time.Millisecond {
		t.Errorf("LatestRTT() = %v, want %v", s.LatestRTT(), 200*time.Millisecond)
	}
	if s.SmoothedRTT() != 287500*time.Microsecond {
		t.Errorf("SmoothedRTT() = %v, want %v", s.SmoothedRTT(), 287500*time.Microsecond)
	}
	if s.MeanDeviation() != 137500*time.Microsecond {
		t.Errorf("MeanDeviation() = %v, want %v", s.MeanDeviation(), 137500*time.Microsecond)
	}
}

func TestRTO(t *testing.T) {
	s := NewRTTStats()
	s.SetMaxAckDelay(2 * time.Second)
	s.SetRTOMultiplier(2)

	// First measurement.
	s.UpdateRTT(300 * time.Millisecond)
	if s.RTO() != 5800*time.Millisecond {
		t.Errorf("RTO() = %v, want %v", s.RTO(), 5800*time.Millisecond)
	}

	// Reset measurement.
	s.Reset()
	if s.RTO() != 1000*time.Millisecond {
		t.Errorf("RTO() = %v, want %v", s.RTO(), 1000*time.Millisecond)
	}
}
