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

// TestNoiseUDPUnderlay_XX_EndToEnd stands up a server and a client
// mux, both configured to use Noise_XX over UDP, and runs a small
// exchange through them. It proves that the datagram handshake flow
// succeeds, the post-handshake Noise AEAD wrapper encrypts/decrypts
// each datagram, and the inner mieru PacketUnderlay still performs
// session multiplexing on top unchanged.
func TestNoiseUDPUnderlay_XX_EndToEnd(t *testing.T) {
	log.SetOutputToTest(t)
	log.SetLevel("DEBUG")

	serverKP := mustNoiseKeypair(t)
	clientKP := mustNoiseKeypair(t)

	port, err := common.UnusedUDPPort()
	if err != nil {
		t.Fatalf("UnusedUDPPort: %v", err)
	}
	serverProperties := NewUnderlayProperties(1400, common.PacketTransport,
		&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port}, nil)

	serverNoise := noiseProtoConfig(&noiseParams{
		pattern:      appctlpb.NoisePattern_NOISE_XX,
		localPrivate: serverKP.Private,
		localPublic:  serverKP.Public,
	})
	serverMux := NewMux(false).
		SetServerUsers(users). // ignored for noise UDP; mux injects a synthetic user
		SetEncryption(appctlpb.EncryptionType_NOISE, serverNoise).
		SetEndpoints([]UnderlayProperties{serverProperties})
	testServer := testtool.NewTestHelperServer()
	if err := serverMux.Start(); err != nil {
		t.Fatalf("Start server mux: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	go func() {
		if err := testServer.Serve(serverMux); err != nil {
			t.Errorf("Serve(): %v", err)
		}
	}()
	defer testServer.Close()
	defer serverMux.Close()
	time.Sleep(100 * time.Millisecond)

	clientNoise := noiseProtoConfig(&noiseParams{
		pattern:      appctlpb.NoisePattern_NOISE_XX,
		localPrivate: clientKP.Private,
		localPublic:  clientKP.Public,
		remotePublic: serverKP.Public,
	})
	clientProperties := NewUnderlayProperties(1400, common.PacketTransport, nil,
		&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port})
	clientMux := NewMux(true).
		SetClientUserNamePassword("ignored-for-noise", []byte("still-ignored")).
		SetClientMultiplexFactor(0).
		SetEncryption(appctlpb.EncryptionType_NOISE, clientNoise).
		SetEndpoints([]UnderlayProperties{clientProperties})

	dialCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, err := clientMux.DialContext(dialCtx)
	if err != nil {
		t.Fatalf("DialContext noise+UDP: %v", err)
	}
	defer conn.Close()

	for j := 0; j < 5; j++ {
		payload := testtool.TestHelperGenRot13Input(512)
		if _, err := conn.Write(payload); err != nil {
			t.Fatalf("Write iter=%d: %v", j, err)
		}
		resp := make([]byte, len(payload))
		if _, err := io.ReadFull(conn, resp); err != nil {
			t.Fatalf("ReadFull iter=%d: %v", j, err)
		}
		rot, err := testtool.TestHelperRot13(resp)
		if err != nil {
			t.Fatalf("rot13: %v", err)
		}
		if !bytes.Equal(payload, rot) {
			t.Fatalf("payload mismatch iter=%d", j)
		}
	}

	if err := clientMux.Close(); err != nil {
		t.Errorf("client mux close: %v", err)
	}
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

