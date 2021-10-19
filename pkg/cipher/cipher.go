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

// cipher provides functions to encrpyt and decrypt a block of data.
package cipher

import (
	"crypto/aes"
	"crypto/cipher"
	crand "crypto/rand"
	"fmt"
)

const (
	DefaultNonceSize = 12 // 12 bytes
	DefaultOverhead  = 16 // 16 bytes
	DefaultKeyLen    = 32 // 256 bits
)

// BlockCipher is an interface of block encryption and decryption.
type BlockCipher interface {
	// Encrypt method adds the nonce in the dst, then encryptes the src.
	Encrypt(plaintext []byte) ([]byte, error)

	// Decrypt method removes the nonce in the src, then decryptes the src.
	Decrypt(ciphertext []byte) ([]byte, error)
}

// AESGCMBlockCipher implements BlockCipher interface with AES-GCM algorithm.
type AESGCMBlockCipher struct {
	aead cipher.AEAD
}

// NewAESGCMBlockCipher creates a new cipher with the supplied key.
func NewAESGCMBlockCipher(key []byte) (*AESGCMBlockCipher, error) {
	if err := validateKeySize(key); err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher() failed: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM() failed: %w", err)
	}

	c := &AESGCMBlockCipher{aead: aead}

	return c, nil
}

// BlockSize returns the block size of cipher.
func (*AESGCMBlockCipher) BlockSize() int {
	return aes.BlockSize
}

// NonceSize returns the number of bytes used by nonce.
func (c *AESGCMBlockCipher) NonceSize() int {
	return c.aead.NonceSize()
}

func (c *AESGCMBlockCipher) Overhead() int {
	return c.aead.Overhead()
}

func (c *AESGCMBlockCipher) Encrypt(plaintext []byte) ([]byte, error) {
	nonce, err := c.newNonce()
	if err != nil {
		return nil, fmt.Errorf("newNonce() failed: %w", err)
	}

	dst := c.aead.Seal(nil, nonce, plaintext, nil)
	nonce = append(nonce, dst...)
	return nonce, nil
}

func (c *AESGCMBlockCipher) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < c.NonceSize() {
		return nil, fmt.Errorf("ciphertext is smaller than nonce size")
	}

	nonce := ciphertext[:c.NonceSize()]
	ciphertext = ciphertext[c.NonceSize():]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("cipher.AEAD.Open() failed: %w", err)
	}
	return plaintext, nil
}

// newNonce generates a new nonce.
func (c *AESGCMBlockCipher) newNonce() ([]byte, error) {
	nonce := make([]byte, c.NonceSize())
	if _, err := crand.Read(nonce); err != nil {
		return nil, err
	}
	return nonce, nil
}

// validateKeySize validates if key size is acceptable.
func validateKeySize(key []byte) error {
	keyLen := len(key)
	if keyLen != 16 && keyLen != 24 && keyLen != 32 {
		return fmt.Errorf("AES key length is %d, want 16 or 24 or 32", keyLen)
	}
	return nil
}
