// Copyright (C) 2022  mieru authors
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

package cipher

import "testing"

func TestGetBlockCipherList(t *testing.T) {
	password := []byte{0x08, 0x09, 0x06, 0x04}
	ciphers, err := getBlockCipherList(password, true)
	if err != nil {
		t.Fatalf("getBlockCipherList() failed: %v", err)
	}
	if len(ciphers) != 3 {
		t.Fatalf("number of ciphers = %d, want %d", len(ciphers), 3)
	}
	for _, cipher := range ciphers {
		if !cipher.IsStateless() {
			t.Errorf("IsStateless() = %v, want %v", cipher.IsStateless(), true)
		}
	}

	ciphers, err = getBlockCipherList(password, false)
	if err != nil {
		t.Fatalf("getBlockCipherList() failed: %v", err)
	}
	if len(ciphers) != 3 {
		t.Fatalf("number of ciphers = %d, want %d", len(ciphers), 3)
	}
	for _, cipher := range ciphers {
		if cipher.IsStateless() {
			t.Errorf("IsStateless() = %v, want %v", cipher.IsStateless(), false)
		}
	}
}
