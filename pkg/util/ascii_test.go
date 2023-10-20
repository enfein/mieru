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
			t.Fatalf("Found non printable character")
		}
	}
}
