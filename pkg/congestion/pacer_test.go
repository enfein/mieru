// Copyright (C) 2024  mieru authors
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

func TestPacer(t *testing.T) {
	pacer := NewPacer(1000, 10000, 100)
	now := time.Now().Truncate(time.Second)
	pacer.OnPacketSent(now, 1000, 1000)

	// Budget at now is 0.
	if can := pacer.CanSend(now, 1, 1000); can {
		t.Errorf("CanSend() = %v, want %v", can, false)
	}

	if can := pacer.CanSend(now.Add(time.Second), 999, 1000); !can {
		t.Errorf("CanSend() = %v, want %v", can, true)
	}

	// Minimum pacing rate is 100.
	if can := pacer.CanSend(now.Add(time.Second), 99, 10); !can {
		t.Errorf("CanSend() = %v, want %v", can, true)
	}

	// Maximum budget is 10000.
	if can := pacer.CanSend(now.Add(time.Second), 10001, 100000); can {
		t.Errorf("CanSend() = %v, want %v", can, false)
	}
}
