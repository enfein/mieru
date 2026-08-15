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
	"bytes"
	crand "crypto/rand"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/enfein/mieru/v3/apis/constant"
)

func TestUserHintOperationsAllocateNoHeap(t *testing.T) {
	user := []byte("allocation-free-user")
	key := make([]byte, DefaultKeyLen)
	c, err := newXChaCha20Poly1305BlockCipher(key)
	if err != nil {
		t.Fatalf("newXChaCha20Poly1305BlockCipher() failed: %v", err)
	}
	c.SetBlockContext(BlockContext{UserName: string(user)})
	var baseNonce [DefaultNonceSize]byte
	nonce := c.addUserHintToNonce(baseNonce[:])
	if !CheckUserFromHint(user, nonce) {
		t.Fatal("generated user hint did not match")
	}

	if allocs := testing.AllocsPerRun(1000, func() {
		if !CheckUserFromHint(user, nonce) {
			panic("user hint did not match")
		}
	}); allocs != 0 {
		t.Fatalf("CheckUserFromHint() allocations = %v, want 0", allocs)
	}

	if allocs := testing.AllocsPerRun(1000, func() {
		localNonce := baseNonce
		c.addUserHintToNonce(localNonce[:])
	}); allocs != 0 {
		t.Fatalf("addUserHintToNonce() allocations = %v, want 0", allocs)
	}
}

func TestUserHintOperationsPanicForLongUserName(t *testing.T) {
	user := strings.Repeat("x", constant.MaxUserNameLen+1)
	var nonce [DefaultNonceSize]byte
	tests := []struct {
		name string
		fn   func()
	}{
		{
			name: "CheckUserFromHint",
			fn: func() {
				CheckUserFromHint([]byte(user), nonce[:])
			},
		},
		{
			name: "addUserHintToNonce",
			fn: func() {
				c := &aeadBlockCipher{ctx: BlockContext{UserName: user}}
				c.addUserHintToNonce(nonce[:])
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("operation did not panic")
				}
			}()
			test.fn()
		})
	}
}

func TestConcurrentStatelessTrialsReturnIndependentCiphers(t *testing.T) {
	password := HashPassword([]byte(t.Name()), []byte("parallel-user"))
	block, err := BlockCipherFromPassword(password, true)
	if err != nil {
		t.Fatalf("BlockCipherFromPassword() failed: %v", err)
	}
	plaintext := []byte("parallel plaintext")
	ciphertext, err := block.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() failed: %v", err)
	}

	const trials = 64
	var wg sync.WaitGroup
	errCh := make(chan error, trials)
	for i := 0; i < trials; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			winner, _, err := TryDecrypt(ciphertext, password, true)
			if err != nil {
				errCh <- err
				return
			}
			if got := winner.BlockContext().UserName; got != "" {
				errCh <- fmt.Errorf("trial %d inherited context %q", i, got)
				return
			}
			want := fmt.Sprintf("trial-%d", i)
			winner.SetBlockContext(BlockContext{UserName: want})
			if got := winner.BlockContext().UserName; got != want {
				errCh <- fmt.Errorf("trial %d context = %q, want %q", i, got, want)
				return
			}
			nonce := make([]byte, DefaultNonceSize)
			nonce[len(nonce)-1] = byte(i)
			sealed, err := winner.EncryptWithNonce(plaintext, nonce)
			if err != nil {
				errCh <- fmt.Errorf("trial %d EncryptWithNonce() failed: %w", i, err)
				return
			}
			opened, err := winner.DecryptWithNonce(sealed, nonce)
			if err != nil {
				errCh <- fmt.Errorf("trial %d DecryptWithNonce() failed: %w", i, err)
				return
			}
			if !bytes.Equal(opened, plaintext) {
				errCh <- fmt.Errorf("trial %d direct plaintext = %q, want %q", i, opened, plaintext)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}

	templates, err := getBlockCipherList(string(password), true)
	if err != nil {
		t.Fatalf("getBlockCipherList() failed: %v", err)
	}
	for i, template := range templates {
		if got := template.BlockContext().UserName; got != "" {
			t.Fatalf("cached template %d context = %q after concurrent trials, want empty", i, got)
		}
	}
}

