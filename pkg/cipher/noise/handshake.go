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

package noise

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/flynn/noise"
)

// maxHandshakeMessage is the maximum allowed size of a single Noise
// handshake message. The Noise spec caps plaintext at 65535 bytes; the
// flynn/noise library enforces the same limit internally.
const maxHandshakeMessage = 65535

// handshakeReadTimeout is a safety timeout applied to each handshake
// message read. Callers that want finer control can wrap the connection
// with their own deadlines before calling Run.
const handshakeReadTimeout = 10 * time.Second

// TransportCiphers pairs the two CipherStates produced by a successful
// Noise handshake, annotated with their purpose relative to the local
// side. The Send cipher encrypts outbound messages; the Recv cipher
// decrypts inbound messages.
type TransportCiphers struct {
	Send *noise.CipherState
	Recv *noise.CipherState

	// ChannelBinding is the handshake hash (`h` at the end of the
	// handshake). Two peers compute the same value after a successful
	// handshake, so it can be used to bind application layer protocols
	// to the Noise session.
	ChannelBinding []byte

	// RemoteStaticPublic is the remote party's static public key after
	// the handshake finishes. For patterns that never transmit a static
	// key it is nil.
	RemoteStaticPublic []byte
}

// Handshake drives one side of a Noise handshake.
//
// Typical usage:
//
//	cfg, _ := noise.NewConfig(noise.PatternXX, noise.DH25519,
//	    noise.CipherChaChaPoly, noise.HashSHA256, noise.RoleInitiator)
//	cfg.LocalStatic = myKeys
//	h, err := noise.NewHandshake(cfg)
//	if err != nil { ... }
//	transport, err := h.Run(conn, nil)
//	if err != nil { ... }
//	// transport.Send / transport.Recv are now ready.
type Handshake struct {
	cfg      Config
	state    *noise.HandshakeState
	finished *TransportCiphers
}

// NewHandshake prepares a handshake state from the given Config.
//
// For patterns with a PSK modifier the PSK is taken from cfg.PresharedKey.
// All key material is copied into the underlying flynn/noise state; the
// caller may safely zero its own buffers after this call returns.
func NewHandshake(cfg Config) (*Handshake, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	suite, err := cfg.cipherSuite()
	if err != nil {
		return nil, err
	}
	pattern, err := cfg.Pattern.handshakePattern()
	if err != nil {
		return nil, err
	}

	hsCfg := noise.Config{
		CipherSuite: suite,
		Pattern:     pattern,
		Initiator:   cfg.Role == RoleInitiator,
		Prologue:    append([]byte(nil), cfg.Prologue...),
		Random:      rand.Reader,
	}

	if cfg.localStaticRequired() {
		hsCfg.StaticKeypair = noise.DHKey{
			Public:  append([]byte(nil), cfg.LocalStatic.Public...),
			Private: append([]byte(nil), cfg.LocalStatic.Private...),
		}
	}
	if cfg.remoteStaticRequired() {
		hsCfg.PeerStatic = append([]byte(nil), cfg.RemoteStaticPublic...)
	}
	if len(cfg.PresharedKey) != 0 {
		hsCfg.PresharedKey = append([]byte(nil), cfg.PresharedKey...)
		hsCfg.PresharedKeyPlacement = cfg.PresharedKeyPlacement
	}

	state, err := noise.NewHandshakeState(hsCfg)
	if err != nil {
		return nil, fmt.Errorf("noise.NewHandshakeState failed: %w", err)
	}
	return &Handshake{cfg: cfg, state: state}, nil
}

// Config returns the validated Config that produced this handshake.
func (h *Handshake) Config() Config { return h.cfg }

// writeMessage serializes the next handshake message. When the
// handshake completes on this side, the resulting TransportCiphers are
// stored on the Handshake and returned via Finish().
func (h *Handshake) writeMessage(payload []byte) (out []byte, done bool, err error) {
	msg, cs1, cs2, err := h.state.WriteMessage(nil, payload)
	if err != nil {
		return nil, false, err
	}
	if cs1 != nil && cs2 != nil {
		h.finished = h.buildTransport(cs1, cs2)
		return msg, true, nil
	}
	return msg, false, nil
}

// readMessage consumes a handshake message and returns the decrypted
// payload (if any). When the handshake completes on a read,
// TransportCiphers can be retrieved via Finish().
func (h *Handshake) readMessage(msg []byte) (payload []byte, done bool, err error) {
	payload, cs1, cs2, err := h.state.ReadMessage(nil, msg)
	if err != nil {
		return nil, false, err
	}
	if cs1 != nil && cs2 != nil {
		h.finished = h.buildTransport(cs1, cs2)
		return payload, true, nil
	}
	return payload, false, nil
}

