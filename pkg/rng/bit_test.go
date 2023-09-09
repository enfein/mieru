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

package rng

import (
	"bytes"
	"testing"
)

func TestFlipBitsAlreadyUnderRatio(t *testing.T) {
	testcases := []struct {
		bs    []byte
		src   byte
		ratio float64
	}{
		{
			nil,
			0,
			0.5,
		},
		{
			[]byte{0x18, 0x18},
			1,
			0.3,
		},
		{
			[]byte{0x7e, 0x7e},
			0,
			0.3,
		},
	}

	for _, tc := range testcases {
		got := FlipBits(tc.bs, tc.src, tc.ratio)
		if !bytes.Equal(got, tc.bs) {
			t.Errorf("FlipBits() = %v, want %v", got, tc.bs)
		}
	}
}

func TestFlipBits(t *testing.T) {
	solutions := make(map[string]struct{})
	for round := 0; round < 100; round++ {
		input := make([]byte, 20)
		for i := range input {
			input[i] = 0xa5
		}
		output := FlipBits(input, 0, 0.25)
		count0, _ := countBits(output)
		if count0 > 40 {
			t.Fatalf("FlipBits() has more 0 bits than expected")
		}
		solutions[string(output)] = struct{}{}
	}
	if len(solutions) < 90 {
		t.Fatalf("Found too little solutions")
	}

	solutions = make(map[string]struct{})
	for round := 0; round < 100; round++ {
		input := make([]byte, 20)
		for i := range input {
			input[i] = 0xa5
		}
		output := FlipBits(input, 1, 0.25)
		_, count1 := countBits(output)
		if count1 > 40 {
			t.Fatalf("FlipBits() has more 1 bits than expected")
		}
		solutions[string(output)] = struct{}{}
	}
	if len(solutions) < 90 {
		t.Fatalf("Found too little solutions")
	}
}

func TestCountBits(t *testing.T) {
	testcases := []struct {
		input  []byte
		count0 int
		count1 int
	}{
		{
			nil,
			0,
			0,
		},
		{
			[]byte{0x00, 0x00},
			16,
			0,
		},
		{
			[]byte{0xff, 0xff},
			0,
			16,
		},
		{
			[]byte{0x80, 0x03},
			13,
			3,
		},
	}

	for _, tc := range testcases {
		count0, count1 := countBits(tc.input)
		if count0 != tc.count0 || count1 != tc.count1 {
			t.Errorf("CountBits(%v) = %d, %d, want %d, %d", tc.input, count0, count1, tc.count0, tc.count1)
		}
	}
}

func TestXORBit(t *testing.T) {
	bs := []byte{0x5a}
	xorBit(bs, 0)
	xorBit(bs, 1)
	xorBit(bs, 6)
	xorBit(bs, 7)
	want := []byte{0x99}
	if !bytes.Equal(bs, want) {
		t.Errorf("xorBit() = %v, want %v", bs, want)
	}
}
