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
	"encoding/hex"
	mrand "math/rand"
	"testing"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/common"
	"google.golang.org/protobuf/proto"
)

func TestDefaultNonceSize(t *testing.T) {
	key := make([]byte, 32)
	if _, err := crand.Read(key); err != nil {
		t.Fatalf("fail to generate key: %v", err)
	}
	c, err := newXChaCha20Poly1305BlockCipher(key)
	if err != nil {
		t.Fatalf("newXChaCha20Poly1305BlockCipher() failed: %v", err)
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
	c, err := newXChaCha20Poly1305BlockCipher(key)
	if err != nil {
		t.Fatalf("newXChaCha20Poly1305BlockCipher() failed: %v", err)
	}
	if c.Overhead() != DefaultOverhead {
		t.Errorf("got overhead size %d; want %d", c.Overhead(), DefaultOverhead)
	}
}

func TestAEADBlockCipherEncryptDecrypt(t *testing.T) {
	for i := 0; i < 1000; i++ {
		key := make([]byte, 32)
		if _, err := crand.Read(key); err != nil {
			t.Fatalf("fail to generate key: %v", err)
		}
		cipher, err := newXChaCha20Poly1305BlockCipher(key)
		if err != nil {
			t.Fatalf("newXChaCha20Poly1305BlockCipher() failed: %v", err)
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

func TestAEADBlockCipherEncryptDecryptImplicitMode(t *testing.T) {
	key := make([]byte, 32)
	if _, err := crand.Read(key); err != nil {
		t.Fatalf("fail to generate key: %v", err)
	}
	sendCipher, err := newXChaCha20Poly1305BlockCipher(key)
	if err != nil {
		t.Fatalf("newXChaCha20Poly1305BlockCipher() failed: %v", err)
	}
	sendCipher.SetImplicitNonceMode(true)
	recvCipher := sendCipher.Clone().(*AEADBlockCipher)
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

func TestAEADBlockCipherClone(t *testing.T) {
	key := make([]byte, 32)
	if _, err := crand.Read(key); err != nil {
		t.Fatalf("fail to generate key: %v", err)
	}
	cipher1, err := newXChaCha20Poly1305BlockCipher(key)
	if err != nil {
		t.Fatalf("newXChaCha20Poly1305BlockCipher() failed: %v", err)
	}
	cipher1.SetImplicitNonceMode(true)
	nonce := make([]byte, cipher1.NonceSize())
	if _, err := crand.Read(nonce); err != nil {
		t.Fatalf("fail to generate nonce: %v", err)
	}
	cipher1.implicitNonce = make([]byte, cipher1.NonceSize())
	copy(cipher1.implicitNonce, nonce)
	cipher2 := cipher1.Clone().(*AEADBlockCipher)

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

func TestAEADBlockCipherIncreaseNonce(t *testing.T) {
	testdata := []struct {
		input  []byte
		output []byte
	}{
		{[]byte{0x89, 0x64}, []byte{0x89, 0x65}},
		{[]byte{0xfe, 0xff, 0xff, 0xff}, []byte{0xff, 0x00, 0x00, 0x00}},
		{[]byte{0xff, 0xff, 0xff, 0xff}, []byte{0x00, 0x00, 0x00, 0x00}},
	}

	cipher := &AEADBlockCipher{enableImplicitNonce: true}
	for _, tc := range testdata {
		cipher.implicitNonce = tc.input
		cipher.increaseNonce()
		if !bytes.Equal(cipher.implicitNonce, tc.output) {
			t.Errorf("got %v, want %v", cipher.implicitNonce, tc.output)
		}
	}
}

func TestAEADBlockCipherNewNonce(t *testing.T) {
	key := make([]byte, 32)
	if _, err := crand.Read(key); err != nil {
		t.Fatalf("fail to generate key: %v", err)
	}
	c, err := newXChaCha20Poly1305BlockCipher(key)
	if err != nil {
		t.Fatalf("newXChaCha20Poly1305BlockCipher() failed: %v", err)
	}
	for i := 0; i < 10; i++ {
		nonce, err := c.newNonce()
		if err != nil {
			t.Fatalf("newNonce() failed: %v", err)
		}
		if len(nonce) != c.NonceSize() {
			t.Fatalf("nonce size = %d, want %d", len(nonce), c.NonceSize())
		}
	}
}

func TestNewNonceTypePrintable(t *testing.T) {
	key := make([]byte, 32)
	if _, err := crand.Read(key); err != nil {
		t.Fatalf("fail to generate key: %v", err)
	}
	c, err := newXChaCha20Poly1305BlockCipher(key)
	if err != nil {
		t.Fatalf("newXChaCha20Poly1305BlockCipher() failed: %v", err)
	}
	nonceType := appctlpb.NonceType_NONCE_TYPE_PRINTABLE
	minLen := int32(8)
	maxLen := int32(12)
	c.SetNoncePattern(&appctlpb.NoncePattern{
		Type:                &nonceType,
		ApplyToAllUDPPacket: proto.Bool(true),
		MinLen:              &minLen,
		MaxLen:              &maxLen,
	})
	for i := 0; i < 10; i++ {
		nonce, err := c.newNonce()
		if err != nil {
			t.Fatalf("newNonce() failed: %v", err)
		}
		// At least minLen bytes must be printable.
		for j := 0; j < int(minLen); j++ {
			if nonce[j] < common.PrintableCharSub || nonce[j] > common.PrintableCharSup {
				t.Fatalf("byte %d at position %d is not printable", nonce[j], j)
			}
		}
	}
}

func TestNewNonceTypePrintableSubset(t *testing.T) {
	key := make([]byte, 32)
	if _, err := crand.Read(key); err != nil {
		t.Fatalf("fail to generate key: %v", err)
	}
	c, err := newXChaCha20Poly1305BlockCipher(key)
	if err != nil {
		t.Fatalf("newXChaCha20Poly1305BlockCipher() failed: %v", err)
	}
	nonceType := appctlpb.NonceType_NONCE_TYPE_PRINTABLE_SUBSET
	minLen := int32(8)
	maxLen := int32(12)
	c.SetNoncePattern(&appctlpb.NoncePattern{
		Type:                &nonceType,
		ApplyToAllUDPPacket: proto.Bool(true),
		MinLen:              &minLen,
		MaxLen:              &maxLen,
	})
	allowed := make(map[byte]struct{})
	for _, b := range []byte(common.Common64Set) {
		allowed[b] = struct{}{}
	}
	for i := 0; i < 10; i++ {
		nonce, err := c.newNonce()
		if err != nil {
			t.Fatalf("newNonce() failed: %v", err)
		}
		for j := 0; j < int(minLen); j++ {
			if _, ok := allowed[nonce[j]]; !ok {
				t.Fatalf("byte %v at position %d is not in Common64Set", nonce[j], j)
			}
		}
	}
}

func TestNewNonceTypeFixed(t *testing.T) {
	key := make([]byte, 32)
	if _, err := crand.Read(key); err != nil {
		t.Fatalf("fail to generate key: %v", err)
	}
	c, err := newXChaCha20Poly1305BlockCipher(key)
	if err != nil {
		t.Fatalf("newXChaCha20Poly1305BlockCipher() failed: %v", err)
	}
	hexA := "0001020304050607"
	hexB := "08090a0b0c0d0e0f"
	nonceType := appctlpb.NonceType_NONCE_TYPE_FIXED
	c.SetNoncePattern(&appctlpb.NoncePattern{
		Type:                &nonceType,
		ApplyToAllUDPPacket: proto.Bool(true),
		CustomHexStrings:    []string{hexA, hexB},
	})
	prefixA, _ := hex.DecodeString(hexA)
	prefixB, _ := hex.DecodeString(hexB)
	sawA := false
	sawB := false
	for i := 0; i < 1000; i++ {
		nonce, err := c.newNonce()
		if err != nil {
			t.Fatalf("newNonce() failed: %v", err)
		}
		prefix := nonce[:len(prefixA)]
		if bytes.Equal(prefix, prefixA) {
			sawA = true
		} else if bytes.Equal(prefix, prefixB) {
			sawB = true
		} else {
			t.Fatalf("nonce prefix %v matches neither %v nor %v", prefix, prefixA, prefixB)
		}
	}
	if !sawA || !sawB {
		t.Errorf("expected both prefixes to be used; sawA=%v, sawB=%v", sawA, sawB)
	}
}

func TestNewNonceApplyOnce(t *testing.T) {
	key := make([]byte, 32)
	if _, err := crand.Read(key); err != nil {
		t.Fatalf("fail to generate key: %v", err)
	}
	c, err := newXChaCha20Poly1305BlockCipher(key)
	if err != nil {
		t.Fatalf("newXChaCha20Poly1305BlockCipher() failed: %v", err)
	}
	nonceType := appctlpb.NonceType_NONCE_TYPE_PRINTABLE_SUBSET
	minLen := int32(8)
	maxLen := int32(12)
	c.SetNoncePattern(&appctlpb.NoncePattern{
		Type:                &nonceType,
		ApplyToAllUDPPacket: proto.Bool(false),
		MinLen:              &minLen,
		MaxLen:              &maxLen,
	})
	allowed := make(map[byte]struct{})
	for _, b := range []byte(common.Common64Set) {
		allowed[b] = struct{}{}
	}

	// First call should apply the pattern.
	nonce, err := c.newNonce()
	if err != nil {
		t.Fatalf("newNonce() failed: %v", err)
	}
	for j := 0; j < int(minLen); j++ {
		if _, ok := allowed[nonce[j]]; !ok {
			t.Fatalf("first nonce: byte %v at position %d is not in Common64Set", nonce[j], j)
		}
	}

	// Subsequent calls should return plain random nonces (no guaranteed pattern).
	foundNonPattern := false
	for i := 0; i < 1000; i++ {
		nonce, err = c.newNonce()
		if err != nil {
			t.Fatalf("newNonce() failed: %v", err)
		}
		for j := 0; j < int(minLen); j++ {
			if _, ok := allowed[nonce[j]]; !ok {
				foundNonPattern = true
				break
			}
		}
		if foundNonPattern {
			break
		}
	}
	if !foundNonPattern {
		t.Errorf("expected subsequent nonces to be plain random, but all matched the pattern")
	}
}

func BenchmarkAES128GCMStateless(b *testing.B) {
	key, data := benchmarkGenKeyAndData(b, 16)
	block, err := newAESGCMBlockCipher(key)
	if err != nil {
		b.Fatalf("newAESGCMBlockCipher() failed: %v", err)
	}
	block.SetImplicitNonceMode(false)
	benchmarkEncryptDecryptStateless(b, block, data)
}

func BenchmarkAES128GCMStateful(b *testing.B) {
	key, data := benchmarkGenKeyAndData(b, 16)
	block, err := newAESGCMBlockCipher(key)
	if err != nil {
		b.Fatalf("newAESGCMBlockCipher() failed: %v", err)
	}
	block.SetImplicitNonceMode(true)
	block2 := block.Clone().(*AEADBlockCipher)
	benchmarkEncryptDecryptStateful(b, block, block2, data)
}

func BenchmarkAES256GCMStateless(b *testing.B) {
	key, data := benchmarkGenKeyAndData(b, 32)
	block, err := newAESGCMBlockCipher(key)
	if err != nil {
		b.Fatalf("newAESGCMBlockCipher() failed: %v", err)
	}
	block.SetImplicitNonceMode(false)
	benchmarkEncryptDecryptStateless(b, block, data)
}

func BenchmarkAES256GCMStateful(b *testing.B) {
	key, data := benchmarkGenKeyAndData(b, 32)
	block, err := newAESGCMBlockCipher(key)
	if err != nil {
		b.Fatalf("newAESGCMBlockCipher() failed: %v", err)
	}
	block.SetImplicitNonceMode(true)
	block2 := block.Clone().(*AEADBlockCipher)
	benchmarkEncryptDecryptStateful(b, block, block2, data)
}

func BenchmarkChaCha20Poly1305Stateless(b *testing.B) {
	key, data := benchmarkGenKeyAndData(b, 32)
	block, err := newChaCha20Poly1305BlockCipher(key)
	if err != nil {
		b.Fatalf("newChaCha20Poly1305BlockCipher() failed: %v", err)
	}
	block.SetImplicitNonceMode(false)
	benchmarkEncryptDecryptStateless(b, block, data)
}

func BenchmarkChaCha20Poly1305Stateful(b *testing.B) {
	key, data := benchmarkGenKeyAndData(b, 32)
	block, err := newChaCha20Poly1305BlockCipher(key)
	if err != nil {
		b.Fatalf("newChaCha20Poly1305BlockCipher() failed: %v", err)
	}
	block.SetImplicitNonceMode(true)
	block2 := block.Clone().(*AEADBlockCipher)
	benchmarkEncryptDecryptStateful(b, block, block2, data)
}

func BenchmarkXChaCha20Poly1305Stateless(b *testing.B) {
	key, data := benchmarkGenKeyAndData(b, 32)
	block, err := newXChaCha20Poly1305BlockCipher(key)
	if err != nil {
		b.Fatalf("newXChaCha20Poly1305BlockCipher() failed: %v", err)
	}
	block.SetImplicitNonceMode(false)
	benchmarkEncryptDecryptStateless(b, block, data)
}

func BenchmarkXChaCha20Poly1305Stateful(b *testing.B) {
	key, data := benchmarkGenKeyAndData(b, 32)
	block, err := newXChaCha20Poly1305BlockCipher(key)
	if err != nil {
		b.Fatalf("newXChaCha20Poly1305BlockCipher() failed: %v", err)
	}
	block.SetImplicitNonceMode(true)
	block2 := block.Clone().(*AEADBlockCipher)
	benchmarkEncryptDecryptStateful(b, block, block2, data)
}

func benchmarkEncryptDecryptStateless(b *testing.B, block BlockCipher, data []byte) {
	b.Helper()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ciphertext, err := block.Encrypt(data)
		if err != nil {
			b.FailNow()
		}
		if _, err := block.Decrypt(ciphertext); err != nil {
			b.FailNow()
		}
	}
}

func benchmarkEncryptDecryptStateful(b *testing.B, sendBlock, recvBlock BlockCipher, data []byte) {
	b.Helper()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ciphertext, err := sendBlock.Encrypt(data)
		if err != nil {
			b.FailNow()
		}
		if _, err := recvBlock.Decrypt(ciphertext); err != nil {
			b.FailNow()
		}
	}
}

func benchmarkGenKeyAndData(b *testing.B, keyLen int) (key, data []byte) {
	b.Helper()
	key = make([]byte, keyLen)
	data = make([]byte, 1500)
	if _, err := crand.Read(key); err != nil {
		b.Fatalf("Generate key failed.")
	}
	if _, err := crand.Read(data); err != nil {
		b.Fatalf("Generate data failed.")
	}
	return
}
