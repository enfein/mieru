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

package mathext

import (
	"testing"
	"time"
)

const (
	oneSecond         time.Duration = 1 * time.Second
	twoSeconds        time.Duration = 2 * time.Second
	threeSeconds      time.Duration = 3 * time.Second
	negativeOneSecond time.Duration = -oneSecond
)

func TestMin(t *testing.T) {
	min := Min(oneSecond, twoSeconds)
	if min != oneSecond {
		t.Errorf("min = %v, want %v", min, oneSecond)
	}
	min = Min(twoSeconds, oneSecond)
	if min != oneSecond {
		t.Errorf("min = %v, want %v", min, oneSecond)
	}
}

func TestMax(t *testing.T) {
	max := Max(oneSecond, twoSeconds)
	if max != twoSeconds {
		t.Errorf("max = %v, want %v", max, twoSeconds)
	}
	max = Max(twoSeconds, oneSecond)
	if max != twoSeconds {
		t.Errorf("max = %v, want %v", max, twoSeconds)
	}
}

func TestMid(t *testing.T) {
	mid := Mid(threeSeconds, twoSeconds, oneSecond)
	if mid != twoSeconds {
		t.Errorf("mid = %v, want %v", mid, twoSeconds)
	}
	mid = Mid(oneSecond, twoSeconds, threeSeconds)
	if mid != twoSeconds {
		t.Errorf("mid = %v, want %v", mid, twoSeconds)
	}
}

func TestAbs(t *testing.T) {
	if Abs(oneSecond) != oneSecond {
		t.Errorf("abs = %v, want %v", Abs(oneSecond), oneSecond)
	}
	if Abs(negativeOneSecond) != oneSecond {
		t.Errorf("abs = %v, want %v", Abs(negativeOneSecond), oneSecond)
	}
}

func TestWithinRange(t *testing.T) {
	if WithinRange(threeSeconds, twoSeconds, oneSecond) == false {
		t.Errorf("WithinRange(3, 2, 1) = %v, want %v", false, true)
	}
	if WithinRange(negativeOneSecond, twoSeconds, oneSecond) == true {
		t.Errorf("WithinRange(-1, 2, 1) = %v, want %v", true, false)
	}
}
