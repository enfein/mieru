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

package util

import (
	"bytes"
	"testing"
)

func TestFillBytes(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		value byte
		want  []byte
	}{
		{"Fill with zero", make([]byte, 5), 0, []byte{0, 0, 0, 0, 0}},
		{"Fill with one", make([]byte, 5), 1, []byte{1, 1, 1, 1, 1}},
		{"Fill with 0xFF", make([]byte, 5), 0xFF, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			FillBytes(tt.input, tt.value)
			if !bytes.Equal(tt.input, tt.want) {
				t.Errorf("FillBytes() = %v, want %v", tt.input, tt.want)
			}
		})
	}
}

func TestIsBitsAllZero(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  bool
	}{
		{"All zero", []byte{0, 0, 0, 0, 0}, true},
		{"Not all zero", []byte{0, 1, 0, 0, 0}, false},
		{"Empty slice", []byte{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsBitsAllZero(tt.input); got != tt.want {
				t.Errorf("IsBitsAllZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsBitsAllOne(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  bool
	}{
		{"All one", []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, true},
		{"Not all one", []byte{0xFF, 0xFE, 0xFF, 0xFF, 0xFF}, false},
		{"Empty slice", []byte{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsBitsAllOne(tt.input); got != tt.want {
				t.Errorf("IsBitsAllOne() = %v, want %v", got, tt.want)
			}
		})
	}
}
