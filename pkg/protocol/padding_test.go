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
// along with this program.  If not, see <https://www.gnu.org/licenses/>

package protocol

import (
	"testing"

	"github.com/enfein/mieru/pkg/rng"
)

func TestNewPadding(t *testing.T) {
	maxLen := rng.Intn(256)
	minConsecutiveASCIILen := rng.IntRange(0, maxLen+1)
	padding := newPadding(paddingOpts{
		maxLen:                 maxLen,
		minConsecutiveASCIILen: minConsecutiveASCIILen,
	})
	if len(padding) > maxLen {
		t.Errorf("Padding length %d is bigger than the maximum length %d", len(padding), maxLen)
	}
	if len(padding) < minConsecutiveASCIILen {
		t.Errorf("Padding length %d is smaller than the minimum length %d", len(padding), minConsecutiveASCIILen)
	}
}
