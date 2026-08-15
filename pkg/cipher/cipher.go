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
	"crypto/cipher"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	mrand "math/rand"
	"sync"

	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/common"
	"golang.org/x/crypto/chacha20poly1305"
	"google.golang.org/protobuf/proto"
)

type AEADType uint8

const (
	// Not supported.
	AES128GCM AEADType = iota + 1
	// Not supported.
	AES256GCM
	// Not supported.
	ChaCha20Poly1305
	// Supported.
	XChaCha20Poly1305
)

var (
	_ BlockCipher = &aeadBlockCipher{}

	errCiphertextTooShort = errors.New("ciphertext is smaller than nonce size")
)

// aeadBlockCipher implements BlockCipher interface with one AEAD algorithm.
type aeadBlockCipher struct {
	aead                cipher.AEAD
	aeadType            AEADType
	key                 [DefaultKeyLen]byte
	enableImplicitNonce bool
	implicitNonce       []byte
	mu                  sync.Mutex
	ctx                 BlockContext
	noncePattern        *appctlpb.NoncePattern
	noncePatternApplied bool
}

// newXChaCha20Poly1305BlockCipher creates a new XChaCha20-Poly1305 cipher with the supplied key.
func newXChaCha20Poly1305BlockCipher(key []byte) (*aeadBlockCipher, error) {
	keyLen := len(key)
	if keyLen != 32 {
		return nil, fmt.Errorf("XChaCha20-Poly1305 key length is %d bytes, want 32 bytes", keyLen)
	}

	var ownedKey [DefaultKeyLen]byte
	copy(ownedKey[:], key)
	aead, err := chacha20poly1305.NewX(ownedKey[:])
	if err != nil {
		return nil, fmt.Errorf("chacha20poly1305.NewX() failed: %w", err)
	}

	return &aeadBlockCipher{
		aead:                aead,
		aeadType:            XChaCha20Poly1305,
		enableImplicitNonce: false,
		key:                 ownedKey,
		implicitNonce:       nil,
	}, nil
}

// NonceSize returns the number of bytes used by nonce.
func (c *aeadBlockCipher) NonceSize() int {
	return c.aead.NonceSize()
}

func (c *aeadBlockCipher) Overhead() int {
	return c.aead.Overhead()
}

func (c *aeadBlockCipher) Encrypt(plaintext []byte) ([]byte, error) {
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
			c.implicitNonce = c.addUserHintToNonce(c.implicitNonce)
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
		nonce = c.addUserHintToNonce(nonce)
	}

	dst := c.aead.Seal(nil, nonce, plaintext, nil)
	if needSendNonce {
		return append(nonce, dst...), nil
	}
	return dst, nil
}

