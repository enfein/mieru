// Copyright (C) 2024  mieru authors
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
// along with this program.  If not, see <https://www.gnu.org/licenses/>

package cipher_test

import (
	crand "crypto/rand"
	"testing"

	"github.com/enfein/mieru/pkg/cipher"
)

func BenchmarkTryDecryptStateless(b *testing.B) {
	benchmarkTryDecrypt(b, true)
}

func BenchmarkTryDecryptStateful(b *testing.B) {
	benchmarkTryDecrypt(b, false)
}

func benchmarkTryDecrypt(b *testing.B, stateless bool) {
	password := make([]byte, 32)
	data := make([]byte, 1024)
	if _, err := crand.Read(password); err != nil {
		b.Fatalf("Generate password failed.")
	}
	if _, err := crand.Read(data); err != nil {
		b.Fatalf("Generate data failed.")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cipher.TryDecrypt(data, password, stateless)
	}
}
