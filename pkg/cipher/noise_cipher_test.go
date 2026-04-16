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

package cipher_test

import (
	"bytes"
	"net"
	"sync"
	"testing"

	"github.com/enfein/mieru/v3/pkg/cipher"
	noisepkg "github.com/enfein/mieru/v3/pkg/cipher/noise"
)

// completeHandshake runs an XX handshake between two goroutines and
// returns both sides' transport ciphers, wired through net.Pipe.
func completeHandshake(t *testing.T) (*noisepkg.TransportCiphers, *noisepkg.TransportCiphers) {
	t.Helper()

	kpI, err := noisepkg.GenerateKeypair(noisepkg.DH25519)
	if err != nil {
		t.Fatalf("keypair I: %v", err)
	}
	kpR, err := noisepkg.GenerateKeypair(noisepkg.DH25519)
	if err != nil {
		t.Fatalf("keypair R: %v", err)
	}

	iCfg := noisepkg.Config{
		Pattern: noisepkg.PatternXX, DH: noisepkg.DH25519,
		Cipher: noisepkg.CipherChaChaPoly, Hash: noisepkg.HashSHA256,
		Role: noisepkg.RoleInitiator, LocalStatic: kpI,
	}
	rCfg := noisepkg.Config{
		Pattern: noisepkg.PatternXX, DH: noisepkg.DH25519,
		Cipher: noisepkg.CipherChaChaPoly, Hash: noisepkg.HashSHA256,
		Role: noisepkg.RoleResponder, LocalStatic: kpR,
	}

	cConn, sConn := net.Pipe()
	t.Cleanup(func() { cConn.Close(); sConn.Close() })

	var wg sync.WaitGroup
	var iT, rT *noisepkg.TransportCiphers
	var iErr, rErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		hs, e := noisepkg.NewHandshake(iCfg)
		if e != nil {
			iErr = e
			return
		}
		iT, iErr = hs.Run(cConn, nil)
	}()
	go func() {
		defer wg.Done()
		hs, e := noisepkg.NewHandshake(rCfg)
		if e != nil {
			rErr = e
			return
		}
		rT, rErr = hs.Run(sConn, nil)
	}()
	wg.Wait()
	if iErr != nil || rErr != nil {
		t.Fatalf("handshake errors: i=%v r=%v", iErr, rErr)
	}
	return iT, rT
}

// TestNoiseBlockCipher_RoundTrip exercises encrypt/decrypt through the
// BlockCipher wrapper.
func TestNoiseBlockCipher_RoundTrip(t *testing.T) {
	iT, rT := completeHandshake(t)

	send := cipher.NewNoiseBlockCipher(iT.Send)
	recv := cipher.NewNoiseBlockCipher(rT.Recv)

	for _, msg := range [][]byte{
		[]byte("a"),
		[]byte("hello mieru"),
		bytes.Repeat([]byte{0x5A}, 1024),
	} {
		ct, err := send.Encrypt(msg)
		if err != nil {
			t.Fatalf("Encrypt: %v", err)
		}
		pt, err := recv.Decrypt(ct)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if !bytes.Equal(pt, msg) {
			t.Fatalf("round-trip mismatch")
		}
	}
}

// TestNoiseBlockCipher_NonceSize matches Overhead and NonceSize to the
// documented values.
func TestNoiseBlockCipher_SizeInterface(t *testing.T) {
	iT, _ := completeHandshake(t)
	bc := cipher.NewNoiseBlockCipher(iT.Send)
	if got := bc.NonceSize(); got != 8 {
		t.Errorf("NonceSize = %d, want 8", got)
	}
	if got := bc.Overhead(); got != 16 {
		t.Errorf("Overhead = %d, want 16", got)
	}
	if bc.IsStateless() {
		t.Errorf("IsStateless = true, want false")
	}
}

// TestNoiseBlockCipher_NonceMethodsError confirms the ExplicitNonce
// methods reject any input.
func TestNoiseBlockCipher_NonceMethodsError(t *testing.T) {
	iT, _ := completeHandshake(t)
	bc := cipher.NewNoiseBlockCipher(iT.Send)
	if _, err := bc.EncryptWithNonce([]byte("x"), make([]byte, 8)); err == nil {
		t.Errorf("EncryptWithNonce must return error")
	}
	if _, err := bc.DecryptWithNonce([]byte("x"), make([]byte, 8)); err == nil {
		t.Errorf("DecryptWithNonce must return error")
	}
}

// TestNoiseBlockCipher_CloneRejects ensures Clone panics instead of
// silently forking a stateful cipher.
func TestNoiseBlockCipher_CloneRejects(t *testing.T) {
	iT, _ := completeHandshake(t)
	bc := cipher.NewNoiseBlockCipher(iT.Send)
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected Clone to panic")
		}
	}()
	_ = bc.Clone()
}

// TestNoiseBlockCipher_DisableImplicitNoncePanics protects against a
// caller accidentally flipping the cipher out of implicit mode.
func TestNoiseBlockCipher_DisableImplicitNoncePanics(t *testing.T) {
	iT, _ := completeHandshake(t)
	bc := cipher.NewNoiseBlockCipher(iT.Send)
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic when disabling implicit nonce mode")
		}
	}()
	bc.SetImplicitNonceMode(false)
}
