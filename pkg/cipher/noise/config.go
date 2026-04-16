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

// Package noise implements the Noise Protocol Framework
// (https://noiseprotocol.org/noise.html) as an alternative encryption
// scheme for mieru.
//
// Unlike the default XChaCha20-Poly1305 scheme that derives keys from a
// shared password via PBKDF2, Noise performs a real Diffie-Hellman
// handshake. After the handshake, each direction of the connection has
// its own AEAD CipherState which is exposed to the rest of mieru as a
// cipher.BlockCipher, so the mieru protocol layer does not need to care
// whether it is talking to a password-based cipher or a noise cipher.
//
// The full Noise name is Noise_<PATTERN>_<DH>_<CIPHER>_<HASH>, for
// example Noise_XX_25519_ChaChaPoly_SHA256. Each component is
// configurable in mieru via the NoiseConfig proto message. Valid values
// are documented in docs/noise.md.
package noise

import (
	"errors"
	"fmt"
	"strings"

	"github.com/flynn/noise"
)

// Pattern identifies a Noise handshake pattern.
//
// Patterns describe how the two participants authenticate each other
// and which static/ephemeral keys are exchanged. See
// https://noiseprotocol.org/noise.html#handshake-patterns for details.
type Pattern string

const (
	PatternNN Pattern = "NN" // 0-RTT, no authentication. Useful for testing.
	PatternNK Pattern = "NK" // Server static key known to client (0-RTT).
	PatternNX Pattern = "NX" // Server transmits static key in handshake.
	PatternXN Pattern = "XN" // Client transmits static key.
	PatternXK Pattern = "XK" // Client knows server static key, client transmits static key.
	PatternXX Pattern = "XX" // Mutual static key exchange. The most common choice.
	PatternIN Pattern = "IN" // Client sends static key immediately.
	PatternIK Pattern = "IK" // 1-RTT mutual authentication. Server static key known up front.
	PatternIX Pattern = "IX" // Mutual with client static in first message.
	PatternK  Pattern = "K"  // One-way pattern, both static keys known.
	PatternN  Pattern = "N"  // One-way pattern, server static key known.
	PatternX  Pattern = "X"  // One-way pattern, client transmits static key.
)

// DH identifies the Diffie-Hellman function.
type DH string

const (
	DH25519 DH = "25519" // Curve25519 (the only one supported by flynn/noise).
)

// Cipher identifies the AEAD cipher used for the handshake and transport phases.
type Cipher string

const (
	CipherChaChaPoly Cipher = "ChaChaPoly" // ChaCha20-Poly1305.
	CipherAESGCM     Cipher = "AESGCM"     // AES-256-GCM.
)

// Hash identifies the hash function used for Noise key derivation.
type Hash string

const (
	HashSHA256  Hash = "SHA256"
	HashSHA512  Hash = "SHA512"
	HashBLAKE2s Hash = "BLAKE2s"
	HashBLAKE2b Hash = "BLAKE2b"
)

// Role selects whether a peer drives the handshake as initiator or responder.
//
// In mieru terms, the SOCKS5 client side uses RoleInitiator and the
// proxy server uses RoleResponder.
type Role uint8

const (
	RoleInitiator Role = 1
	RoleResponder Role = 2
)

// Config is a validated Noise configuration ready to construct a Handshake.
//
// The zero value is not valid. Use NewConfig to build a Config and
// Validate() to check a Config that was deserialized from proto.
type Config struct {
	// Pattern is the handshake pattern (e.g. Pattern XX).
	Pattern Pattern

	// DH, Cipher and Hash together pick the cipher suite.
	DH     DH
	Cipher Cipher
	Hash   Hash

	// Role is the local role in the handshake (initiator or responder).
	Role Role

	// LocalStatic is the local long-term static keypair.
	//
	// Required when the pattern involves "s" for the local role
	// (e.g. pattern XX for either role, or pattern NK for the responder).
	// When set, LocalStatic.Public must be the DH public key derived
	// from LocalStatic.Private.
	LocalStatic Keypair

	// RemoteStaticPublic is the remote peer's long-term static public key.
	//
	// Required when the pattern transmits the remote static key ahead of
	// the handshake (e.g. pattern NK or IK for the initiator).
	RemoteStaticPublic []byte

	// PresharedKey is an optional 32-byte PSK that is mixed into the
	// handshake. It is required when Pattern contains a "psk" modifier
	// (e.g. "XXpsk3"). mieru's HashPassword can be used as a PSK.
	PresharedKey []byte

	// PresharedKeyPlacement specifies the position of the PSK token in
	// the pattern (0-based). Ignored when PresharedKey is nil.
	// For example, XXpsk3 corresponds to placement = 3.
	PresharedKeyPlacement int

	// Prologue is optional application-specific data that is mixed into
	// the handshake. Both sides must supply identical prologues.
	Prologue []byte
}

