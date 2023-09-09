// Copyright (C) 2021  mieru authors
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

package rng

import (
	"testing"
	"time"
)

func TestIntnScaleDown(t *testing.T) {
	numbers := make([]int, 10)
	for i := 0; i < 100000; i++ {
		n := Intn(10)
		numbers[n] += 1
	}
	for i := 0; i+1 < 10; i++ {
		if numbers[i] < numbers[i+1] {
			t.Errorf("unexpected scale down: numbers[i] should >= numbers[i+1]")
		}
	}
}

func TestRandTime(t *testing.T) {
	oneHour := time.Hour
	begin := time.Now()
	end := begin.Add(oneHour)
	randTime := RandTime(begin, end)
	if randTime.Before(begin) || randTime.After(end) {
		t.Errorf("generated rand time is out of range")
	}
}

func TestFixedInt(t *testing.T) {
	v := FixedInt(256)
	v2 := FixedInt(256)
	if v2 != v {
		t.Errorf("FixedInt() = %d, want %d", v2, v)
	}
}
