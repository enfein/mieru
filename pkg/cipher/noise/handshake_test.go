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

package noise_test

import (
	"bytes"
	"crypto/rand"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/enfein/mieru/v3/pkg/cipher/noise"
)

// pipeConn adapts net.Pipe to satisfy io.ReadWriter with deadlines.
// It is used to drive both sides of a handshake in the same goroutine
// via two counter-connected pipes.

func TestHandshake_XX_ChaChaSHA256(t *testing.T) {
	runHandshakeRoundTrip(t, noise.PatternXX, noise.DH25519,
		noise.CipherChaChaPoly, noise.HashSHA256)
}

func TestHandshake_XX_AESGCM_SHA512(t *testing.T) {
	runHandshakeRoundTrip(t, noise.PatternXX, noise.DH25519,
		noise.CipherAESGCM, noise.HashSHA512)
}

func TestHandshake_NN_BLAKE2s(t *testing.T) {
	runHandshakeRoundTrip(t, noise.PatternNN, noise.DH25519,
		noise.CipherChaChaPoly, noise.HashBLAKE2s)
}

func TestHandshake_NK_ChaChaBLAKE2b(t *testing.T) {
	runHandshakeRoundTrip(t, noise.PatternNK, noise.DH25519,
		noise.CipherChaChaPoly, noise.HashBLAKE2b)
}

func TestHandshake_IK_AESGCM(t *testing.T) {
	runHandshakeRoundTrip(t, noise.PatternIK, noise.DH25519,
		noise.CipherAESGCM, noise.HashSHA256)
}

func TestHandshake_IX_ChaCha(t *testing.T) {
	runHandshakeRoundTrip(t, noise.PatternIX, noise.DH25519,
		noise.CipherChaChaPoly, noise.HashSHA256)
}

// runHandshakeRoundTrip exercises a full handshake for the given suite
// and then a small number of transport-phase round-trips to confirm
// that the derived cipher states are symmetric.
func runHandshakeRoundTrip(t *testing.T, p noise.Pattern, dh noise.DH, c noise.Cipher, h noise.Hash) {
	t.Helper()

	initiator := noise.Config{Pattern: p, DH: dh, Cipher: c, Hash: h, Role: noise.RoleInitiator}
	responder := noise.Config{Pattern: p, DH: dh, Cipher: c, Hash: h, Role: noise.RoleResponder}

	// Generate keypairs as needed by the pattern.
	var iKP, rKP noise.Keypair
	if needsLocalStatic(initiator) {
		iKP = mustKeypair(t, dh)
		initiator.LocalStatic = iKP
	}
	if needsLocalStatic(responder) {
		rKP = mustKeypair(t, dh)
		responder.LocalStatic = rKP
	}
	if needsRemoteStatic(initiator) {
		initiator.RemoteStaticPublic = rKP.Public
	}
	if needsRemoteStatic(responder) {
		responder.RemoteStaticPublic = iKP.Public
	}

	if err := initiator.Validate(); err != nil {
		t.Fatalf("initiator.Validate: %v", err)
	}
	if err := responder.Validate(); err != nil {
		t.Fatalf("responder.Validate: %v", err)
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	var wg sync.WaitGroup
	var iTransport, rTransport *noise.TransportCiphers
	var iErr, rErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		hs, err := noise.NewHandshake(initiator)
		if err != nil {
			iErr = err
			return
		}
		iTransport, iErr = hs.Run(clientConn, nil)
	}()
	go func() {
		defer wg.Done()
		hs, err := noise.NewHandshake(responder)
		if err != nil {
			rErr = err
			return
		}
		rTransport, rErr = hs.Run(serverConn, nil)
	}()
	wg.Wait()

	if iErr != nil {
		t.Fatalf("initiator handshake: %v", iErr)
	}
	if rErr != nil {
		t.Fatalf("responder handshake: %v", rErr)
	}
	if iTransport == nil || rTransport == nil {
		t.Fatalf("transport ciphers missing")
	}
	if !bytes.Equal(iTransport.ChannelBinding, rTransport.ChannelBinding) {
		t.Fatalf("channel binding mismatch between peers")
	}

	// Exchange a few messages each way.
	msgs := [][]byte{
		[]byte("hello"),
		[]byte("the quick brown fox jumps over the lazy dog"),
		bytes.Repeat([]byte("x"), 4096),
	}
	for _, m := range msgs {
		ct, err := iTransport.Send.Encrypt(nil, nil, m)
		if err != nil {
			t.Fatalf("initiator encrypt: %v", err)
		}
		pt, err := rTransport.Recv.Decrypt(nil, nil, ct)
		if err != nil {
			t.Fatalf("responder decrypt: %v", err)
		}
		if !bytes.Equal(m, pt) {
			t.Fatalf("round-trip mismatch")
		}
	}
	for _, m := range msgs {
		ct, err := rTransport.Send.Encrypt(nil, nil, m)
		if err != nil {
			t.Fatalf("responder encrypt: %v", err)
		}
		pt, err := iTransport.Recv.Decrypt(nil, nil, ct)
		if err != nil {
			t.Fatalf("initiator decrypt: %v", err)
		}
		if !bytes.Equal(m, pt) {
			t.Fatalf("reverse round-trip mismatch")
		}
	}
}

