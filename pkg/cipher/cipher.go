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
	"crypto/aes"
	"crypto/cipher"
	crand "crypto/rand"
	"fmt"
	"sync"

	"github.com/enfein/mieru/pkg/util"
)

const (
	noncePrintablePrefixLen = 8
)

var (
	_ BlockCipher = &AESGCMBlockCipher{}
)

// AESGCMBlockCipher implements BlockCipher interface with AES-GCM algorithm.
type AESGCMBlockCipher struct {
	aead                cipher.AEAD
	enableImplicitNonce bool
	key                 []byte
	implicitNonce       []byte
	mu                  sync.Mutex
	ctx                 BlockContext
}

// newAESGCMBlockCipher creates a new cipher with the supplied key.
func newAESGCMBlockCipher(key []byte) (*AESGCMBlockCipher, error) {
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

	c := &AESGCMBlockCipher{
		aead:                aead,
		enableImplicitNonce: false,
		key:                 key,
		implicitNonce:       nil,
	}

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
	c.mu.Lock()
	defer c.mu.Unlock()
	var nonce []byte
	var err error
	needSendNonce := true
	if c.enableImplicitNonce {
		if len(c.implicitNonce) == 0 {
			c.implicitNonce, err = c.newNonce()
			if err != nil {
				return nil, fmt.Errorf("newNonce() failed: %w", err)
			}
			// Must create a copy because nonce will be extended.
			nonce = make([]byte, len(c.implicitNonce))
			copy(nonce, c.implicitNonce)
		} else {
			c.increaseNonce()
			nonce = c.implicitNonce
			needSendNonce = false
		}
	} else {
		nonce, err = c.newNonce()
		if err != nil {
			return nil, fmt.Errorf("newNonce() failed: %w", err)
		}
	}

	dst := c.aead.Seal(nil, nonce, plaintext, nil)
	if needSendNonce {
		return append(nonce, dst...), nil
	}
	return dst, nil
}

func (c *AESGCMBlockCipher) EncryptWithNonce(plaintext, nonce []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.enableImplicitNonce {
		return nil, fmt.Errorf("EncryptWithNonce() is not supported when implicit nonce is enabled")
	}
	if len(nonce) != DefaultNonceSize {
		return nil, fmt.Errorf("want nonce size %d, got %d", DefaultNonceSize, len(nonce))
	}
	return c.aead.Seal(nil, nonce, plaintext, nil), nil
}

func (c *AESGCMBlockCipher) Decrypt(ciphertext []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var nonce []byte
	if c.enableImplicitNonce {
		if len(c.implicitNonce) == 0 {
			if len(ciphertext) < c.NonceSize() {
				return nil, fmt.Errorf("ciphertext is smaller than nonce size")
			}
			c.implicitNonce = make([]byte, c.NonceSize())
			copy(c.implicitNonce, []byte(ciphertext[:c.NonceSize()]))
			ciphertext = ciphertext[c.NonceSize():]
		} else {
			c.increaseNonce()
		}
		nonce = c.implicitNonce
	} else {
		if len(ciphertext) < c.NonceSize() {
			return nil, fmt.Errorf("ciphertext is smaller than nonce size")
		}
		nonce = ciphertext[:c.NonceSize()]
		ciphertext = ciphertext[c.NonceSize():]
	}

	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("cipher.AEAD.Open() failed: %w", err)
	}
	return plaintext, nil
}

func (c *AESGCMBlockCipher) DecryptWithNonce(ciphertext, nonce []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.enableImplicitNonce {
		return nil, fmt.Errorf("EncryptWithNonce() is not supported when implicit nonce is enabled")
	}
	if len(nonce) != DefaultNonceSize {
		return nil, fmt.Errorf("want nonce size %d, got %d", DefaultNonceSize, len(nonce))
	}
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("cipher.AEAD.Open() failed: %w", err)
	}
	return plaintext, nil
}

func (c *AESGCMBlockCipher) Clone() BlockCipher {
	c.mu.Lock()
	defer c.mu.Unlock()
	newCipher, err := newAESGCMBlockCipher(c.key)
	if err != nil {
		panic(err)
	}
	newCipher.enableImplicitNonce = c.enableImplicitNonce
	if len(c.implicitNonce) != 0 {
		newCipher.implicitNonce = make([]byte, len(c.implicitNonce))
		copy(newCipher.implicitNonce, c.implicitNonce)
	}
	newCipher.ctx = c.ctx
	return newCipher
}

func (c *AESGCMBlockCipher) SetImplicitNonceMode(enable bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enableImplicitNonce = enable
	if !enable {
		c.implicitNonce = nil
	}
}

func (c *AESGCMBlockCipher) IsStateless() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.enableImplicitNonce
}

func (c *AESGCMBlockCipher) BlockContext() BlockContext {
	return c.ctx
}

func (c *AESGCMBlockCipher) SetBlockContext(bc BlockContext) {
	c.ctx = bc
}

// newNonce generates a new nonce.
func (c *AESGCMBlockCipher) newNonce() ([]byte, error) {
	nonce := make([]byte, c.NonceSize())
	if _, err := crand.Read(nonce); err != nil {
		return nil, err
	}

	// Adjust the nonce such that the first a few bytes are printable ASCII characters.
	rewriteLen := noncePrintablePrefixLen
	if rewriteLen > c.NonceSize() {
		rewriteLen = c.NonceSize()
	}
	util.ToPrintableChar(nonce, 0, rewriteLen)
	return nonce, nil
}

func (c *AESGCMBlockCipher) increaseNonce() {
	if !c.enableImplicitNonce || len(c.implicitNonce) == 0 {
		panic("implicit nonce mode is not enabled")
	}
	for i := range c.implicitNonce {
		j := len(c.implicitNonce) - 1 - i
		c.implicitNonce[j] += 1
		if c.implicitNonce[j] != 0 {
			break
		}
	}
}

// validateKeySize validates if key size is acceptable.
func validateKeySize(key []byte) error {
	keyLen := len(key)
	if keyLen != 16 && keyLen != 24 && keyLen != 32 {
		return fmt.Errorf("AES key length is %d, want 16 or 24 or 32", keyLen)
	}
	return nil
}
