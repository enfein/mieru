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
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package common

import "fmt"

// BitDistribution describes the probability of bit 0 and bit 1 in a byte array.
type BitDistribution struct {
	Bit0      float64
	Bit1      float64
	Bit0Count int
	Bit1Count int
}

func (bd BitDistribution) String() string {
	return fmt.Sprintf("BitDistribution{Bit0=%f, Bit1=%f, Bit0Count=%d, Bit1Count=%d}", bd.Bit0, bd.Bit1, bd.Bit0Count, bd.Bit1Count)
}

// ToBitDistribution returns the BitDistribution from a byte array.
func ToBitDistribution(arr []byte) BitDistribution {
	if len(arr) == 0 {
		return BitDistribution{
			Bit0: 0.5,
			Bit1: 0.5,
		}
	}

	var bit0Count, bit1Count float64
	for _, b := range arr {
		for i := 0; i < 8; i++ {
			if (b>>i)&1 == 0 {
				bit0Count++
			} else {
				bit1Count++
			}
		}
	}
	return BitDistribution{
		Bit0:      bit0Count / float64(len(arr)*8),
		Bit1:      bit1Count / float64(len(arr)*8),
		Bit0Count: int(bit0Count),
		Bit1Count: int(bit1Count),
	}
}
