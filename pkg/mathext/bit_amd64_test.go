// Copyright (C) 2026  mieru authors
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
	mrand "math/rand"
	"testing"

	"golang.org/x/sys/cpu"
)

func TestBMI2Implementations(t *testing.T) {
	if !cpu.X86.HasBMI2 {
		t.Skip("CPU does not support BMI2")
	}

	r := mrand.New(mrand.NewSource(0))
	for i := 0; i < 10_000; i++ {
		x := r.Uint64()
		mask := r.Uint64()
		if got, want := pdepBMI2(x, mask), pdepReference(x, mask); got != want {
			t.Fatalf("pdepBMI2(%#x, %#x) = %#x, want %#x", x, mask, got, want)
		}
		if got, want := pextBMI2(x, mask), pextReference(x, mask); got != want {
			t.Fatalf("pextBMI2(%#x, %#x) = %#x, want %#x", x, mask, got, want)
		}
	}
}