func (c *aeadBlockCipher) EncryptWithNonce(plaintext, nonce []byte) ([]byte, error) {
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

func (c *aeadBlockCipher) Decrypt(ciphertext []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var nonce []byte
	if c.enableImplicitNonce {
		if len(c.implicitNonce) == 0 {
			if len(ciphertext) < c.NonceSize() {
				return nil, errCiphertextTooShort
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
			return nil, errCiphertextTooShort
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

func (c *aeadBlockCipher) DecryptWithNonce(ciphertext, nonce []byte) ([]byte, error) {
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

func (c *aeadBlockCipher) DecryptStatelessTo(ciphertext, dst []byte) ([]byte, error) {
	if len(ciphertext) < c.aead.NonceSize() {
		return nil, errCiphertextTooShort
	}
	nonceSize := c.aead.NonceSize()
	return c.aead.Open(dst, ciphertext[:nonceSize], ciphertext[nonceSize:], nil)
}

func (c *aeadBlockCipher) Clone() BlockCipher {
	c.mu.Lock()
	defer c.mu.Unlock()

	var newCipher *aeadBlockCipher
	var err error
	if c.aeadType == XChaCha20Poly1305 {
		newCipher, err = newXChaCha20Poly1305BlockCipher(c.key[:])
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
	newCipher.noncePattern = proto.Clone(c.noncePattern).(*appctlpb.NoncePattern)
	return newCipher
}

func (c *aeadBlockCipher) CloneStatelessFast() BlockCipher {
	return c.cloneStatelessFast()
}

func (c *aeadBlockCipher) SetImplicitNonceMode(enable bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enableImplicitNonce = enable
	if !enable {
		c.implicitNonce = nil
	}
}

func (c *aeadBlockCipher) IsStateless() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.enableImplicitNonce
}

func (c *aeadBlockCipher) BlockContext() BlockContext {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ctx
}

func (c *aeadBlockCipher) SetBlockContext(bc BlockContext) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ctx = bc
}

func (c *aeadBlockCipher) NoncePattern() *appctlpb.NoncePattern {
	c.mu.Lock()
	defer c.mu.Unlock()
	return proto.Clone(c.noncePattern).(*appctlpb.NoncePattern)
}

func (c *aeadBlockCipher) SetNoncePattern(pattern *appctlpb.NoncePattern) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.noncePattern = proto.Clone(pattern).(*appctlpb.NoncePattern)
}

// newNonce generates a new nonce.
func (c *aeadBlockCipher) newNonce() ([]byte, error) {
	nonce := make([]byte, c.NonceSize())
	if _, err := crand.Read(nonce); err != nil {
		return nil, err
	}

	if c.noncePattern == nil {
		return nonce, nil
	}

	// For UDP (stateless) cipher, if the pattern was already applied
	// and applyToAllUDPPacket is false, don't change the nonce.
	if !c.enableImplicitNonce && c.noncePatternApplied && !c.noncePattern.GetApplyToAllUDPPacket() {
		return nonce, nil
	}

	switch c.noncePattern.GetType() {
	case appctlpb.NonceType_NONCE_TYPE_RANDOM:
		// No change to the nonce.
	case appctlpb.NonceType_NONCE_TYPE_PRINTABLE:
		rewriteLen := c.nonceRewriteLen()
		common.ToPrintableChar(nonce, 0, rewriteLen)
	case appctlpb.NonceType_NONCE_TYPE_PRINTABLE_SUBSET:
		rewriteLen := c.nonceRewriteLen()
		common.ToCommon64Set(nonce, 0, rewriteLen)
	case appctlpb.NonceType_NONCE_TYPE_FIXED:
		hexStrings := c.noncePattern.GetCustomHexStrings()
		if len(hexStrings) > 0 {
			idx := mrand.Intn(len(hexStrings))
			prefix, err := hex.DecodeString(hexStrings[idx])
			if err == nil {
				copyLen := len(prefix)
				if copyLen > c.NonceSize() {
					copyLen = c.NonceSize()
				}
				copy(nonce[:copyLen], prefix[:copyLen])
			} else {
				panic(fmt.Errorf("fail to decode hex string: %w", err))
			}
		}
		// If no hex strings are provided, use plain random nonce.
	}

	c.noncePatternApplied = true
	return nonce, nil
}

// nonceRewriteLen returns a random length in [minLen, maxLen] clamped to the nonce size.
func (c *aeadBlockCipher) nonceRewriteLen() int {
	minLen := int(c.noncePattern.GetMinLen())
	maxLen := int(c.noncePattern.GetMaxLen())
	if maxLen > c.NonceSize() {
		maxLen = c.NonceSize()
	}
	if minLen > maxLen {
		minLen = maxLen
	}
	if minLen == maxLen {
		return minLen
	}
	rangeSize := mrand.Intn(maxLen - minLen + 1)
	return minLen + rangeSize
}

func (c *aeadBlockCipher) increaseNonce() {
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

func (c *aeadBlockCipher) cloneStatelessFast() *aeadBlockCipher {
	return &aeadBlockCipher{
		aead:     c.aead,
		aeadType: c.aeadType,
		key:      c.key,
	}
}

func (c *aeadBlockCipher) addUserHintToNonce(nonce []byte) []byte {
	if c.ctx.UserName == "" {
		return nonce
	}
	if len(c.ctx.UserName) > constant.MaxUserNameLen {
		panic(fmt.Sprintf("user name length %d exceeds maximum %d", len(c.ctx.UserName), constant.MaxUserNameLen))
	}
	if len(nonce) < NoncePrefixLenForUserHint+NonceSuffixLenForUserHint {
		panic(fmt.Sprintf("nonce length %d is too short", len(nonce)))
	}
	var input [constant.MaxUserNameLen + NoncePrefixLenForUserHint]byte
	n := copy(input[:], c.ctx.UserName)
	n += copy(input[n:], nonce[:NoncePrefixLenForUserHint])
	output := sha256.Sum256(input[:n])
	copy(nonce[len(nonce)-NonceSuffixLenForUserHint:], output[:NonceSuffixLenForUserHint])
	return nonce
}

// selectDecrypt returns the appropriate cipher block that can decrypt the data,
// as well as the decrypted result.
func selectDecrypt(data []byte, blocks []*aeadBlockCipher) (*aeadBlockCipher, []byte, error) {
	for _, block := range blocks {
		decrypted, err := block.Decrypt(data)
		if err != nil {
			continue
		}
		return block, decrypted, nil
	}

	return nil, nil, fmt.Errorf("unable to decrypt from supplied %d cipher blocks", len(blocks))
}

func selectDecryptStateless(ciphertext, dst []byte, blocks []*aeadBlockCipher) (*aeadBlockCipher, []byte, error) {
	if dst == nil && len(ciphertext) >= DefaultNonceSize+DefaultOverhead {
		dst = make([]byte, 0, len(ciphertext)-DefaultNonceSize-DefaultOverhead)
	}
	for _, block := range blocks {
		decrypted, err := block.DecryptStatelessTo(ciphertext, dst)
		if err != nil {
			continue
		}
		return block, decrypted, nil
	}
	return nil, nil, errUnableToDecrypt
}
