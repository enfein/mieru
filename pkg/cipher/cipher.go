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

	"github.com/enfein/mieru/v3/pkg/common"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	nonceRewritePrefixLen = 8
)

type AEADType uint8

const (
	AES128GCM AEADType = iota + 1
	AES256GCM
	ChaCha20Poly1305
	XChaCha20Poly1305
)

var (
	_ BlockCipher = &AEADBlockCipher{}
)

// AEADBlockCipher implements BlockCipher interface with one AEAD algorithm.
type AEADBlockCipher struct {
	aead                cipher.AEAD
	aeadType            AEADType
	enableImplicitNonce bool
	key                 []byte
	implicitNonce       []byte
	mu                  sync.Mutex
	ctx                 BlockContext
}

// newAESGCMBlockCipher creates a new AES-GCM cipher with the supplied key.
func newAESGCMBlockCipher(key []byte) (*AEADBlockCipher, error) {
	keyLen := len(key)
	if keyLen != 16 && keyLen != 32 {
		return nil, fmt.Errorf("AES key length is %d bytes, want 16 bytes or 32 bytes", keyLen)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher() failed: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM() failed: %w", err)
	}

	c := &AEADBlockCipher{
		aead:                aead,
		enableImplicitNonce: false,
		key:                 key,
		implicitNonce:       nil,
	}
	if keyLen == 16 {
		c.aeadType = AES128GCM
	} else if keyLen == 32 {
		c.aeadType = AES256GCM
	} else {
		panic(fmt.Sprintf("invalid AES key length: %d bytes", keyLen))
	}
	return c, nil
}

// newChaCha20Poly1305BlockCipher creates a new ChaCha20-Poly1305 cipher with the supplied key.
func newChaCha20Poly1305BlockCipher(key []byte) (*AEADBlockCipher, error) {
	keyLen := len(key)
	if keyLen != 32 {
		return nil, fmt.Errorf("ChaCha20-Poly1305 key length is %d bytes, want 32 bytes", keyLen)
	}

	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, fmt.Errorf("chacha20poly1305.New() failed: %w", err)
	}

	return &AEADBlockCipher{
		aead:                aead,
		aeadType:            ChaCha20Poly1305,
		enableImplicitNonce: false,
		key:                 key,
		implicitNonce:       nil,
	}, nil
}

// newXChaCha20Poly1305BlockCipher creates a new XChaCha20-Poly1305 cipher with the supplied key.
func newXChaCha20Poly1305BlockCipher(key []byte) (*AEADBlockCipher, error) {
	keyLen := len(key)
	if keyLen != 32 {
		return nil, fmt.Errorf("XChaCha20-Poly1305 key length is %d bytes, want 32 bytes", keyLen)
	}

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("chacha20poly1305.NewX() failed: %w", err)
	}

	return &AEADBlockCipher{
		aead:                aead,
		aeadType:            XChaCha20Poly1305,
		enableImplicitNonce: false,
		key:                 key,
		implicitNonce:       nil,
	}, nil
}

// BlockSize returns the block size of cipher.
func (*AEADBlockCipher) BlockSize() int {
	return aes.BlockSize
}

// NonceSize returns the number of bytes used by nonce.
func (c *AEADBlockCipher) NonceSize() int {
	return c.aead.NonceSize()
}

func (c *AEADBlockCipher) Overhead() int {
	return c.aead.Overhead()
}

func (c *AEADBlockCipher) Encrypt(plaintext []byte) ([]byte, error) {
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

func (c *AEADBlockCipher) EncryptWithNonce(plaintext, nonce []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.enableImplicitNonce {
		return nil, fmt.Errorf("EncryptWithNonce() is not supported when implicit nonce is enabled")
	}
	if len(nonce) != c.NonceSize() {
		return nil, fmt.Errorf("want nonce size %d, got %d", c.NonceSize(), len(nonce))
	}
	return c.aead.Seal(nil, nonce, plaintext, nil), nil
}

func (c *AEADBlockCipher) Decrypt(ciphertext []byte) ([]byte, error) {
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

func (c *AEADBlockCipher) DecryptWithNonce(ciphertext, nonce []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.enableImplicitNonce {
		return nil, fmt.Errorf("EncryptWithNonce() is not supported when implicit nonce is enabled")
	}
	if len(nonce) != c.NonceSize() {
		return nil, fmt.Errorf("want nonce size %d, got %d", c.NonceSize(), len(nonce))
	}
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("cipher.AEAD.Open() failed: %w", err)
	}
	return plaintext, nil
}

func (c *AEADBlockCipher) Clone() BlockCipher {
	c.mu.Lock()
	defer c.mu.Unlock()

	var newCipher *AEADBlockCipher
	var err error
	if c.aeadType == AES128GCM || c.aeadType == AES256GCM {
		newCipher, err = newAESGCMBlockCipher(c.key)
	} else if c.aeadType == ChaCha20Poly1305 {
		newCipher, err = newChaCha20Poly1305BlockCipher(c.key)
	} else if c.aeadType == XChaCha20Poly1305 {
		newCipher, err = newXChaCha20Poly1305BlockCipher(c.key)
	} else {
		panic("invalid AEAD type")
	}
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

func (c *AEADBlockCipher) SetImplicitNonceMode(enable bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enableImplicitNonce = enable
	if !enable {
		c.implicitNonce = nil
	}
}

func (c *AEADBlockCipher) IsStateless() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.enableImplicitNonce
}

func (c *AEADBlockCipher) BlockContext() BlockContext {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ctx
}

func (c *AEADBlockCipher) SetBlockContext(bc BlockContext) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ctx = bc
}

// newNonce generates a new nonce.
func (c *AEADBlockCipher) newNonce() ([]byte, error) {
	nonce := make([]byte, c.NonceSize())
	if _, err := crand.Read(nonce); err != nil {
		return nil, err
	}

	// Adjust the nonce such that the first a few bytes are printable ASCII characters.
	rewriteLen := nonceRewritePrefixLen
	if rewriteLen > c.NonceSize() {
		rewriteLen = c.NonceSize()
	}
	common.ToCommon64Set(nonce, 0, rewriteLen)
	return nonce, nil
}

func (c *AEADBlockCipher) increaseNonce() {
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