// TestHandshake_PSK exercises the psk-modified XXpsk3 pattern.
func TestHandshake_PSK_XXpsk3(t *testing.T) {
	psk := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, psk); err != nil {
		t.Fatalf("psk: %v", err)
	}

	kpI := mustKeypair(t, noise.DH25519)
	kpR := mustKeypair(t, noise.DH25519)

	initCfg := noise.Config{
		Pattern:               noise.PatternXX,
		DH:                    noise.DH25519,
		Cipher:                noise.CipherChaChaPoly,
		Hash:                  noise.HashSHA256,
		Role:                  noise.RoleInitiator,
		LocalStatic:           kpI,
		PresharedKey:          psk,
		PresharedKeyPlacement: 3,
	}
	respCfg := noise.Config{
		Pattern:               noise.PatternXX,
		DH:                    noise.DH25519,
		Cipher:                noise.CipherChaChaPoly,
		Hash:                  noise.HashSHA256,
		Role:                  noise.RoleResponder,
		LocalStatic:           kpR,
		PresharedKey:          psk,
		PresharedKeyPlacement: 3,
	}

	cConn, sConn := net.Pipe()
	defer cConn.Close()
	defer sConn.Close()

	var wg sync.WaitGroup
	var iT, rT *noise.TransportCiphers
	var iErr, rErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		hs, err := noise.NewHandshake(initCfg)
		if err != nil {
			iErr = err
			return
		}
		iT, iErr = hs.Run(cConn, nil)
	}()
	go func() {
		defer wg.Done()
		hs, err := noise.NewHandshake(respCfg)
		if err != nil {
			rErr = err
			return
		}
		rT, rErr = hs.Run(sConn, nil)
	}()
	wg.Wait()

	if iErr != nil || rErr != nil {
		t.Fatalf("psk handshake errors: initiator=%v responder=%v", iErr, rErr)
	}
	if !bytes.Equal(iT.ChannelBinding, rT.ChannelBinding) {
		t.Fatalf("psk channel binding mismatch")
	}

	ct, err := iT.Send.Encrypt(nil, nil, []byte("ping"))
	if err != nil {
		t.Fatalf("psk encrypt: %v", err)
	}
	pt, err := rT.Recv.Decrypt(nil, nil, ct)
	if err != nil {
		t.Fatalf("psk decrypt: %v", err)
	}
	if string(pt) != "ping" {
		t.Fatalf("psk plaintext mismatch")
	}
}

// TestHandshake_WrongPSK fails when the two sides disagree on the PSK.
func TestHandshake_WrongPSK(t *testing.T) {
	pskA := bytes.Repeat([]byte{0xAA}, 32)
	pskB := bytes.Repeat([]byte{0xBB}, 32)

	kpI := mustKeypair(t, noise.DH25519)
	kpR := mustKeypair(t, noise.DH25519)

	initCfg := noise.Config{
		Pattern: noise.PatternXX, DH: noise.DH25519,
		Cipher: noise.CipherChaChaPoly, Hash: noise.HashSHA256,
		Role: noise.RoleInitiator, LocalStatic: kpI,
		PresharedKey: pskA, PresharedKeyPlacement: 3,
	}
	respCfg := noise.Config{
		Pattern: noise.PatternXX, DH: noise.DH25519,
		Cipher: noise.CipherChaChaPoly, Hash: noise.HashSHA256,
		Role: noise.RoleResponder, LocalStatic: kpR,
		PresharedKey: pskB, PresharedKeyPlacement: 3,
	}

	cConn, sConn := net.Pipe()
	defer cConn.Close()
	defer sConn.Close()

	done := make(chan struct{})
	var iErr, rErr error
	go func() {
		hs, err := noise.NewHandshake(initCfg)
		if err != nil {
			iErr = err
			close(done)
			return
		}
		_, iErr = hs.Run(cConn, nil)
		close(done)
	}()
	hs, err := noise.NewHandshake(respCfg)
	if err != nil {
		t.Fatalf("respCfg: %v", err)
	}
	_, rErr = hs.Run(sConn, nil)
	<-done

	if iErr == nil && rErr == nil {
		t.Fatalf("expected handshake failure with mismatched PSKs")
	}
}

