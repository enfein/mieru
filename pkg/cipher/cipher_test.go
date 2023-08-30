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
	c, err := newAESGCMBlockCipher(key)
	if err != nil {
		t.Fatalf("newAESGCMBlockCipher() failed: %v", err)
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
	c, err := newAESGCMBlockCipher(key)
	if err != nil {
		t.Fatalf("newAESGCMBlockCipher() failed: %v", err)
	}
	if c.Overhead() != DefaultOverhead {
		t.Errorf("got overhead size %d; want %d", c.Overhead(), DefaultOverhead)
	}
}

func TestAESGCMBlockCipherEncryptDecrypt(t *testing.T) {
	for i := 0; i < 1000; i++ {
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
		cipher, err := newAESGCMBlockCipher(key)
		if err != nil {
			t.Fatalf("newAESGCMBlockCipher() failed: %v", err)
		}
		if !cipher.IsStateless() {
			t.Fatalf("IsStateless() = %v, want %v", cipher.IsStateless(), true)
		}

		size := mrand.Intn(4096)
		data := make([]byte, size)
		if _, err := crand.Read(data); err != nil {
			t.Fatalf("fail to generate data: %v", err)
		}
		ciphertext, err := cipher.Encrypt(data)
		if err != nil {
			t.Fatalf("Encrypt() failed: %v", err)
		}
		plaintext, err := cipher.Decrypt(ciphertext)
		if err != nil {
			t.Fatalf("Decrypt() failed: %v", err)
		}
		if !bytes.Equal(data, plaintext) {
			t.Errorf("data after decryption is different")
		}

		nonce := make([]byte, DefaultNonceSize)
		if _, err := crand.Read(nonce); err != nil {
			t.Fatalf("fail to generate nonce: %v", err)
		}
		ciphertext, err = cipher.EncryptWithNonce(data, nonce)
		if err != nil {
			t.Fatalf("EncryptWithNonce() failed: %v", err)
		}
		plaintext, err = cipher.DecryptWithNonce(ciphertext, nonce)
		if err != nil {
			t.Fatalf("DecryptWithNonce() failed: %v", err)
		}
		if !bytes.Equal(data, plaintext) {
			t.Errorf("data after decryption is different")
		}
	}
}

func TestAESGCMBlockCipherEncryptDecryptImplicitMode(t *testing.T) {
	key := make([]byte, 32)
	if _, err := crand.Read(key); err != nil {
		t.Fatalf("fail to generate key: %v", err)
	}
	sendCipher, err := newAESGCMBlockCipher(key)
	if err != nil {
		t.Fatalf("newAESGCMBlockCipher() failed: %v", err)
	}
	sendCipher.SetImplicitNonceMode(true)
	recvCipher := sendCipher.Clone().(*AESGCMBlockCipher)
	if sendCipher.IsStateless() {
		t.Fatalf("IsStateless() = %v, want %v", sendCipher.IsStateless(), false)
	}
	if recvCipher.IsStateless() {
		t.Fatalf("IsStateless() = %v, want %v", recvCipher.IsStateless(), false)
	}

	data := make([]byte, 4096)
	for i := 0; i < 1000; i++ {
		if _, err := crand.Read(data); err != nil {
			t.Fatalf("fail to generate data: %v", err)
		}
		ciphertext, err := sendCipher.Encrypt(data)
		if err != nil {
			t.Fatalf("Encrypt() failed: %v", err)
		}
		plaintext, err := recvCipher.Decrypt(ciphertext)
		if err != nil {
			t.Fatalf("Decrypt() failed: %v", err)
		}
		if !bytes.Equal(data, plaintext) {
			t.Errorf("data after decryption is different")
		}
	}
}

