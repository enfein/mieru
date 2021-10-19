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
	mrand "math/rand"
	"sync"
	"time"
)

var once sync.Once

func init() {
	InitSeed()
}

// InitSeed initializes the random seed.
func InitSeed() {
	once.Do(func() {
		mrand.Seed(time.Now().UnixNano())
	})
}

// Intn returns a random int from [0, n) with scale down distribution.
func Intn(n int) int {
	return int(float64(mrand.Intn(n+1)) * scaleDown())
}

// Intn returns a random int64 from [0, n) with scale down distribution.
func Int63n(n int64) int64 {
	return int64(float64(mrand.Int63n(n+1)) * scaleDown())
}

// IntRange returns a random int from [m, n) with scale down distribution.
func IntRange(m, n int) int {
	return m + Intn(n-m)
}

// IntRange64 returns a random int64 from [m, n) with scale down distribution.
func IntRange64(m, n int64) int64 {
	return m + Int63n(n-m)
}

// RandTime returns a random time from [begin, end) with scale down distribution.
func RandTime(begin, end time.Time) time.Time {
	beginNano := begin.UnixNano()
	endNano := end.UnixNano()
	randNano := IntRange64(beginNano, endNano)
	randSec := randNano / 1000000000
	randNano = randNano % 1000000000
	return time.Unix(randSec, randNano)
}

// scaleDown returns a random number from [0.0, 1.0), where
// a smaller number has higher probability to occur compared to a bigger number.
func scaleDown() float64 {
	base := mrand.Float64()
	return base * base
}
