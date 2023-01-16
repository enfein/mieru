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

import "testing"

func TestInRange(t *testing.T) {
	cubic := NewCubicSendAlgorithm(16, 16)
	var cwnd uint32
	var inSlowStart bool

	cwnd = cubic.OnAck()
	if cwnd != 16 {
		t.Errorf("OnAck() = %d, want %d", cwnd, 16)
	}
	inSlowStart = cubic.InSlowStart()
	if !inSlowStart {
		t.Errorf("InSlowStart() = %v, want %v", inSlowStart, true)
	}

	cwnd = cubic.OnAck()
	if cwnd != 16 {
		t.Errorf("OnAck() = %d, want %d", cwnd, 16)
	}
	inSlowStart = cubic.InSlowStart()
	if !inSlowStart {
		t.Errorf("InSlowStart() = %v, want %v", inSlowStart, true)
	}

	cwnd = cubic.OnLoss()
	if cwnd != 16 {
		t.Errorf("OnLoss() = %d, want %d", cwnd, 16)
	}
	inSlowStart = cubic.InSlowStart()
	if inSlowStart {
		t.Errorf("InSlowStart() = %v, want %v", inSlowStart, false)
	}
}
