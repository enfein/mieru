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
	"time"
)

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
	keygen := PBKDF2Gen{
		Salt: SaltFromTime(time.Now())[1],
		Iter: DefaultIter,
	}
	cipherKey, err := keygen.NewKey(password, DefaultKeyLen)
	if err != nil {
		return nil, fmt.Errorf("NewKey() failed: %w", err)
	}
	blockCipher, err := NewAESGCMBlockCipher(cipherKey)
	if err != nil {
		return nil, fmt.Errorf("NewAESGCMBlockCipher() failed: %w", err)
	}
	return blockCipher, nil
}

// BlockCipherListFromPassword creates three BlockCipher objects using different salts
// from the password with the default settings.
func BlockCipherListFromPassword(password []byte) ([]BlockCipher, error) {
	salts := SaltFromTime(time.Now())
	blockCiphers := make([]BlockCipher, 0, 3)
	for i := 0; i < 3; i++ {
		keygen := PBKDF2Gen{
			Salt: salts[i],
			Iter: DefaultIter,
		}
		cipherKey, err := keygen.NewKey(password, DefaultKeyLen)
		if err != nil {
			return nil, fmt.Errorf("NewKey() failed: %w", err)
		}
		blockCipher, err := NewAESGCMBlockCipher(cipherKey)
		if err != nil {
			return nil, fmt.Errorf("NewAESGCMBlockCipher() failed: %w", err)
		}
		blockCiphers = append(blockCiphers, blockCipher)
	}
	return blockCiphers, nil
}