// TestHandshake_PrologueMismatch fails when prologues differ.
//
// XX has the interesting property that the first side to notice the
// prologue divergence is the responder (when it tries to decrypt the
// initiator's third message, the AEAD tag fails). The initiator may
// still complete its local state machine, so we only require that at
// least one side produces an error — and we back that with a short
// watchdog that closes the pipe in case flynn/noise ever changes that
// behavior and both sides end up "happy".
func TestHandshake_PrologueMismatch(t *testing.T) {
	kpI := mustKeypair(t, noise.DH25519)
	kpR := mustKeypair(t, noise.DH25519)

	initCfg := noise.Config{
		Pattern: noise.PatternXX, DH: noise.DH25519,
		Cipher: noise.CipherChaChaPoly, Hash: noise.HashSHA256,
		Role: noise.RoleInitiator, LocalStatic: kpI,
		Prologue: []byte("prologue-A"),
	}
	respCfg := noise.Config{
		Pattern: noise.PatternXX, DH: noise.DH25519,
		Cipher: noise.CipherChaChaPoly, Hash: noise.HashSHA256,
		Role: noise.RoleResponder, LocalStatic: kpR,
		Prologue: []byte("prologue-B"),
	}

	cConn, sConn := net.Pipe()

	// Watchdog: close both ends after a short delay so a hung side
	// fails fast rather than waiting for the library-level read
	// timeout.
	time.AfterFunc(300*time.Millisecond, func() {
		_ = cConn.Close()
		_ = sConn.Close()
	})

	done := make(chan struct{})
	var iErr, rErr error
	go func() {
		hs, err := noise.NewHandshake(initCfg)
		if err == nil {
			_, iErr = hs.Run(cConn, nil)
		} else {
			iErr = err
		}
		close(done)
	}()
	hs, err := noise.NewHandshake(respCfg)
	if err != nil {
		t.Fatalf("respCfg: %v", err)
	}
	_, rErr = hs.Run(sConn, nil)
	<-done
	if iErr == nil && rErr == nil {
		t.Fatalf("expected handshake failure with mismatched prologues")
	}
}

// TestConfig_ValidationErrors walks through bad configurations.
func TestConfig_ValidationErrors(t *testing.T) {
	// Missing local static for a pattern that needs it.
	cfg := noise.Config{Pattern: noise.PatternXX, DH: noise.DH25519,
		Cipher: noise.CipherChaChaPoly, Hash: noise.HashSHA256,
		Role: noise.RoleInitiator}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected error for missing static key")
	}

	// Unknown pattern.
	cfg = noise.Config{Pattern: noise.Pattern("ZZ"), DH: noise.DH25519,
		Cipher: noise.CipherChaChaPoly, Hash: noise.HashSHA256,
		Role: noise.RoleInitiator}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected unknown pattern error")
	}

	// Wrong PSK size.
	kp := mustKeypair(t, noise.DH25519)
	cfg = noise.Config{Pattern: noise.PatternXX, DH: noise.DH25519,
		Cipher: noise.CipherChaChaPoly, Hash: noise.HashSHA256,
		Role: noise.RoleInitiator, LocalStatic: kp,
		PresharedKey: []byte("short")}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected PSK length error")
	}

	// Initiator of NK without remote static.
	cfg = noise.Config{Pattern: noise.PatternNK, DH: noise.DH25519,
		Cipher: noise.CipherChaChaPoly, Hash: noise.HashSHA256,
		Role: noise.RoleInitiator}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected missing remote static error for NK initiator")
	}
}

// TestConfig_FullName matches the Noise spec's canonical name format.
func TestConfig_FullName(t *testing.T) {
	cfg := noise.Config{
		Pattern: noise.PatternXX, DH: noise.DH25519,
		Cipher: noise.CipherChaChaPoly, Hash: noise.HashSHA256,
		Role: noise.RoleInitiator,
	}
	if got, want := cfg.FullName(), "Noise_XX_25519_ChaChaPoly_SHA256"; got != want {
		t.Fatalf("FullName = %q, want %q", got, want)
	}
	cfg.PresharedKey = make([]byte, 32)
	cfg.PresharedKeyPlacement = 3
	if got, want := cfg.FullName(), "Noise_XXpsk3_25519_ChaChaPoly_SHA256"; got != want {
		t.Fatalf("FullName with psk = %q, want %q", got, want)
	}
}