// The cached XChaCha20-Poly1305 templates are immutable. Concurrent Open
// calls share only their read-only key and create all working state per call.
func TestConcurrentPreparedStatelessTrialsAreRaceFree(t *testing.T) {
	password := HashPassword([]byte(t.Name()), []byte("parallel-prepared-user"))
	block, err := BlockCipherFromPassword(password, true)
	if err != nil {
		t.Fatalf("BlockCipherFromPassword() failed: %v", err)
	}
	plaintext := []byte("parallel prepared plaintext")
	ciphertext, err := block.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() failed: %v", err)
	}
	decryptor, err := NewStatelessDecryptor(password)
	if err != nil {
		t.Fatalf("NewStatelessDecryptor() failed: %v", err)
	}

	const trials = 64
	var wg sync.WaitGroup
	errCh := make(chan error, trials)
	for i := 0; i < trials; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			winner, got, err := decryptor.TryDecrypt(ciphertext, make([]byte, 0, len(plaintext)))
			if err != nil {
				errCh <- err
				return
			}
			if !bytes.Equal(got, plaintext) {
				errCh <- fmt.Errorf("trial %d plaintext = %q, want %q", i, got, plaintext)
				return
			}
			want := fmt.Sprintf("prepared-trial-%d", i)
			winner.SetBlockContext(BlockContext{UserName: want})
			if got := winner.BlockContext().UserName; got != want {
				errCh <- fmt.Errorf("trial %d context = %q, want %q", i, got, want)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}

	entry := decryptor.ciphers.Load()
	if entry == nil {
		t.Fatal("prepared decryptor did not retain trial material")
	}
	for i, template := range entry.cipherList {
		if got := template.BlockContext().UserName; got != "" {
			t.Fatalf("cached template %d context = %q after concurrent trials, want empty", i, got)
		}
	}
}

func BenchmarkTryDecrypt(b *testing.B) {
	password := HashPassword([]byte("benchmark-password"), []byte("benchmark-user"))
	data := make([]byte, 1500)
	if _, err := crand.Read(data); err != nil {
		b.Fatalf("failed to generate data: %v", err)
	}

	for _, benchmark := range []struct {
		name      string
		stateless bool
	}{
		{name: "Stateful", stateless: false},
		{name: "Stateless", stateless: true},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			block, err := BlockCipherFromPassword(password, benchmark.stateless)
			if err != nil {
				b.Fatalf("BlockCipherFromPassword() failed: %v", err)
			}
			ciphertext, err := block.Encrypt(data)
			if err != nil {
				b.Fatalf("Encrypt() failed: %v", err)
			}

			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				winner, plaintext, err := TryDecrypt(ciphertext, password, benchmark.stateless)
				if err != nil || winner == nil || len(plaintext) != len(data) {
					b.Fatalf("TryDecrypt() = (block nil=%t, plaintext bytes=%d, %v)", winner == nil, len(plaintext), err)
				}
			}
		})
	}
}

func BenchmarkCheckUserFromHint(b *testing.B) {
	user := []byte("benchmark-user")
	key := make([]byte, 32)
	if _, err := crand.Read(key); err != nil {
		b.Fatalf("fail to generate key: %v", err)
	}
	c, err := newXChaCha20Poly1305BlockCipher(key)
	if err != nil {
		b.Fatalf("newXChaCha20Poly1305BlockCipher() failed: %v", err)
	}
	c.SetBlockContext(BlockContext{UserName: string(user)})
	nonce, err := c.newNonce()
	if err != nil {
		b.Fatalf("newNonce() failed: %v", err)
	}
	nonce = c.addUserHintToNonce(nonce)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !CheckUserFromHint(user, nonce) {
			b.Fatal("user hint did not match")
		}
	}
}
