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
	"bytes"
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

func TestToPrintableCharDistribution(t *testing.T) {
	// Feed one rejected value followed by one value for each printable
	// character to verify both rejection sampling and an even distribution.
	randomData := make([]byte, 0, (printableCharLen+1)*2)
	randomData = append(randomData, 0xff, 0xff)
	for i := 0; i < printableCharLen; i++ {
		randomData = append(randomData, byte(i>>8), byte(i))
	}

	oldReader := crand.Reader
	crand.Reader = bytes.NewReader(randomData)
	t.Cleanup(func() { crand.Reader = oldReader })

	b := make([]byte, printableCharLen)
	ToPrintableChar(b, 0, len(b))
	counts := make([]int, printableCharLen)
	for _, c := range b {
		counts[int(c)-PrintableCharSub]++
	}
	for i, count := range counts {
		if count != 1 {
			t.Errorf("character %q occurred %d times, want 1", byte(i+PrintableCharSub), count)
		}
	}
}

func BenchmarkToPrintableChar(b *testing.B) {
	const size = 1024
	src := make([]byte, size)
	dst := make([]byte, size)

	b.SetBytes(size)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		copy(dst, src)
		ToPrintableChar(dst, 0, len(dst))
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

func TestMaxConsecutivePrintableLength(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected int
	}{
		{
			name:     "empty slice",
			input:    []byte{},
			expected: 0,
		},
		{
			name:     "all printable",
			input:    []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"),
			expected: 62,
		},
		{
			name:     "no printable",
			input:    []byte{0x01, 0x02, 0x03, 0x04},
			expected: 0,
		},
		{
			name:     "mixed printable and non-printable",
			input:    []byte{0x01, 'A', 'B', 0x02, 'C', 'D', 'E', 0x03, 'F', 'G'},
			expected: 3,
		},
		{
			name:     "printable at the end",
			input:    []byte{0x01, 0x02, 'A', 'B', 'C'},
			expected: 3,
		},
		{
			name:     "printable at the beginning",
			input:    []byte{'A', 'B', 'C', 0x01, 0x02},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaxConsecutivePrintableLength(tt.input)
			if result != tt.expected {
				t.Errorf("MaxConsecutivePrintableLength(%v) = %d, want %d", tt.name, result, tt.expected)
			}
		})
	}
}