// Keypair represents a Curve25519 (or other DH) keypair.
type Keypair struct {
	Public  []byte
	Private []byte
}

// FullName returns the canonical Noise protocol name, e.g.
// "Noise_XX_25519_ChaChaPoly_SHA256".
func (c Config) FullName() string {
	pattern := string(c.Pattern)
	if len(c.PresharedKey) != 0 {
		pattern = fmt.Sprintf("%spsk%d", pattern, c.PresharedKeyPlacement)
	}
	return fmt.Sprintf("Noise_%s_%s_%s_%s", pattern, c.DH, c.Cipher, c.Hash)
}

// Validate checks that a Config is well-formed. It does not check
// whether the keys match the chosen DH curve; that happens inside
// flynn/noise when the handshake is built.
func (c Config) Validate() error {
	if !c.Pattern.known() {
		return fmt.Errorf("unknown noise pattern %q", c.Pattern)
	}
	if !c.DH.known() {
		return fmt.Errorf("unknown noise DH %q", c.DH)
	}
	if !c.Cipher.known() {
		return fmt.Errorf("unknown noise cipher %q", c.Cipher)
	}
	if !c.Hash.known() {
		return fmt.Errorf("unknown noise hash %q", c.Hash)
	}
	if c.Role != RoleInitiator && c.Role != RoleResponder {
		return fmt.Errorf("noise role must be initiator or responder, got %d", c.Role)
	}
	if c.localStaticRequired() {
		if len(c.LocalStatic.Private) == 0 || len(c.LocalStatic.Public) == 0 {
			return fmt.Errorf("noise pattern %q with role %d requires a local static keypair", c.Pattern, c.Role)
		}
	}
	if c.remoteStaticRequired() && len(c.RemoteStaticPublic) == 0 {
		return fmt.Errorf("noise pattern %q with role %d requires the remote static public key", c.Pattern, c.Role)
	}
	if len(c.PresharedKey) != 0 && len(c.PresharedKey) != 32 {
		return fmt.Errorf("noise PSK must be exactly 32 bytes, got %d", len(c.PresharedKey))
	}
	if len(c.PresharedKey) != 0 && c.PresharedKeyPlacement < 0 {
		return fmt.Errorf("noise PSK placement must be >= 0, got %d", c.PresharedKeyPlacement)
	}
	return nil
}

// NewConfig constructs a Config with the given parameters and validates it.
func NewConfig(pattern Pattern, dh DH, cipher Cipher, hash Hash, role Role) (Config, error) {
	cfg := Config{
		Pattern: pattern,
		DH:      dh,
		Cipher:  cipher,
		Hash:    hash,
		Role:    role,
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// ParsePattern turns a proto/string value into a Pattern, accepting any
// case. It returns an error for unknown patterns.
func ParsePattern(s string) (Pattern, error) {
	up := strings.ToUpper(strings.TrimSpace(s))
	p := Pattern(up)
	if !p.known() {
		return "", fmt.Errorf("unknown noise pattern %q", s)
	}
	return p, nil
}

// ParseDH, ParseCipher and ParseHash do the same for cipher suite pieces.

func ParseDH(s string) (DH, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return DH25519, nil
	}
	d := DH(t)
	if !d.known() {
		return "", fmt.Errorf("unknown noise DH %q", s)
	}
	return d, nil
}

func ParseCipher(s string) (Cipher, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return CipherChaChaPoly, nil
	}
	// Accept common aliases.
	switch strings.ToLower(t) {
	case "chachapoly", "chacha20poly1305", "chacha20-poly1305":
		return CipherChaChaPoly, nil
	case "aesgcm", "aes-gcm", "aes256gcm", "aes-256-gcm":
		return CipherAESGCM, nil
	}
	c := Cipher(t)
	if !c.known() {
		return "", fmt.Errorf("unknown noise cipher %q", s)
	}
	return c, nil
}

func ParseHash(s string) (Hash, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return HashSHA256, nil
	}
	h := Hash(t)
	if !h.known() {
		return "", fmt.Errorf("unknown noise hash %q", s)
	}
	return h, nil
}

// Internal helpers.

