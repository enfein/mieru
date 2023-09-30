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
	"crypto/sha256"
	"fmt"

	"github.com/enfein/mieru/pkg/metrics"
)

const (
	DefaultNonceSize = 12 // 12 bytes
	DefaultOverhead  = 16 // 16 bytes
	DefaultKeyLen    = 32 // 256 bits

	ClientDecryptionMetricGroupName = "cipher - client"
	ServerDecryptionMetricGroupName = "cipher - server"
)

var (
	// Number of decryption using the cipher block associated with the connection.
	ClientDirectDecrypt = metrics.RegisterMetric(ClientDecryptionMetricGroupName, "DirectDecrypt")

	// Number of decryption using the stored cipher block but failed.
	ClientFailedDirectDecrypt = metrics.RegisterMetric(ClientDecryptionMetricGroupName, "FailedDirectDecrypt")

	// Number of decryption using the cipher block associated with the connection.
	ServerDirectDecrypt = metrics.RegisterMetric(ServerDecryptionMetricGroupName, "DirectDecrypt")

	// Number of decryption using the stored cipher block but failed.
	ServerFailedDirectDecrypt = metrics.RegisterMetric(ServerDecryptionMetricGroupName, "FailedDirectDecrypt")

	// Number of decryption that iterates all possible cipher blocks.
	ServerIterateDecrypt = metrics.RegisterMetric(ServerDecryptionMetricGroupName, "IterateDecrypt")

	// Number of decryption that failed after iterating all possible cipher blocks.
	ServerFailedIterateDecrypt = metrics.RegisterMetric(ServerDecryptionMetricGroupName, "FailedIterateDecrypt")
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

	NonceSize() int

	Overhead() int

	// Clone method creates a deep copy of block cipher itself.
	// Panic if this operation fails.
	Clone() BlockCipher

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
	cipherList, err := getBlockCipherList(password, stateless)
	if err != nil {
		return nil, err
	}
	return cipherList[1], nil
}

// BlockCipherListFromPassword creates three BlockCipher objects using different salts
// from the password with the default settings.
func BlockCipherListFromPassword(password []byte, stateless bool) ([]BlockCipher, error) {
	return getBlockCipherList(password, stateless)
}

// TryDecrypt tries to decrypt the data with all possible keys generated from the password.
// If successful, returns the block cipher as well as the decrypted results.
func TryDecrypt(data, password []byte, stateless bool) (BlockCipher, []byte, error) {
	blocks, err := BlockCipherListFromPassword(password, stateless)
	if err != nil {
		return nil, nil, fmt.Errorf("BlockCipherListFromPassword() failed: %w", err)
	}
	return SelectDecrypt(data, blocks)
}

// SelectDecrypt returns the appropriate cipher block that can decrypt the data,
// as well as the decrypted result.
func SelectDecrypt(data []byte, blocks []BlockCipher) (BlockCipher, []byte, error) {
	for _, block := range blocks {
		decrypted, err := block.Decrypt(data)
		if err != nil {
			continue
		}
		return block, decrypted, nil
	}

	return nil, nil, fmt.Errorf("unable to decrypt from supplied %d cipher blocks", len(blocks))
}

// CloneBlockCiphers clones a slice of block ciphers.
func CloneBlockCiphers(blocks []BlockCipher) []BlockCipher {
	clones := make([]BlockCipher, len(blocks))
	for i, b := range blocks {
		clones[i] = b.Clone()
	}
	return clones
}
