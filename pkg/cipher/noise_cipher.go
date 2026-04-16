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
	"fmt"
	"sync"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/flynn/noise"
	"google.golang.org/protobuf/proto"
)

// noiseBlockCipher adapts a Noise transport-phase CipherState to the
// BlockCipher interface.
//
// Semantics:
//
//   - Noise's transport CipherState maintains a 64-bit monotonically
//     increasing counter that is used as the AEAD nonce for every
//     message. The counter is derived independently by each peer, so no
//     nonce bytes travel on the wire.
//   - A noiseBlockCipher always operates in "implicit nonce" mode. The
//     implicit-mode field on BlockCipher is exposed only to preserve
//     interface parity with AEADBlockCipher.
//   - Clone() is deliberately not supported because forking a CipherState
//     produces two independent counters that desynchronise immediately.
//     Callers must not clone noise ciphers — the cache layer already
//     treats implicit-nonce ciphers as non-cloneable.
type noiseBlockCipher struct {
	mu sync.Mutex

	cs      *noise.CipherState
	ctx     BlockContext
	pattern *appctlpb.NoncePattern
}

var _ BlockCipher = &noiseBlockCipher{}

// NewNoiseBlockCipher wraps a Noise transport CipherState as a
// BlockCipher. The resulting BlockCipher is stateful and must not be
// cloned.
//
// This is the bridge between pkg/cipher/noise (which performs the
// handshake) and the rest of mieru's protocol layer (which operates on
// BlockCipher values). After a successful Noise handshake a caller
// typically invokes:
//
//	send := cipher.NewNoiseBlockCipher(transport.Send)
//	recv := cipher.NewNoiseBlockCipher(transport.Recv)
//
// and installs them on a session.
func NewNoiseBlockCipher(cs *noise.CipherState) BlockCipher {
	if cs == nil {
		panic("cipher.NewNoiseBlockCipher: cipher state is nil")
	}
	return &noiseBlockCipher{cs: cs}
}

// Encrypt seals plaintext. It does not emit the nonce — the peer
// derives it from its own counter.
func (b *noiseBlockCipher) Encrypt(plaintext []byte) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cs == nil {
		return nil, fmt.Errorf("noise block cipher has no cipher state")
	}
	out, err := b.cs.Encrypt(nil, nil, plaintext)
	if err != nil {
		return nil, fmt.Errorf("noise encrypt: %w", err)
	}
	return out, nil
}

// EncryptWithNonce is unsupported: Noise transport uses its own
// monotonic counter and cannot accept external nonces.
func (b *noiseBlockCipher) EncryptWithNonce(plaintext, nonce []byte) ([]byte, error) {
	return nil, fmt.Errorf("noise block cipher does not support EncryptWithNonce")
}

// Decrypt opens ciphertext using the Noise CipherState.
func (b *noiseBlockCipher) Decrypt(ciphertext []byte) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cs == nil {
		return nil, fmt.Errorf("noise block cipher has no cipher state")
	}
	pt, err := b.cs.Decrypt(nil, nil, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("noise decrypt: %w", err)
	}
	return pt, nil
}

// DecryptWithNonce is unsupported: see EncryptWithNonce.
func (b *noiseBlockCipher) DecryptWithNonce(ciphertext, nonce []byte) ([]byte, error) {
	return nil, fmt.Errorf("noise block cipher does not support DecryptWithNonce")
}

// NonceSize reports the size of the implicit counter (Noise uses a
// 64-bit nonce internally). The counter is never transmitted.
func (b *noiseBlockCipher) NonceSize() int { return 8 }

// Overhead returns the AEAD tag size (16 bytes for both
// ChaCha20-Poly1305 and AES-GCM).
func (b *noiseBlockCipher) Overhead() int { return 16 }

// Clone is not supported for Noise ciphers because a Noise CipherState
// is stateful and non-forkable.
func (b *noiseBlockCipher) Clone() BlockCipher {
	panic("noise block cipher does not support Clone(): cipher states are not forkable")
}

// SetImplicitNonceMode must remain enabled for Noise ciphers. Disabling
// implicit mode would desynchronise the counter from the peer.
func (b *noiseBlockCipher) SetImplicitNonceMode(enable bool) {
	if !enable {
		panic("noise block cipher: implicit nonce mode cannot be disabled")
	}
}

// IsStateless always reports false. Noise transport ciphers are
// fundamentally stateful.
func (b *noiseBlockCipher) IsStateless() bool { return false }

// BlockContext returns a copy of the associated context.
func (b *noiseBlockCipher) BlockContext() BlockContext {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ctx
}

// SetBlockContext stores the given context.
func (b *noiseBlockCipher) SetBlockContext(bc BlockContext) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ctx = bc
}

// NoncePattern returns a copy of the associated nonce pattern. Noise
// ciphers do not act on nonce patterns (they have no externally
// controlled nonce); the value is kept only for interface parity.
func (b *noiseBlockCipher) NoncePattern() *appctlpb.NoncePattern {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pattern == nil {
		return nil
	}
	return proto.Clone(b.pattern).(*appctlpb.NoncePattern)
}

// SetNoncePattern records a nonce pattern. No effect on the actual
// encryption; see NoncePattern.
func (b *noiseBlockCipher) SetNoncePattern(pattern *appctlpb.NoncePattern) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if pattern == nil {
		b.pattern = nil
		return
	}
	b.pattern = proto.Clone(pattern).(*appctlpb.NoncePattern)
}
