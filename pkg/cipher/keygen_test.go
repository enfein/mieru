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

package cipher

import (
	"bytes"
	"fmt"
	"testing"
	"time"
)

func TestSaltFromTimeSize(t *testing.T) {
	salts := saltFromTime(time.Now())
	if len(salts) != 3 {
		t.Errorf("got %d []byte; want 3", len(salts))
	}
}

func hasOverlap(a, b [][]byte) bool {
	for _, c := range a {
		for _, d := range b {
			if bytes.Equal(c, d) {
				return true
			}
		}
	}
	return false
}

func TestSaltFromTimeCoverage(t *testing.T) {
	noOverlap := 0
	nTest := 30

	for i := 1; i <= nTest; i++ {
		forward, err := time.ParseDuration(fmt.Sprintf("%dm", i))
		if err != nil {
			t.Fatalf("failed to parse time duration")
		}
		backward, err := time.ParseDuration(fmt.Sprintf("-%dm", i))
		if err != nil {
			t.Fatalf("failed to parse time duration")
		}

		curr := saltFromTime(time.Now())
		prev := saltFromTime(time.Now().Add(backward))
		next := saltFromTime(time.Now().Add(forward))
		if !hasOverlap(prev, curr) && !hasOverlap(curr, next) {
			noOverlap += 1
		}
	}

	t.Logf("No overlap after %d minutes", nTest-noOverlap)
	if noOverlap == 0 {
		t.Errorf("all tested time difference has salt overlap")
	}
	if noOverlap == nTest {
		t.Errorf("all tested time difference has no salt overlap")
	}
}
