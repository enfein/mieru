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
	"crypto/sha256"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/metrics"
)

const (
	DefaultNonceSize = 24 // 24 bytes. In mieru v2, the value was 12.
	DefaultOverhead  = 16 // 16 bytes
	DefaultKeyLen    = 32 // 256 bits

	NoncePrefixLenForUserHint = 16 // 16 bytes user hint input
	NonceSuffixLenForUserHint = 4  // 4 bytes user hint output

	ClientDecryptionMetricGroupName = "cipher - client"
	ServerDecryptionMetricGroupName = "cipher - server"
)

var (
	// Number of decryption using the cipher block associated with the connection.
	ClientDirectDecrypt = metrics.RegisterMetric(ClientDecryptionMetricGroupName, "DirectDecrypt", metrics.COUNTER)

	// Number of decryption using the stored cipher block but failed.
	ClientFailedDirectDecrypt = metrics.RegisterMetric(ClientDecryptionMetricGroupName, "FailedDirectDecrypt", metrics.COUNTER)

	// Number of decryption using the cipher block associated with the connection.
	ServerDirectDecrypt = metrics.RegisterMetric(ServerDecryptionMetricGroupName, "DirectDecrypt", metrics.COUNTER)

	// Number of decryption using the stored cipher block but failed.
	ServerFailedDirectDecrypt = metrics.RegisterMetric(ServerDecryptionMetricGroupName, "FailedDirectDecrypt", metrics.COUNTER)

	// Number of decryption that iterates all possible cipher blocks.
	ServerIterateDecrypt = metrics.RegisterMetric(ServerDecryptionMetricGroupName, "IterateDecrypt", metrics.COUNTER)

	// Number of decryption that failed after iterating all possible cipher blocks.
	ServerFailedIterateDecrypt = metrics.RegisterMetric(ServerDecryptionMetricGroupName, "FailedIterateDecrypt", metrics.COUNTER)

	// Number of decryption where a user hint match was found.
	ServerHintMatchDecrypt = metrics.RegisterMetric(ServerDecryptionMetricGroupName, "HintMatchDecrypt", metrics.COUNTER)

	// Number of decryption where a user hint match was found but decryption failed.
	ServerFailedHintMatchDecrypt = metrics.RegisterMetric(ServerDecryptionMetricGroupName, "FailedHintMatchDecrypt", metrics.COUNTER)
)

// BlockCipher is an interface of block encryption and decryption.
type BlockCipher interface {
	// Encrypt method adds the nonce in the dst, then encryptes the src.
	Encrypt(plaintext []byte) ([]byte, error)

	// EncryptWithNonce encrypts the src with the given nonce.
	// This method is not supported by stateful BlockCipher.
	EncryptWithNonce(plaintext, nonce []byte) ([]byte, error)

	// Decrypt method removes the nonce in the src, then decryptes the src.
	Decrypt(ciphertext []byte) ([]byte, error)

	// DecryptWithNonce decrypts the src with the given nonce.
	// This method is not supported by stateful BlockCipher.
	DecryptWithNonce(ciphertext, nonce []byte) ([]byte, error)

	// DecryptStatelessTo decrypts ciphertext with its prepended nonce and
	// appends the plaintext to dst.
	// It MUST only be called on a stateless cipher that is not being mutated
	// concurrently.
	// This code is performance sensitive and may not check all invariants.
	DecryptStatelessTo(ciphertext, dst []byte) ([]byte, error)

	// NonceSize returns the size of the nonce that must be passed to Seal
	// and Open.
	NonceSize() int

	// Overhead returns the maximum difference between the lengths of a
	// plaintext and its ciphertext.
	Overhead() int

	// Clone method creates a deep copy of block cipher itself.
	// Panic if this operation fails.
	Clone() BlockCipher

	// CloneStatelessFast creates a new BlockCipher.
	// The BlockCipher being cloned must be stateless and not being mutated
	// concurrently.
	// BlockContext and NoncePattern are NOT cloned.
	// This code is performance sensitive and may not check all invariants.
	CloneStatelessFast() BlockCipher

	// SetImplicitNonceMode enables or disables implicit nonce mode.
	// Under implicit nonce mode, the nonce is set exactly once on the first
	// Encrypt() or Decrypt() call. After that, all Encrypt() or Decrypt()
	// calls will not look up nonce in the data. Each Encrypt() or Decrypt()
	// will cause the nonce value to be increased by 1.
	//
	// Implicit nonce mode is disabled by default.
	//
	// Disabling implicit nonce mode removes the implicit nonce (state)
	// from the block cipher.
	SetImplicitNonceMode(enable bool)

	// IsStateless returns true if the BlockCipher can do arbitrary Encrypt()
	// and Decrypt() in any sequence.
	IsStateless() bool

	// BlockContext returns a copy of BlockContext.
	BlockContext() BlockContext

	// SetBlockContext sets the BlockContext.
	SetBlockContext(bc BlockContext)

	// NoncePattern returns a copy of NoncePattern associated with the cipher block.
	NoncePattern() *appctlpb.NoncePattern

	// SetNoncePattern sets the NoncePattern associated with the cipher block.
	SetNoncePattern(pattern *appctlpb.NoncePattern)
}

