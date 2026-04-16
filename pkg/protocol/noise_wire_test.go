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

package protocol

import (
	"bytes"
	"context"
	"encoding/hex"
	"io"
	mrand "math/rand"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher/noise"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/testtool"
	"google.golang.org/protobuf/proto"
)

// TestNoiseTCPUnderlay_XX_EndToEnd stands up a server and a client
// mux, both configured to use Noise_XX over TCP, and runs a short
// rot13 exchange through them. It is the end-to-end proof that the
// Noise handshake runs inside the mux on both sides and that the
// derived send/recv ciphers are correctly plumbed through the stream
// underlay's send and receive paths.
func TestNoiseTCPUnderlay_XX_EndToEnd(t *testing.T) {
	log.SetOutputToTest(t)
	log.SetLevel("DEBUG")

	serverKP := mustNoiseKeypair(t)
	clientKP := mustNoiseKeypair(t)

	port, err := common.UnusedTCPPort()
	if err != nil {
		t.Fatalf("UnusedTCPPort: %v", err)
	}
	serverProperties := NewUnderlayProperties(1400, common.StreamTransport,
		&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: port}, nil)

	// The server knows only its own static key; the client also sends
	// its static key during the XX handshake, so no pre-sharing of
	// client keys is required.
	serverNoise := noiseProtoConfig(&noiseParams{
		pattern:      appctlpb.NoisePattern_NOISE_XX,
		localPrivate: serverKP.Private,
		localPublic:  serverKP.Public,
	})

	serverMux := NewMux(false).
		SetServerUsers(users).
		SetEncryption(appctlpb.EncryptionType_NOISE, serverNoise).
		SetEndpoints([]UnderlayProperties{serverProperties})

	testServer := testtool.NewTestHelperServer()
	if err := serverMux.Start(); err != nil {
		t.Fatalf("Start server mux: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	go func() {
		if err := testServer.Serve(serverMux); err != nil {
			t.Errorf("Serve(): %v", err)
		}
	}()
	defer testServer.Close()
	defer serverMux.Close()
	time.Sleep(100 * time.Millisecond)

	// Client: knows server's static public key; supplies its own.
	clientNoise := noiseProtoConfig(&noiseParams{
		pattern:       appctlpb.NoisePattern_NOISE_XX,
		localPrivate:  clientKP.Private,
		localPublic:   clientKP.Public,
		remotePublic:  serverKP.Public,
	})

	clientProperties := NewUnderlayProperties(1400, common.StreamTransport, nil,
		&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: port})
	clientMux := NewMux(true).
		SetClientUserNamePassword("xiaochitang", []byte("irrelevant-for-noise")).
		SetClientMultiplexFactor(0). // no multiplex so every Dial spins a fresh handshake
		SetEncryption(appctlpb.EncryptionType_NOISE, clientNoise).
		SetEndpoints([]UnderlayProperties{clientProperties})

	dialCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	const concurrent = 2
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			conn, err := clientMux.DialContext(dialCtx)
			if err != nil {
				t.Errorf("[client %d] Dial: %v", idx, err)
				return
			}
			defer conn.Close()
			for j := 0; j < 20; j++ {
				payloadSize := mrand.Intn(maxPDU/4) + 1
				payload := testtool.TestHelperGenRot13Input(payloadSize)
				if _, err := conn.Write(payload); err != nil {
					t.Errorf("[client %d] Write: %v", idx, err)
					return
				}
				resp := make([]byte, payloadSize)
				if _, err := io.ReadFull(conn, resp); err != nil {
					t.Errorf("[client %d] ReadFull: %v", idx, err)
					return
				}
				rot, err := testtool.TestHelperRot13(resp)
				if err != nil {
					t.Errorf("[client %d] Rot13 server reply decode: %v", idx, err)
					return
				}
				if !bytes.Equal(payload, rot) {
					t.Errorf("[client %d] payload mismatch at iter %d", idx, j)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	if err := clientMux.Close(); err != nil {
		t.Errorf("client mux close: %v", err)
	}
}

// TestNoiseUDPUnderlay_RejectedCleanly confirms the early-fail guard
// that refuses Noise + UDP with a user-visible error message rather
// than hanging or panicking mid-session.
func TestNoiseUDPUnderlay_RejectedCleanly(t *testing.T) {
	serverKP := mustNoiseKeypair(t)

	cfg := noiseProtoConfig(&noiseParams{
		pattern:      appctlpb.NoisePattern_NOISE_XX,
		localPrivate: serverKP.Private,
		localPublic:  serverKP.Public,
	})

	// Client-side: dialing a UDP endpoint with NOISE enabled should
	// fail at newUnderlay() time with the documented message.
	port, err := common.UnusedUDPPort()
	if err != nil {
		t.Fatalf("UnusedUDPPort: %v", err)
	}
	clientProps := NewUnderlayProperties(1400, common.PacketTransport, nil,
		&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port})

	m := NewMux(true).
		SetClientUserNamePassword("x", []byte("y")).
		SetEncryption(appctlpb.EncryptionType_NOISE, cfg).
		SetEndpoints([]UnderlayProperties{clientProps})

	dialCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = m.DialContext(dialCtx)
	if err == nil {
		t.Fatalf("expected error dialing UDP with noise; got nil")
	}
	// Be generous — the error is wrapped; check the sentinel substring.
	if !bytesContains([]byte(err.Error()), []byte("noise encryption is not supported over UDP")) {
		t.Fatalf("error does not explain UDP rejection: %v", err)
	}
	_ = m.Close()
}

// --- helpers --------------------------------------------------------

type noiseParams struct {
	pattern      appctlpb.NoisePattern
	localPrivate []byte
	localPublic  []byte
	remotePublic []byte
}

func noiseProtoConfig(p *noiseParams) *appctlpb.NoiseConfig {
	pat := p.pattern
	dh := appctlpb.NoiseDH_NOISE_DH_25519
	ch := appctlpb.NoiseCipher_NOISE_CIPHER_CHACHA20POLY1305
	ha := appctlpb.NoiseHash_NOISE_HASH_SHA256
	cfg := &appctlpb.NoiseConfig{
		Pattern: &pat,
		Dh:      &dh,
		Cipher:  &ch,
		Hash:    &ha,
	}
	if len(p.localPrivate) != 0 {
		s := hex.EncodeToString(p.localPrivate)
		cfg.LocalStaticPrivateKey = proto.String(s)
	}
	if len(p.localPublic) != 0 {
		s := hex.EncodeToString(p.localPublic)
		cfg.LocalStaticPublicKey = proto.String(s)
	}
	if len(p.remotePublic) != 0 {
		s := hex.EncodeToString(p.remotePublic)
		cfg.RemoteStaticPublicKey = proto.String(s)
	}
	return cfg
}

func mustNoiseKeypair(t *testing.T) noise.Keypair {
	t.Helper()
	kp, err := noise.GenerateKeypair(noise.DH25519)
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}
	return kp
}

func bytesContains(haystack, needle []byte) bool {
	return bytes.Contains(haystack, needle)
}
