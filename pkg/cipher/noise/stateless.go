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
	"encoding/binary"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/flynn/noise"
)

// StatelessCipher is an AEAD that carries its nonce counter on the
// wire. It is designed for datagram transports where packets may
// arrive out of order and the receiver must decrypt every packet in
// isolation.
//
// Construction happens at the end of a Noise handshake, from the
// 32-byte key that flynn/noise exposes via
// CipherState.UnsafeKey(). Every Encrypt call allocates a new counter
// from a monotonic 64-bit atomic source and fabricates a fresh
// noise.CipherState through UnsafeNewCipherState so each packet is
// encrypted with a unique (key, counter) pair. The counter is
// prepended to the ciphertext so the receiver can reconstruct the
// same CipherState for decryption.
//
// Security properties (see WireGuard §5.4 for the same argument):
//
//   - Confidentiality + integrity: the underlying AEAD (ChaCha20-Poly1305
//     or AES-256-GCM, depending on the cipher suite) with a unique
//     64-bit counter per packet gives IND-CCA2 security up to 2^64
//     packets — well beyond any realistic session.
//   - Forward secrecy: inherited from the Noise handshake that produced
//     the key. No long-term key is used for transport encryption.
//   - Replay resistance: supplied externally by the caller (mieru has
//     its own replay.Cache that rejects any (key, nonce) repeat).
//   - Out-of-order delivery: no state is kept between packets on the
//     receiver side, so any arrival order is accepted.
//
// Wire format of a single sealed packet:
//
//	| counter (8 bytes BE) | AEAD ciphertext | auth tag (16 bytes) |
type StatelessCipher struct {
	suite   noise.CipherSuite
	key     [32]byte
	counter atomic.Uint64 // next send-side counter
}

// NewStatelessCipher wraps a 32-byte symmetric key in a StatelessCipher
// bound to the given cipher suite. key is copied; the caller may
// zero its own buffer afterwards.
func NewStatelessCipher(suite noise.CipherSuite, key []byte) (*StatelessCipher, error) {
	if suite == nil {
		return nil, errors.New("noise.StatelessCipher: nil suite")
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("noise.StatelessCipher: key must be 32 bytes, got %d", len(key))
	}
	sc := &StatelessCipher{suite: suite}
	copy(sc.key[:], key)
	return sc, nil
}

// StatelessCipherFromTransport builds a StatelessCipher from one half of
// a Noise transport result (either Send or Recv). It is the normal
// path for mieru's UDP underlay: after handshake, both sides derive a
// pair of StatelessCiphers — one for each direction.
func StatelessCipherFromTransport(cs *noise.CipherState, suite noise.CipherSuite) (*StatelessCipher, error) {
	if cs == nil {
		return nil, errors.New("noise.StatelessCipher: nil transport state")
	}
	k := cs.UnsafeKey()
	return NewStatelessCipher(suite, k[:])
}

// NonceSize reports the number of bytes used to transport the counter
// on the wire (always 8).
func (s *StatelessCipher) NonceSize() int { return 8 }

// Overhead reports the AEAD tag size (always 16).
func (s *StatelessCipher) Overhead() int { return 16 }

// Seal allocates a fresh counter and returns `counter || ciphertext`.
// ad is optional additional authenticated data; mieru passes nil.
func (s *StatelessCipher) Seal(plaintext, ad []byte) ([]byte, error) {
	counter := s.counter.Add(1) - 1
	cs := noise.UnsafeNewCipherState(s.suite, s.key, counter)
	ct, err := cs.Encrypt(nil, ad, plaintext)
	if err != nil {
		return nil, fmt.Errorf("noise.StatelessCipher.Seal: %w", err)
	}
	out := make([]byte, 8+len(ct))
	binary.BigEndian.PutUint64(out[:8], counter)
	copy(out[8:], ct)
	return out, nil
}

// Open decrypts `counter || ciphertext` as emitted by Seal. It does
// not perform replay detection — callers that care must consult an
// external replay cache using the counter as the replay tag.
//
// The returned plaintext is a newly allocated slice; the input packet
// is not modified.
func (s *StatelessCipher) Open(packet, ad []byte) (plaintext []byte, counter uint64, err error) {
	if len(packet) < 8 {
		return nil, 0, fmt.Errorf("noise.StatelessCipher.Open: packet shorter than nonce (%d bytes)", len(packet))
	}
	counter = binary.BigEndian.Uint64(packet[:8])
	cs := noise.UnsafeNewCipherState(s.suite, s.key, counter)
	pt, err := cs.Decrypt(nil, ad, packet[8:])
	if err != nil {
		return nil, counter, fmt.Errorf("noise.StatelessCipher.Open: %w", err)
	}
	return pt, counter, nil
}

// Clone returns a new StatelessCipher that shares the same suite and
// key but starts its outbound counter at zero. This is intended for
// mieru's CipherCache which always Clones before handing a cipher to a
// session; since the counter is transmitted on the wire, independent
// counters on forked ciphers do not cause decryption failures on the
// peer.
func (s *StatelessCipher) Clone() *StatelessCipher {
	clone := &StatelessCipher{suite: s.suite, key: s.key}
	return clone
}

// SuiteForConfig exposes the flynn/noise CipherSuite corresponding to
// a Config. It is needed by callers that construct a StatelessCipher
// directly from the handshake output.
func SuiteForConfig(cfg Config) (noise.CipherSuite, error) {
	return cfg.cipherSuite()
}
