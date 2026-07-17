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
	"math/bits"
	mrand "math/rand"
	"testing"
)

func TestRepeatUint32(t *testing.T) {
	tests := []struct {
		name string
		in   uint32
		want uint64
	}{
		{name: "zero", in: 0, want: 0},
		{name: "pattern", in: 0x12345678, want: 0x1234567812345678},
		{name: "all bits set", in: 0xffffffff, want: 0xffffffffffffffff},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RepeatUint32(tt.in); got != tt.want {
				t.Errorf("RepeatUint32(%#x) = %#x, want %#x", tt.in, got, tt.want)
			}
		})
	}
}

func TestPDEP(t *testing.T) {
	tests := []struct {
		name string
		x    uint64
		mask uint64
		want uint64
	}{
		{name: "zero mask", x: ^uint64(0), mask: 0, want: 0},
		{name: "zero value", x: 0, mask: ^uint64(0), want: 0},
		{name: "all bits", x: ^uint64(0), mask: ^uint64(0), want: ^uint64(0)},
		{name: "alternating mask", x: 0xb, mask: 0x55, want: 0x45},
		{name: "sparse mask", x: 0xd, mask: 0x32, want: 0x22},
		{name: "highest bit", x: 1, mask: 1 << 63, want: 1 << 63},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := PDEP(test.x, test.mask); got != test.want {
				t.Fatalf("PDEP(%#x, %#x) = %#x, want %#x", test.x, test.mask, got, test.want)
			}
			if got := pdepGeneric(test.x, test.mask); got != test.want {
				t.Fatalf("pdepGeneric(%#x, %#x) = %#x, want %#x", test.x, test.mask, got, test.want)
			}
		})
	}
}

func TestPEXT(t *testing.T) {
	tests := []struct {
		name string
		x    uint64
		mask uint64
		want uint64
	}{
		{name: "zero mask", x: ^uint64(0), mask: 0, want: 0},
		{name: "zero value", x: 0, mask: ^uint64(0), want: 0},
		{name: "all bits", x: ^uint64(0), mask: ^uint64(0), want: ^uint64(0)},
		{name: "alternating mask", x: 0x45, mask: 0x55, want: 0xb},
		{name: "sparse mask", x: 0x22, mask: 0x32, want: 0x5},
		{name: "highest bit", x: 1 << 63, mask: 1 << 63, want: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := PEXT(test.x, test.mask); got != test.want {
				t.Fatalf("PEXT(%#x, %#x) = %#x, want %#x", test.x, test.mask, got, test.want)
			}
			if got := pextGeneric(test.x, test.mask); got != test.want {
				t.Fatalf("pextGeneric(%#x, %#x) = %#x, want %#x", test.x, test.mask, got, test.want)
			}
		})
	}
}

func TestPDEPAndPEXTGenerated(t *testing.T) {
	r := mrand.New(mrand.NewSource(0))
	for i := 0; i < 10_000; i++ {
		x := r.Uint64()
		mask := r.Uint64()

		if got, want := PDEP(x, mask), pdepReference(x, mask); got != want {
			t.Fatalf("PDEP(%#x, %#x) = %#x, want %#x", x, mask, got, want)
		}
		if got, want := pdepGeneric(x, mask), pdepReference(x, mask); got != want {
			t.Fatalf("pdepGeneric(%#x, %#x) = %#x, want %#x", x, mask, got, want)
		}
		if got, want := PEXT(x, mask), pextReference(x, mask); got != want {
			t.Fatalf("PEXT(%#x, %#x) = %#x, want %#x", x, mask, got, want)
		}
		if got, want := pextGeneric(x, mask), pextReference(x, mask); got != want {
			t.Fatalf("pextGeneric(%#x, %#x) = %#x, want %#x", x, mask, got, want)
		}

		lowMask := ^uint64(0)
		if n := bits.OnesCount64(mask); n < 64 {
			lowMask = 1<<n - 1
		}
		if got, want := PEXT(PDEP(x, mask), mask), x&lowMask; got != want {
			t.Fatalf("PEXT(PDEP(%#x, %#x), %#x) = %#x, want %#x", x, mask, mask, got, want)
		}
		if got, want := PDEP(PEXT(x, mask), mask), x&mask; got != want {
			t.Fatalf("PDEP(PEXT(%#x, %#x), %#x) = %#x, want %#x", x, mask, mask, got, want)
		}
	}
}

func pdepReference(x, mask uint64) uint64 {
	var result uint64
	srcBit := uint64(1)
	for maskBit := uint64(1); maskBit != 0; maskBit <<= 1 {
		if mask&maskBit == 0 {
			continue
		}
		if x&srcBit != 0 {
			result |= maskBit
		}
		srcBit <<= 1
	}
	return result
}

func pextReference(x, mask uint64) uint64 {
	var result uint64
	resultBit := uint64(1)
	for maskBit := uint64(1); maskBit != 0; maskBit <<= 1 {
		if mask&maskBit == 0 {
			continue
		}
		if x&maskBit != 0 {
			result |= resultBit
		}
		resultBit <<= 1
	}
	return result
}
