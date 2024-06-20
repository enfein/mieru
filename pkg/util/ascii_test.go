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

package util

import (
	crand "crypto/rand"
	"testing"
)

func TestToPrintableChar(t *testing.T) {
	b := make([]byte, 1024)
	for {
		if _, err := crand.Read(b); err == nil {
			break
		}
	}

	ToPrintableChar(b, 0, 1024)
	for i := 0; i < 1024; i++ {
		if b[i] < PrintableCharSub || b[i] > PrintableCharSup {
			t.Errorf("Found non printable character")
		}
	}
}

func TestToCommon64Set(t *testing.T) {
	if len(Common64Set) != 64 {
		t.Fatalf("Common64Set has %d characters, want 64", len(Common64Set))
	}
	s := make(map[byte]struct{})
	for _, c := range []byte(Common64Set) {
		s[c] = struct{}{}
	}
	b := make([]byte, 1024)
	for {
		if _, err := crand.Read(b); err == nil {
			break
		}
	}

	ToCommon64Set(b, 0, 1024)
	for i := 0; i < 1024; i++ {
		if _, ok := s[b[i]]; !ok {
			t.Errorf("Found character not in Common64Set")
		}
	}
}