func TestAESGCMBlockCipherClone(t *testing.T) {
	key := make([]byte, 32)
	if _, err := crand.Read(key); err != nil {
		t.Fatalf("fail to generate key: %v", err)
	}
	cipher1, err := newAESGCMBlockCipher(key)
	if err != nil {
		t.Fatalf("newAESGCMBlockCipher() failed: %v", err)
	}
	cipher1.SetImplicitNonceMode(true)
	nonce := make([]byte, cipher1.NonceSize())
	if _, err := crand.Read(nonce); err != nil {
		t.Fatalf("fail to generate nonce: %v", err)
	}
	cipher1.implicitNonce = make([]byte, cipher1.NonceSize())
	copy(cipher1.implicitNonce, nonce)
	cipher2 := cipher1.Clone().(*AESGCMBlockCipher)

	data := make([]byte, 4096)
	if _, err := crand.Read(data); err != nil {
		t.Fatalf("fail to generate data: %v", err)
	}
	ciphertext1, err := cipher1.Encrypt(data)
	if err != nil {
		t.Fatalf("Encrypt() failed: %v", err)
	}
	ciphertext2, err := cipher2.Encrypt(data)
	if err != nil {
		t.Fatalf("Encrypt() failed: %v", err)
	}
	if !bytes.Equal(ciphertext1, ciphertext2) {
		t.Errorf("data after encryption is different")
	}

	copy(cipher1.implicitNonce, nonce)
	copy(cipher2.implicitNonce, nonce)
	plaintext1, err := cipher1.Decrypt(ciphertext1)
	if err != nil {
		t.Fatalf("Decrypt() failed: %v", err)
	}
	plaintext2, err := cipher2.Decrypt(ciphertext2)
	if err != nil {
		t.Fatalf("Decrypt() failed: %v", err)
	}
	if !bytes.Equal(plaintext1, plaintext2) {
		t.Errorf("data after decryption is different")
	}
}

func TestAESGCMBlockCipherIncreaseNonce(t *testing.T) {
	testdata := []struct {
		input  []byte
		output []byte
	}{
		{[]byte{0x89, 0x64}, []byte{0x8a, 0x64}},
		{[]byte{0xff, 0xff, 0xff, 0xfe}, []byte{0x00, 0x00, 0x00, 0xff}},
		{[]byte{0xff, 0xff, 0xff, 0xff}, []byte{0x00, 0x00, 0x00, 0x00}},
	}

	cipher := &AESGCMBlockCipher{enableImplicitNonce: true}
	for _, tc := range testdata {
		cipher.implicitNonce = tc.input
		cipher.increaseNonce()
		if !bytes.Equal(cipher.implicitNonce, tc.output) {
			t.Errorf("got %v, want %v", cipher.implicitNonce, tc.output)
		}
	}
}

func TestAESGCMBlockCipherNewNonce(t *testing.T) {
	key := make([]byte, 32)
	if _, err := crand.Read(key); err != nil {
		t.Fatalf("fail to generate key: %v", err)
	}
	c, err := newAESGCMBlockCipher(key)
	if err != nil {
		t.Fatalf("newAESGCMBlockCipher() failed: %v", err)
	}
	distribution := make(map[byte]int32)
	for i := 0; i < 100000; i++ {
		nonce, err := c.newNonce()
		if err != nil {
			t.Fatalf("newNonce() failed: %v", err)
		}
		for j, b := range nonce {
			if j >= noncePrintablePrefixLen {
				break
			}
			if b < printableCharSub || b > printableCharSup {
				t.Fatalf("Byte %v in position %d is not a printable ASCII character", b, j)
			}
			distribution[b]++
		}
	}
	var max int32 = 0
	var min int32 = 0x7FFFFFFF
	for _, val := range distribution {
		if val > max {
			max = val
		}
		if val < min {
			min = val
		}
	}
	ratio := float64(min) / float64(max)
	t.Logf("Nonce random ratio is %f", ratio)
	if ratio < 0.8 {
		t.Errorf("Nonce random ratio %f is too low", ratio)
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