func (h *Handshake) buildTransport(cs1, cs2 *noise.CipherState) *TransportCiphers {
	t := &TransportCiphers{
		ChannelBinding:     append([]byte(nil), h.state.ChannelBinding()...),
		RemoteStaticPublic: copyBytes(h.state.PeerStatic()),
	}
	if h.cfg.Role == RoleInitiator {
		t.Send, t.Recv = cs1, cs2
	} else {
		t.Send, t.Recv = cs2, cs1
	}
	return t
}

// Run drives the handshake to completion on the given transport.
//
// Every handshake message is framed as a 2-byte big-endian length prefix
// followed by that many bytes of Noise handshake payload. The same
// framing is used on both sides and it is caller-agnostic — the caller
// only needs an io.ReadWriter that preserves byte order (e.g. a TCP
// net.Conn). For UDP or already-framed transports, use
// WriteHandshakeMessage / ReadHandshakeMessage instead.
//
// The firstPayload argument is only used for the very first message
// this side writes (it is mixed into the handshake hash). Most callers
// pass nil; supply a non-nil payload only if you know the pattern
// accepts one. The returned TransportCiphers are also stored on the
// Handshake and can be retrieved again via Finish().
func (h *Handshake) Run(rw io.ReadWriter, firstPayload []byte) (*TransportCiphers, error) {
	type deadliner interface {
		SetReadDeadline(time.Time) error
	}

	isInitiator := h.cfg.Role == RoleInitiator
	myTurn := isInitiator
	firstWriteConsumed := false

	for {
		if myTurn {
			var payload []byte
			if !firstWriteConsumed {
				payload = firstPayload
				firstWriteConsumed = true
			}
			msg, done, err := h.writeMessage(payload)
			if err != nil {
				return nil, fmt.Errorf("noise write message: %w", err)
			}
			if err := writeFramed(rw, msg); err != nil {
				return nil, fmt.Errorf("noise write framed: %w", err)
			}
			if done {
				return h.finished, nil
			}
			myTurn = false
			continue
		}

		if d, ok := rw.(deadliner); ok {
			_ = d.SetReadDeadline(time.Now().Add(handshakeReadTimeout))
		}
		msg, err := readFramed(rw)
		if err != nil {
			return nil, fmt.Errorf("noise read framed: %w", err)
		}
		_, done, err := h.readMessage(msg)
		if err != nil {
			return nil, fmt.Errorf("noise read message: %w", err)
		}
		if done {
			if d, ok := rw.(deadliner); ok {
				_ = d.SetReadDeadline(time.Time{})
			}
			return h.finished, nil
		}
		myTurn = true
	}
}

// WriteHandshakeMessage serializes the next handshake message without
// any framing. Use this when embedding a Noise handshake into a
// datagram protocol that already provides message boundaries.
func (h *Handshake) WriteHandshakeMessage(payload []byte) (out []byte, done bool, err error) {
	return h.writeMessage(payload)
}

// ReadHandshakeMessage feeds a received handshake message into the
// state machine. See WriteHandshakeMessage.
func (h *Handshake) ReadHandshakeMessage(msg []byte) (payload []byte, done bool, err error) {
	return h.readMessage(msg)
}

// Finish returns the TransportCiphers after the handshake has
// completed. It returns an error if called before completion.
func (h *Handshake) Finish() (*TransportCiphers, error) {
	if h.finished == nil {
		return nil, errors.New("noise handshake is not complete")
	}
	return h.finished, nil
}

// GenerateKeypair produces a random static keypair usable with the
// given DH function.
func GenerateKeypair(dh DH) (Keypair, error) {
	cfg := Config{DH: dh, Cipher: CipherChaChaPoly, Hash: HashSHA256}
	suite, err := cfg.cipherSuite()
	if err != nil {
		return Keypair{}, err
	}
	k, err := suite.GenerateKeypair(rand.Reader)
	if err != nil {
		return Keypair{}, fmt.Errorf("GenerateKeypair: %w", err)
	}
	return Keypair{
		Public:  append([]byte(nil), k.Public...),
		Private: append([]byte(nil), k.Private...),
	}, nil
}

// writeFramed writes a single length-prefixed handshake message.
func writeFramed(w io.Writer, msg []byte) error {
	if len(msg) > maxHandshakeMessage {
		return fmt.Errorf("handshake message %d bytes exceeds max %d", len(msg), maxHandshakeMessage)
	}
	var lenBuf [2]byte
	binary.BigEndian.PutUint16(lenBuf[:], uint16(len(msg)))
	if _, err := w.Write(lenBuf[:]); err != nil {
		return err
	}
	if len(msg) == 0 {
		return nil
	}
	_, err := w.Write(msg)
	return err
}

// readFramed reads a single length-prefixed handshake message.
func readFramed(r io.Reader) ([]byte, error) {
	var lenBuf [2]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint16(lenBuf[:]))
	if n > maxHandshakeMessage {
		return nil, fmt.Errorf("handshake message length %d exceeds max %d", n, maxHandshakeMessage)
	}
	if n == 0 {
		return nil, nil
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func copyBytes(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out
}
