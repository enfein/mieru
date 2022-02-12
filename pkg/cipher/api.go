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
func BlockCipherFromPassword(password []byte) (BlockCipher, error) {
	cipherList, err := getBlockCipherList(password)
	if err != nil {
		return nil, err
	}
	return cipherList[1], nil
}

// BlockCipherListFromPassword creates three BlockCipher objects using different salts
// from the password with the default settings.
func BlockCipherListFromPassword(password []byte) ([]BlockCipher, error) {
	return getBlockCipherList(password)
}
