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
	"math/bits"
	"strconv"
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

func TestUint32WithBits(t *testing.T) {
	for n := 0; n <= 32; n++ {
		for i := 0; i < 100; i++ {
			got := Uint32WithBits(n)
			if count := bits.OnesCount32(got); count != n {
				t.Fatalf("Uint32WithBits(%d) returned %032b with %d bits set", n, got, count)
			}
		}
	}
}

func TestUint32WithBitsPanic(t *testing.T) {
	for _, n := range []int{-1, 33} {
		t.Run("n="+strconv.Itoa(n), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("Uint32WithBits(%d) did not panic", n)
				}
			}()
			Uint32WithBits(n)
		})
	}
}

func TestFixedInt(t *testing.T) {
	v := FixedInt(256, "test")
	v2 := FixedInt(256, "test")
	if v2 != v {
		t.Errorf("FixedInt() = %d, want %d", v2, v)
	}
}

func TestFixedIntPerHost(t *testing.T) {
	cacheSize := func() int {
		size := 0
		fixedV.Range(func(key, value any) bool {
			size++
			return true
		})
		return size
	}

	prevSize := cacheSize()
	v := FixedIntVH(256)
	v2 := FixedIntVH(256)
	if v2 != v {
		t.Errorf("FixedIntPerHost() = %d, want %d", v2, v)
	}
	nextSize := cacheSize()
	if nextSize > prevSize+1 {
		t.Errorf("The cache size can only increase by 1")
	}
}