func (p Pattern) known() bool {
	switch p {
	case PatternNN, PatternNK, PatternNX,
		PatternXN, PatternXK, PatternXX,
		PatternIN, PatternIK, PatternIX,
		PatternK, PatternN, PatternX:
		return true
	}
	return false
}

func (d DH) known() bool {
	return d == DH25519
}

func (c Cipher) known() bool {
	switch c {
	case CipherChaChaPoly, CipherAESGCM:
		return true
	}
	return false
}

func (h Hash) known() bool {
	switch h {
	case HashSHA256, HashSHA512, HashBLAKE2s, HashBLAKE2b:
		return true
	}
	return false
}

// cipherSuite maps the configured cipher suite to a flynn/noise CipherSuite.
func (c Config) cipherSuite() (noise.CipherSuite, error) {
	var dhFn noise.DHFunc
	switch c.DH {
	case DH25519:
		dhFn = noise.DH25519
	default:
		return nil, fmt.Errorf("unsupported noise DH %q", c.DH)
	}

	var cipherFn noise.CipherFunc
	switch c.Cipher {
	case CipherChaChaPoly:
		cipherFn = noise.CipherChaChaPoly
	case CipherAESGCM:
		cipherFn = noise.CipherAESGCM
	default:
		return nil, fmt.Errorf("unsupported noise cipher %q", c.Cipher)
	}

	var hashFn noise.HashFunc
	switch c.Hash {
	case HashSHA256:
		hashFn = noise.HashSHA256
	case HashSHA512:
		hashFn = noise.HashSHA512
	case HashBLAKE2s:
		hashFn = noise.HashBLAKE2s
	case HashBLAKE2b:
		hashFn = noise.HashBLAKE2b
	default:
		return nil, fmt.Errorf("unsupported noise hash %q", c.Hash)
	}

	return noise.NewCipherSuite(dhFn, cipherFn, hashFn), nil
}

// handshakePattern maps Pattern to a flynn/noise HandshakePattern.
func (p Pattern) handshakePattern() (noise.HandshakePattern, error) {
	switch p {
	case PatternNN:
		return noise.HandshakeNN, nil
	case PatternNK:
		return noise.HandshakeNK, nil
	case PatternNX:
		return noise.HandshakeNX, nil
	case PatternXN:
		return noise.HandshakeXN, nil
	case PatternXK:
		return noise.HandshakeXK, nil
	case PatternXX:
		return noise.HandshakeXX, nil
	case PatternIN:
		return noise.HandshakeIN, nil
	case PatternIK:
		return noise.HandshakeIK, nil
	case PatternIX:
		return noise.HandshakeIX, nil
	case PatternK:
		return noise.HandshakeK, nil
	case PatternN:
		return noise.HandshakeN, nil
	case PatternX:
		return noise.HandshakeX, nil
	}
	return noise.HandshakePattern{}, fmt.Errorf("unsupported noise pattern %q", p)
}

// localStaticRequired reports whether the pattern + role combination
// needs a local static keypair.
//
// The table below encodes Noise spec Table 7.5. "s" in a message column
// means the side sends its static key; a pattern with "s" anywhere
// implies that side owns a static key. For the simple one-way patterns
// the initiator drives message 1 and sender = initiator.
func (c Config) localStaticRequired() bool {
	switch c.Pattern {
	case PatternNN:
		return false
	case PatternNK, PatternNX:
		// Only responder has a static key.
		return c.Role == RoleResponder
	case PatternXN, PatternXK:
		// Initiator has a static key; for XK responder also has one.
		if c.Pattern == PatternXK && c.Role == RoleResponder {
			return true
		}
		return c.Role == RoleInitiator
	case PatternXX:
		return true
	case PatternIN:
		return c.Role == RoleInitiator
	case PatternIK, PatternIX:
		return true
	case PatternK:
		return true
	case PatternN:
		return c.Role == RoleResponder
	case PatternX:
		// Initiator sends static, responder has static.
		return true
	}
	return false
}

// remoteStaticRequired reports whether the local side needs to know the
// remote's static public key up front (i.e. before the handshake).
func (c Config) remoteStaticRequired() bool {
	switch c.Pattern {
	case PatternNK, PatternXK, PatternIK, PatternK, PatternN, PatternX:
		// In these patterns the initiator must already know the responder's
		// static key. The responder never needs to pre-know the initiator
		// because the initiator transmits it (if applicable).
		return c.Role == RoleInitiator
	}
	return false
}

// ErrPSKRequired is returned when a proto config asks for a psk-modified
// pattern but the caller supplied no PSK material.
var ErrPSKRequired = errors.New("noise pattern requires a preshared key but none was provided")
