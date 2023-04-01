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
	"fmt"
	mrand "math/rand"
)

// FlipBits modifies the input byte slice by changing some bits from 0 -> 1
// or 1 -> 0.
//
// The `src` must be either 0 or 1. When it is 0, some 0 bits will be changed
// to 1 bits. When it is 1, some 1 bits will be changed to 0 bits.
//
// The `ratio` specifies the desired ratio of `src` bits in the byte slice
// after flip. The value must be within [0.0, 1.0]. When the input slice already
// has a smaller ratio, no flip is performed.
func FlipBits(bs []byte, src byte, ratio float64) []byte {
	if src != 0 && src != 1 {
		panic(fmt.Sprintf("FlipBits() has invalid input: src is %d, must be either 0 or 1", src))
	}
	if ratio < 0 || ratio > 1 {
		panic(fmt.Sprintf("FlipBits() has invalid input: ratio is %f, must be within [0.0, 1.0]", ratio))
	}
	if len(bs) == 0 {
		return bs
	}

	// Check the current ratio and compare with target.
	toFlip := 0
	count0, count1 := countBits(bs)
	switch src {
	case 0:
		toFlip = count0 - int(float64(count0+count1)*ratio)
	case 1:
		toFlip = count1 - int(float64(count0+count1)*ratio)
	}
	if toFlip <= 0 {
		return bs
	}

	// Pick bits to flip using reservoir algorithm.
	positions := make([]int, toFlip)
	pIdx := 0 // position index
	mIdx := 0 // match index
	for i, b := range bs {
		var mask byte = 0x80
		for j := 7; j >= 0; j-- {
			if (src == 0 && b&mask == 0) || (src == 1 && b&mask != 0) {
				mIdx++
				if pIdx < toFlip {
					positions[pIdx] = 8*i + 7 - j
					pIdx++
				} else {
					r := mrand.Float64()
					if r < float64(toFlip)/float64(mIdx) {
						replace := mrand.Intn(toFlip)
						positions[replace] = 8*i + 7 - j
					}
				}
			}
			mask = mask >> 1
		}
	}

	// Do bit flip.
	for _, pos := range positions {
		xorBit(bs, pos)
	}

	return bs
}

// countBits returns the number of 0 bit and the number of 1 bit
// in the given byte slice.
func countBits(bs []byte) (int, int) {
	if len(bs) == 0 {
		return 0, 0
	}
	count0, count1 := 0, 0
	for _, b := range bs {
		var mask byte = 1
		for i := 0; i < 8; i++ {
			if b&mask > 0 {
				count1++
			} else {
				count0++
			}
			mask = mask << 1
		}
	}
	return count0, count1
}

func xorBit(bs []byte, i int) {
	byteIdx := i / 8
	bitIdx := i % 8
	if len(bs) < byteIdx+1 {
		panic("When XOR bit, index is out of range")
	}
	var mask byte = 1 << bitIdx
	bs[byteIdx] = bs[byteIdx] ^ byte(mask)
}
