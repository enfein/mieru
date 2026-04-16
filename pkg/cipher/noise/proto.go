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
	"encoding/hex"
	"fmt"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
)

// ConfigFromProto builds a Config for the given role from an
// appctlpb.NoiseConfig. The returned Config is fully validated and
// ready to pass to NewHandshake.
//
// Defaults applied when the proto leaves a field unset:
//
//	Pattern = NOISE_XX
//	DH      = NOISE_DH_25519
//	Cipher  = NOISE_CIPHER_CHACHA20POLY1305
//	Hash    = NOISE_HASH_SHA256
//
// Hex-encoded key material is decoded here. If the local static public
// key is missing but the private key is present, the public key is
// derived via the chosen DH function.
func ConfigFromProto(pc *appctlpb.NoiseConfig, role Role) (Config, error) {
	if pc == nil {
		return Config{}, fmt.Errorf("noise: nil NoiseConfig")
	}

	cfg := Config{Role: role}

	switch pc.GetPattern() {
	case appctlpb.NoisePattern_NOISE_PATTERN_UNSPECIFIED:
		cfg.Pattern = PatternXX
	case appctlpb.NoisePattern_NOISE_NN:
		cfg.Pattern = PatternNN
	case appctlpb.NoisePattern_NOISE_NK:
		cfg.Pattern = PatternNK
	case appctlpb.NoisePattern_NOISE_NX:
		cfg.Pattern = PatternNX
	case appctlpb.NoisePattern_NOISE_XN:
		cfg.Pattern = PatternXN
	case appctlpb.NoisePattern_NOISE_XK:
		cfg.Pattern = PatternXK
	case appctlpb.NoisePattern_NOISE_XX:
		cfg.Pattern = PatternXX
	case appctlpb.NoisePattern_NOISE_IN:
		cfg.Pattern = PatternIN
	case appctlpb.NoisePattern_NOISE_IK:
		cfg.Pattern = PatternIK
	case appctlpb.NoisePattern_NOISE_IX:
		cfg.Pattern = PatternIX
	case appctlpb.NoisePattern_NOISE_K:
		cfg.Pattern = PatternK
	case appctlpb.NoisePattern_NOISE_N:
		cfg.Pattern = PatternN
	case appctlpb.NoisePattern_NOISE_X:
		cfg.Pattern = PatternX
	default:
		return Config{}, fmt.Errorf("noise: unsupported proto pattern %v", pc.GetPattern())
	}

	switch pc.GetDh() {
	case appctlpb.NoiseDH_NOISE_DH_UNSPECIFIED, appctlpb.NoiseDH_NOISE_DH_25519:
		cfg.DH = DH25519
	default:
		return Config{}, fmt.Errorf("noise: unsupported proto DH %v", pc.GetDh())
	}

	switch pc.GetCipher() {
	case appctlpb.NoiseCipher_NOISE_CIPHER_UNSPECIFIED, appctlpb.NoiseCipher_NOISE_CIPHER_CHACHA20POLY1305:
		cfg.Cipher = CipherChaChaPoly
	case appctlpb.NoiseCipher_NOISE_CIPHER_AES256GCM:
		cfg.Cipher = CipherAESGCM
	default:
		return Config{}, fmt.Errorf("noise: unsupported proto cipher %v", pc.GetCipher())
	}

	switch pc.GetHash() {
	case appctlpb.NoiseHash_NOISE_HASH_UNSPECIFIED, appctlpb.NoiseHash_NOISE_HASH_SHA256:
		cfg.Hash = HashSHA256
	case appctlpb.NoiseHash_NOISE_HASH_SHA512:
		cfg.Hash = HashSHA512
	case appctlpb.NoiseHash_NOISE_HASH_BLAKE2S:
		cfg.Hash = HashBLAKE2s
	case appctlpb.NoiseHash_NOISE_HASH_BLAKE2B:
		cfg.Hash = HashBLAKE2b
	default:
		return Config{}, fmt.Errorf("noise: unsupported proto hash %v", pc.GetHash())
	}

	// Decode keys only if provided. Validate() will later check that
	// required keys are present for the selected pattern and role.
	if s := pc.GetLocalStaticPrivateKey(); s != "" {
		priv, err := hex.DecodeString(s)
		if err != nil {
			return Config{}, fmt.Errorf("noise: decode localStaticPrivateKey: %w", err)
		}
		cfg.LocalStatic.Private = priv
	}
	if s := pc.GetLocalStaticPublicKey(); s != "" {
		pub, err := hex.DecodeString(s)
		if err != nil {
			return Config{}, fmt.Errorf("noise: decode localStaticPublicKey: %w", err)
		}
		cfg.LocalStatic.Public = pub
	}
	if len(cfg.LocalStatic.Private) > 0 && len(cfg.LocalStatic.Public) == 0 {
		// Derive the public key from the private key.
		pub, err := derivePublic(cfg.DH, cfg.LocalStatic.Private)
		if err != nil {
			return Config{}, err
		}
		cfg.LocalStatic.Public = pub
	}
	if s := pc.GetRemoteStaticPublicKey(); s != "" {
		pub, err := hex.DecodeString(s)
		if err != nil {
			return Config{}, fmt.Errorf("noise: decode remoteStaticPublicKey: %w", err)
		}
		cfg.RemoteStaticPublic = pub
	}
	if s := pc.GetPresharedKey(); s != "" {
		psk, err := hex.DecodeString(s)
		if err != nil {
			return Config{}, fmt.Errorf("noise: decode presharedKey: %w", err)
		}
		cfg.PresharedKey = psk
		cfg.PresharedKeyPlacement = int(pc.GetPresharedKeyPlacement())
	}
	if s := pc.GetPrologue(); s != "" {
		pro, err := hex.DecodeString(s)
		if err != nil {
			return Config{}, fmt.Errorf("noise: decode prologue: %w", err)
		}
		cfg.Prologue = pro
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// derivePublic computes the DH public key corresponding to priv.
func derivePublic(dh DH, priv []byte) ([]byte, error) {
	switch dh {
	case DH25519:
		if len(priv) != 32 {
			return nil, fmt.Errorf("noise: DH 25519 private key must be 32 bytes, got %d", len(priv))
		}
		pub := x25519Basepoint(priv)
		if len(pub) != 32 {
			return nil, fmt.Errorf("noise: derived public key has wrong length %d", len(pub))
		}
		return pub, nil
	}
	return nil, fmt.Errorf("noise: unsupported DH %q", dh)
}

// x25519Basepoint computes priv * basepoint on Curve25519, using the
// standard library's x/crypto implementation. Wrapped here so tests
// can override it; the real implementation lives in x25519.go.
var x25519Basepoint = x25519BasepointDefault