// BlockContext contains optional context associated to a cipher block.
type BlockContext struct {
	UserName string
}

// HashPassword generates a hashed password from
// the raw password and a unique value that decorates the password.
func HashPassword(rawPassword, uniqueValue []byte) []byte {
	p := append(rawPassword, 0x00) // 0x00 separates the password and username.
	p = append(p, uniqueValue...)
	hashed := sha256.Sum256(p)
	return hashed[:]
}

// BlockCipherFromPassword creates a BlockCipher object from the password
// with the default settings.
func BlockCipherFromPassword(password []byte, stateless bool) (BlockCipher, error) {
	entry, err := getCachedCiphers(string(password), time.Now())
	if err != nil {
		return nil, err
	}
	block := entry.cipherList[1].CloneStatelessFast()
	if !stateless {
		block.SetImplicitNonceMode(true)
	}
	return block, nil
}

// BlockCipherListFromPassword creates three BlockCipher objects using different salts
// from the password with the default settings.
func BlockCipherListFromPassword(password []byte, stateless bool) ([]BlockCipher, error) {
	entry, err := getCachedCiphers(string(password), time.Now())
	if err != nil {
		return nil, err
	}
	blocks := make([]BlockCipher, len(entry.cipherList))
	for i, template := range entry.cipherList {
		blocks[i] = template.CloneStatelessFast()
		if !stateless {
			blocks[i].SetImplicitNonceMode(true)
		}
	}
	return blocks, nil
}

// TryDecrypt tries to decrypt the data with all possible keys generated from the password.
// If successful, returns the block cipher as well as the decrypted results.
func TryDecrypt(data, password []byte, stateless bool) (BlockCipher, []byte, error) {
	if stateless {
		entry, err := getCachedCiphers(string(password), time.Now())
		if err != nil {
			return nil, nil, fmt.Errorf("getBlockCipherList() failed: %w", err)
		}
		block, plaintext, err := selectDecryptStateless(data, nil, entry.cipherList)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to decrypt from supplied %d cipher blocks", len(entry.cipherList))
		}
		return block.CloneStatelessFast(), plaintext, nil
	}

	// stateful
	blocks, err := getBlockCipherList(string(password), stateless)
	if err != nil {
		return nil, nil, fmt.Errorf("getBlockCipherList() failed: %w", err)
	}
	block, plaintext, err := selectDecrypt(data, blocks)
	if err != nil {
		return nil, nil, err
	}
	return block, plaintext, nil
}

// CheckUserFromHint checks if the user is the one associated with the nonce.
// It panics if the user is empty or too long, or the nonce is too short.
func CheckUserFromHint(user, nonce []byte) bool {
	if len(user) == 0 {
		panic("user is empty")
	}
	if len(user) > constant.MaxUserNameLen {
		panic(fmt.Sprintf("user name length %d exceeds maximum %d", len(user), constant.MaxUserNameLen))
	}
	if len(nonce) < NoncePrefixLenForUserHint+NonceSuffixLenForUserHint {
		panic(fmt.Sprintf("nonce length %d is too short", len(nonce)))
	}
	var input [constant.MaxUserNameLen + NoncePrefixLenForUserHint]byte
	n := copy(input[:], user)
	n += copy(input[n:], nonce[:NoncePrefixLenForUserHint])
	output := sha256.Sum256(input[:n])
	return bytes.Equal(output[:NonceSuffixLenForUserHint], nonce[len(nonce)-NonceSuffixLenForUserHint:])
}

// NewStatelessDecryptor builds a StatelessDecryptor for the given password.
func NewStatelessDecryptor(password []byte) (*StatelessDecryptor, error) {
	if len(password) == 0 {
		return nil, fmt.Errorf("password is empty")
	}
	return &StatelessDecryptor{password: string(password)}, nil
}

// StatelessDecryptor is optimized and safe for concurrent decryption.
type StatelessDecryptor struct {
	password string
	ciphers  atomic.Pointer[cachedCiphers]
}

func (d *StatelessDecryptor) TryDecrypt(ciphertext, dst []byte) (BlockCipher, []byte, error) {
	return d.tryDecryptAt(ciphertext, dst, time.Now())
}

func (d *StatelessDecryptor) tryDecryptAt(ciphertext, dst []byte, now time.Time) (BlockCipher, []byte, error) {
	if d == nil {
		return nil, nil, fmt.Errorf("stateless decryptor is nil")
	}
	epoch := cipherKeyEpoch(now)
	entry := d.ciphers.Load()
	if entry == nil || entry.epoch != epoch {
		var err error
		entry, err = getCachedCiphers(d.password, now)
		if err != nil {
			return nil, nil, err
		}
		d.ciphers.Store(entry)
	}
	block, plaintext, err := selectDecryptStateless(ciphertext, dst, entry.cipherList)
	if err != nil {
		return nil, nil, err
	}
	return block.CloneStatelessFast(), plaintext, nil
}

func cipherKeyEpoch(t time.Time) int64 {
	return t.Round(KeyRefreshInterval).Unix()
}
