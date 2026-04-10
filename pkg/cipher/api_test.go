// Copyright (C) 2026  mieru authors
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
	crand "crypto/rand"
	"crypto/sha256"
	"fmt"
	"testing"
)

func Benchmark10KUserStatefulTryDecrypt(b *testing.B) {
	const numUsers = 10000
	passwords := make([][]byte, numUsers)
	for i := 0; i < numUsers; i++ {
		passwords[i] = HashPassword(fmt.Appendf(nil, "password-%d", i), fmt.Appendf(nil, "user-%d", i))
	}

	data := make([]byte, 1500)
	if _, err := crand.Read(data); err != nil {
		b.Fatalf("failed to generate data: %v", err)
	}

	// Encrypt with a stateful cipher.
	block, err := BlockCipherFromPassword(passwords[numUsers-1], false)
	if err != nil {
		b.Fatalf("BlockCipherFromPassword() failed: %v", err)
	}
	ciphertext, err := block.Encrypt(data)
	if err != nil {
		b.Fatalf("Encrypt() failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < numUsers; j++ {
			_, _, err := TryDecrypt(ciphertext, passwords[j], false)
			if err == nil {
				break
			}
		}
	}
}

func Benchmark10KUserStatelessTryDecrypt(b *testing.B) {
	const numUsers = 10000
	passwords := make([][]byte, numUsers)
	for i := 0; i < numUsers; i++ {
		passwords[i] = HashPassword(fmt.Appendf(nil, "password-%d", i), fmt.Appendf(nil, "user-%d", i))
	}

	data := make([]byte, 1500)
	if _, err := crand.Read(data); err != nil {
		b.Fatalf("failed to generate data: %v", err)
	}

	// Encrypt with a stateless cipher.
	block, err := BlockCipherFromPassword(passwords[numUsers-1], true)
	if err != nil {
		b.Fatalf("BlockCipherFromPassword() failed: %v", err)
	}
	ciphertext, err := block.Encrypt(data)
	if err != nil {
		b.Fatalf("Encrypt() failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < numUsers; j++ {
			_, _, err := TryDecrypt(ciphertext, passwords[j], true)
			if err == nil {
				break
			}
		}
	}
}

func BenchmarkCheck10KUserFromHint(b *testing.B) {
	const numUsers = 10000
	users := make([][]byte, numUsers)
	for i := 0; i < numUsers; i++ {
		users[i] = fmt.Appendf(nil, "user-%d", i)
	}

	// Build a nonce that matches the last user.
	nonce := make([]byte, DefaultNonceSize)
	if _, err := crand.Read(nonce); err != nil {
		b.Fatalf("failed to generate nonce: %v", err)
	}
	input := append(users[numUsers-1], nonce[:noncePrefixLenForUserHint]...)
	output := sha256.Sum256(input)
	copy(nonce[DefaultNonceSize-nonceSuffixLenForUserHint:], output[:nonceSuffixLenForUserHint])

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < numUsers; j++ {
			if CheckUserFromHint(users[j], nonce) {
				break
			}
		}
	}
}
