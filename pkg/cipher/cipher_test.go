// Copyright (C) 2021  mieru authors
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

import (
	"bytes"
	crand "crypto/rand"
	mrand "math/rand"
	"testing"
	"time"
)

func TestDefaultNonceSize(t *testing.T) {
	key := make([]byte, 32)
	if _, err := crand.Read(key); err != nil {
		t.Fatalf("fail to generate key: %v", err)
	}
	c, err := NewAESGCMBlockCipher(key)
	if err != nil {
		t.Fatalf("create AES GCM block cipher: %v", err)
	}
	if c.NonceSize() != DefaultNonceSize {
		t.Errorf("got nonce size %d; want %d", c.NonceSize(), DefaultNonceSize)
	}
}

func TestDefaultOverhead(t *testing.T) {
	key := make([]byte, 32)
	if _, err := crand.Read(key); err != nil {
		t.Fatalf("fail to generate key: %v", err)
	}
	c, err := NewAESGCMBlockCipher(key)
	if err != nil {
		t.Fatalf("create AES GCM block cipher: %v", err)
	}
	if c.Overhead() != DefaultOverhead {
		t.Errorf("got overhead size %d; want %d", c.Overhead(), DefaultOverhead)
	}
}

func TestAESGCMBlockCipherValidateKeySize(t *testing.T) {
	testdata := []struct {
		key []byte
		err bool
	}{
		{nil, true},
		{[]byte{}, true},
		{make([]byte, 16), false},
		{make([]byte, 24), false},
		{make([]byte, 32), false},
		{make([]byte, 48), true},
	}

	for _, tc := range testdata {
		got := validateKeySize(tc.key)
		if got != nil && !tc.err {
			t.Errorf("got %v; want no error", got)
		}
		if got == nil && tc.err {
			t.Errorf("got no error; want error")
		}
	}
}

func TestAESGCMBlockCipherEncryptDecrypt(t *testing.T) {
	round := 1000
	for i := 0; i < round; i++ {
		var key []byte
		mrand.Seed(time.Now().UnixNano())
		n := mrand.Intn(3)
		switch n {
		case 0:
			key = make([]byte, 16)
		case 1:
			key = make([]byte, 24)
		case 2:
			key = make([]byte, 32)
		}
		if _, err := crand.Read(key); err != nil {
			t.Fatalf("fail to generate key: %v", err)
		}
		c, err := NewAESGCMBlockCipher(key)
		if err != nil {
			t.Errorf("create AES GCM block cipher: %v", err)
		}

		size := mrand.Intn(4096)
		data := make([]byte, size)
		if _, err := mrand.Read(data); err != nil {
			t.Fatalf("fail to generate data: %v", err)
		}

		ciphertext, err := c.Encrypt(data)
		if err != nil {
			t.Errorf("encrypt: %v", err)
		}
		plaintext, err := c.Decrypt(ciphertext)
		if err != nil {
			t.Errorf("decrypt: %v", err)
		}
		if !bytes.Equal(data, plaintext) {
			t.Errorf("data after decryption is different")
		}
	}
}
