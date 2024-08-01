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

func TestMinFilter(t *testing.T) {
	filter := NewWindowedFilter(5, 0, MinFilter[int])
	for i := 1; i <= 10; i++ {
		filter.Update(i, int64(i))
	}
	best := filter.GetBest()
	second := filter.GetSecondBest()
	third := filter.GetThirdBest()
	t.Logf("MinFilter: %d %d %d", best, second, third)
	if best >= second {
		t.Errorf("with MinFilter, got best >= second")
	}
	if best >= third {
		t.Errorf("with MinFilter, got best >= third")
	}
	if second >= third {
		t.Errorf("with MinFilter, got second >= third")
	}
	filter.Reset(0, time.Now().UnixNano())
	if filter.GetBest() != 0 || filter.GetSecondBest() != 0 || filter.GetThirdBest() != 0 {
		t.Errorf("got non-zero after Reset()")
	}
}

func TestMaxFilter(t *testing.T) {
	filter := NewWindowedFilter(5, 0, MaxFilter[int])
	for i := 10; i >= 1; i-- {
		filter.Update(i, int64(11-i))
	}
	best := filter.GetBest()
	second := filter.GetSecondBest()
	third := filter.GetThirdBest()
	t.Logf("MaxFilter: %d %d %d", best, second, third)
	if best <= second {
		t.Errorf("with MaxFilter, got best <= second")
	}
	if best <= third {
		t.Errorf("with MaxFilter, got best <= third")
	}
	if second <= third {
		t.Errorf("with MaxFilter, got second <= third")
	}
	filter.Reset(0, time.Now().UnixNano())
	if filter.GetBest() != 0 || filter.GetSecondBest() != 0 || filter.GetThirdBest() != 0 {
		t.Errorf("got non-zero after Reset()")
	}
}
