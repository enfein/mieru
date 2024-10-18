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

import (
	"reflect"
	"testing"
)

func TestToBitDistribution(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected BitDistribution
	}{
		{
			name:     "empty slice",
			input:    []byte{},
			expected: BitDistribution{Bit0: 0.5, Bit1: 0.5, Bit0Count: 0, Bit1Count: 0},
		},
		{
			name:     "all zeros",
			input:    []byte{0x00, 0x00},
			expected: BitDistribution{Bit0: 1.0, Bit1: 0.0, Bit0Count: 16, Bit1Count: 0},
		},
		{
			name:     "all ones",
			input:    []byte{0xFF, 0xFF},
			expected: BitDistribution{Bit0: 0.0, Bit1: 1.0, Bit0Count: 0, Bit1Count: 16},
		},
		{
			name:     "mixed 1",
			input:    []byte{0xF0, 0x0F},
			expected: BitDistribution{Bit0: 0.5, Bit1: 0.5, Bit0Count: 8, Bit1Count: 8},
		},
		{
			name:     "mixed 2",
			input:    []byte{0xA0, 0x05},
			expected: BitDistribution{Bit0: 0.75, Bit1: 0.25, Bit0Count: 12, Bit1Count: 4},
		},
		{
			name:     "mixed 3",
			input:    []byte{0xFC, 0x3F},
			expected: BitDistribution{Bit0: 0.25, Bit1: 0.75, Bit0Count: 4, Bit1Count: 12},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ToBitDistribution(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("TestToBitDistribution(%v) = %v, want %v", tt.name, result, tt.expected)
			}
		})
	}
}