// TestParseHelpers covers the string parsers used by proto wiring.
func TestParseHelpers(t *testing.T) {
	if p, err := noise.ParsePattern("xx"); err != nil || p != noise.PatternXX {
		t.Fatalf("ParsePattern: %v / %v", p, err)
	}
	if _, err := noise.ParsePattern("zz"); err == nil {
		t.Fatalf("ParsePattern expected error")
	}
	if d, err := noise.ParseDH(""); err != nil || d != noise.DH25519 {
		t.Fatalf("ParseDH default: %v / %v", d, err)
	}
	if c, err := noise.ParseCipher("chacha20-poly1305"); err != nil || c != noise.CipherChaChaPoly {
		t.Fatalf("ParseCipher alias: %v / %v", c, err)
	}
	if c, err := noise.ParseCipher("AES-GCM"); err != nil || c != noise.CipherAESGCM {
		t.Fatalf("ParseCipher alias aes: %v / %v", c, err)
	}
	if h, err := noise.ParseHash("BLAKE2s"); err != nil || h != noise.HashBLAKE2s {
		t.Fatalf("ParseHash blake2s: %v / %v", h, err)
	}
}

// TestHandshake_FailsOnStalledPeer covers handshake failure when the
// peer never responds.
//
// net.Pipe is fully synchronous: Write blocks until Read is called on
// the other side. A silent peer that drains writes and never replies
// would hang the handshake forever. We schedule a short-lived timer to
// close the connection, which turns every blocked Read/Write into an
// error inside Handshake.Run and lets the test finish quickly.
func TestHandshake_FailsOnStalledPeer(t *testing.T) {
	if testing.Short() {
		t.Skip("slow test")
	}

	cConn, sConn := net.Pipe()

	// Drain any data the initiator writes; never write back.
	go func() {
		buf := make([]byte, 1024)
		for {
			if _, err := sConn.Read(buf); err != nil {
				return
			}
		}
	}()

	// Force the handshake to abort quickly.
	time.AfterFunc(200*time.Millisecond, func() {
		_ = cConn.Close()
		_ = sConn.Close()
	})

	kp := mustKeypair(t, noise.DH25519)
	cfg := noise.Config{
		Pattern: noise.PatternXX, DH: noise.DH25519,
		Cipher: noise.CipherChaChaPoly, Hash: noise.HashSHA256,
		Role: noise.RoleInitiator, LocalStatic: kp,
	}
	hs, err := noise.NewHandshake(cfg)
	if err != nil {
		t.Fatalf("NewHandshake: %v", err)
	}
	_, err = hs.Run(cConn, nil)
	if err == nil {
		t.Fatalf("expected error from aborted handshake, got nil")
	}
}

// mustKeypair returns a fresh keypair for the given DH function.
func mustKeypair(t *testing.T, dh noise.DH) noise.Keypair {
	t.Helper()
	kp, err := noise.GenerateKeypair(dh)
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}
	return kp
}

// Helpers that reproduce Config's internal rules so the test suite can
// set keys correctly without reaching into unexported methods.
//
// Keeping these in the test file (rather than exporting the helper)
// lets us catch a divergence between documented behavior and the
// library's internal helpers.
func needsLocalStatic(cfg noise.Config) bool {
	switch cfg.Pattern {
	case noise.PatternNN:
		return false
	case noise.PatternNK, noise.PatternNX:
		return cfg.Role == noise.RoleResponder
	case noise.PatternXN:
		return cfg.Role == noise.RoleInitiator
	case noise.PatternXK:
		return true
	case noise.PatternXX:
		return true
	case noise.PatternIN:
		return cfg.Role == noise.RoleInitiator
	case noise.PatternIK, noise.PatternIX:
		return true
	case noise.PatternK:
		return true
	case noise.PatternN:
		return cfg.Role == noise.RoleResponder
	case noise.PatternX:
		return true
	}
	return false
}

func needsRemoteStatic(cfg noise.Config) bool {
	switch cfg.Pattern {
	case noise.PatternNK, noise.PatternXK, noise.PatternIK,
		noise.PatternK, noise.PatternN, noise.PatternX:
		return cfg.Role == noise.RoleInitiator
	}
	return false
}
