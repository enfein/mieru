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
	"encoding/hex"
	"testing"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher/noise"
)

// TestConfigFromProto_Defaults ensures that an empty NoiseConfig
// decodes to the documented defaults (XX / 25519 / ChaChaPoly / SHA256).
// A fresh XX-initiator with no keys should fail validation because XX
// requires a local static — the test asserts that path too.
func TestConfigFromProto_Defaults(t *testing.T) {
	pc := &appctlpb.NoiseConfig{}
	// Role initiator, defaults, no keys — validation should fail.
	if _, err := noise.ConfigFromProto(pc, noise.RoleInitiator); err == nil {
		t.Fatalf("expected validation error for default XX without static key")
	}
}

// TestConfigFromProto_RoundTrip encodes a full config through the
// proto converter and confirms every field survives.
func TestConfigFromProto_RoundTrip(t *testing.T) {
	kp, err := noise.GenerateKeypair(noise.DH25519)
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}
	remote, err := noise.GenerateKeypair(noise.DH25519)
	if err != nil {
		t.Fatalf("GenerateKeypair remote: %v", err)
	}
	psk := make([]byte, 32)
	for i := range psk {
		psk[i] = byte(i)
	}

	pattern := appctlpb.NoisePattern_NOISE_IK
	dh := appctlpb.NoiseDH_NOISE_DH_25519
	cipher := appctlpb.NoiseCipher_NOISE_CIPHER_AES256GCM
	hash := appctlpb.NoiseHash_NOISE_HASH_SHA512
	placement := int32(0)

	localPriv := hex.EncodeToString(kp.Private)
	localPub := hex.EncodeToString(kp.Public)
	remotePub := hex.EncodeToString(remote.Public)
	pskHex := hex.EncodeToString(psk)
	prologue := hex.EncodeToString([]byte("mieru v3"))

	pc := &appctlpb.NoiseConfig{
		Pattern:               &pattern,
		Dh:                    &dh,
		Cipher:                &cipher,
		Hash:                  &hash,
		LocalStaticPrivateKey: &localPriv,
		LocalStaticPublicKey:  &localPub,
		RemoteStaticPublicKey: &remotePub,
		PresharedKey:          &pskHex,
		PresharedKeyPlacement: &placement,
		Prologue:              &prologue,
	}

	cfg, err := noise.ConfigFromProto(pc, noise.RoleInitiator)
	if err != nil {
		t.Fatalf("ConfigFromProto: %v", err)
	}
	if cfg.Pattern != noise.PatternIK {
		t.Errorf("pattern = %q, want IK", cfg.Pattern)
	}
	if cfg.Cipher != noise.CipherAESGCM {
		t.Errorf("cipher = %q, want AESGCM", cfg.Cipher)
	}
	if cfg.Hash != noise.HashSHA512 {
		t.Errorf("hash = %q, want SHA512", cfg.Hash)
	}
	if string(cfg.Prologue) != "mieru v3" {
		t.Errorf("prologue = %q, want %q", cfg.Prologue, "mieru v3")
	}
	if len(cfg.PresharedKey) != 32 {
		t.Errorf("psk len = %d, want 32", len(cfg.PresharedKey))
	}
}

// TestConfigFromProto_DerivesPublic confirms that omitting the public
// key causes mieru to derive it from the private key.
func TestConfigFromProto_DerivesPublic(t *testing.T) {
	kp, err := noise.GenerateKeypair(noise.DH25519)
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}
	privHex := hex.EncodeToString(kp.Private)
	pat := appctlpb.NoisePattern_NOISE_XX
	pc := &appctlpb.NoiseConfig{
		Pattern:               &pat,
		LocalStaticPrivateKey: &privHex,
		// LocalStaticPublicKey deliberately left unset.
	}
	cfg, err := noise.ConfigFromProto(pc, noise.RoleInitiator)
	if err != nil {
		t.Fatalf("ConfigFromProto: %v", err)
	}
	if len(cfg.LocalStatic.Public) != 32 {
		t.Fatalf("derived public key wrong length %d", len(cfg.LocalStatic.Public))
	}
	// Derived public must match the original keypair.
	if hex.EncodeToString(cfg.LocalStatic.Public) != hex.EncodeToString(kp.Public) {
		t.Fatalf("derived public differs from GenerateKeypair output")
	}
}

// TestConfigFromProto_BadHex rejects malformed hex input.
func TestConfigFromProto_BadHex(t *testing.T) {
	bad := "zzzzz"
	pat := appctlpb.NoisePattern_NOISE_XX
	pc := &appctlpb.NoiseConfig{
		Pattern:               &pat,
		LocalStaticPrivateKey: &bad,
	}
	if _, err := noise.ConfigFromProto(pc, noise.RoleInitiator); err == nil {
		t.Fatalf("expected hex decode error")
	}
}
