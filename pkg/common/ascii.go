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

package common

import (
	crand "crypto/rand"
	"math/big"
)

const (
	PrintableCharSub = 0x20 // 0x20, i.e. ' ', is the first printable ASCII character
	PrintableCharSup = 0x7E // 0x7E, i.e. '~', is the last printable ASCII character

	// Common64Set contains 64 selected common characters.
	// This value can change in different software releases.
	Common64Set = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz+/"
)

var (
	printableCharRange = big.NewInt(PrintableCharSup - PrintableCharSub + 1)
)

// ToPrintableChar rewrites [beginIdx, endIdx) of the byte slice with printable
// ASCII characters.
func ToPrintableChar(b []byte, beginIdx, endIdx int) {
	if beginIdx > endIdx {
		panic("begin index > end index")
	}
	if endIdx > len(b) {
		panic("index out of range")
	}
	for i := beginIdx; i < endIdx; i++ {
		if b[i] < PrintableCharSub || b[i] > PrintableCharSup {
			if b[i]&0x80 > 0 {
				lowBits := b[i] & 0x7F
				if lowBits >= PrintableCharSub && lowBits <= PrintableCharSup {
					b[i] = lowBits
					continue
				}
			}
			var randBigInt *big.Int
			var err error
			for {
				randBigInt, err = crand.Int(crand.Reader, printableCharRange)
				if err == nil {
					break
				}
			}
			b[i] = byte(randBigInt.Int64() + PrintableCharSub)
		}
	}
}

// ToCommon64Set rewrites [beginIdx, endIdx) of the byte slice with characters
// from Common64Set.
func ToCommon64Set(b []byte, beginIdx, endIdx int) {
	if beginIdx > endIdx {
		panic("begin index > end index")
	}
	if endIdx > len(b) {
		panic("index out of range")
	}
	for i := beginIdx; i < endIdx; i++ {
		setIdx := b[i] & 0x3f
		b[i] = Common64Set[setIdx]
	}
}

// MaxConsecutivePrintableLength returns the length of the maximum consecutive bytes
// that are printable.
func MaxConsecutivePrintableLength(b []byte) int {
	maxLen := 0
	currentLen := 0

	for _, c := range b {
		if c >= PrintableCharSub && c <= PrintableCharSup {
			currentLen++
			if currentLen > maxLen {
				maxLen = currentLen
			}
		} else {
			currentLen = 0
		}
	}

	return maxLen
}
