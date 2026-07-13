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

package trafficpattern

import (
	mrand "math/rand"
	"strings"
	"testing"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"google.golang.org/protobuf/proto"
)

func TestExplicitValuesPreserved(t *testing.T) {
	origin := &appctlpb.TrafficPattern{
		Seed:      proto.Int32(12345),
		UnlockAll: proto.Bool(true),
		TcpFragment: &appctlpb.TCPFragment{
			Enable:     proto.Bool(true),
			MaxSleepMs: proto.Int32(50),
		},
		Nonce: &appctlpb.NoncePattern{
			Type:                appctlpb.NonceType_NONCE_TYPE_PRINTABLE.Enum(),
			ApplyToAllUDPPacket: proto.Bool(true),
			MinLen:              proto.Int32(5),
			MaxLen:              proto.Int32(10),
		},
		Padding: &appctlpb.PaddingPattern{
			MaxMiddlePaddingLen: proto.Int32(64),
			MaxEndPaddingLen:    proto.Int32(128),
		},
		LowEntropy: &appctlpb.LowEntropyPattern{
			Mode:         appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_48.Enum(),
			MaskRotation: appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_LEFT_3.Enum(),
		},
	}

	cfg, _ := NewConfig(origin)

	if cfg.effective.GetSeed() != 12345 {
		t.Errorf("expected seed 12345, got %d", cfg.effective.GetSeed())
	}
	if cfg.effective.GetUnlockAll() != true {
		t.Errorf("expected unlockAll true, got %v", cfg.effective.GetUnlockAll())
	}
	if cfg.effective.TcpFragment.GetEnable() != true {
		t.Errorf("expected tcpFragment.enable true, got %v", cfg.effective.TcpFragment.GetEnable())
	}
	if cfg.effective.TcpFragment.GetMaxSleepMs() != 50 {
		t.Errorf("expected tcpFragment.maxSleepMs 50, got %d", cfg.effective.TcpFragment.GetMaxSleepMs())
	}
	if cfg.effective.Nonce.GetType() != appctlpb.NonceType_NONCE_TYPE_PRINTABLE {
		t.Errorf("expected nonce.type PRINTABLE, got %v", cfg.effective.Nonce.GetType())
	}
	if cfg.effective.Nonce.GetApplyToAllUDPPacket() != true {
		t.Errorf("expected nonce.applyToAllUDPPacket true, got %v", cfg.effective.Nonce.GetApplyToAllUDPPacket())
	}
	if cfg.effective.Nonce.GetMinLen() != 5 {
		t.Errorf("expected nonce.minLen 5, got %d", cfg.effective.Nonce.GetMinLen())
	}
	if cfg.effective.Nonce.GetMaxLen() != 10 {
		t.Errorf("expected nonce.maxLen 10, got %d", cfg.effective.Nonce.GetMaxLen())
	}
	if cfg.effective.Padding.GetMaxMiddlePaddingLen() != 64 {
		t.Errorf("expected padding.maxMiddlePaddingLen 64, got %d", cfg.effective.Padding.GetMaxMiddlePaddingLen())
	}
	if cfg.effective.Padding.GetMaxEndPaddingLen() != 128 {
		t.Errorf("expected padding.maxEndPaddingLen 128, got %d", cfg.effective.Padding.GetMaxEndPaddingLen())
	}
	if cfg.effective.LowEntropy.GetMode() != appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_48 {
		t.Errorf("expected lowEntropy.mode LOW_ENTROPY_MODE_48, got %v", cfg.effective.LowEntropy.GetMode())
	}
	if cfg.effective.LowEntropy.GetMaskRotation() != appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_LEFT_3 {
		t.Errorf("expected lowEntropy.maskRotation LOW_ENTROPY_MASK_ROTATE_LEFT_3, got %v", cfg.effective.LowEntropy.GetMaskRotation())
	}
}

func TestImplicitValuesGenerated(t *testing.T) {
	origin := &appctlpb.TrafficPattern{}

	cfg, _ := NewConfig(origin)

	// TCPFragment should be initialized
	if cfg.effective.TcpFragment == nil {
		t.Fatal("expected tcpFragment to be initialized")
	}
	if cfg.effective.TcpFragment.Enable == nil {
		t.Error("expected tcpFragment.enable to be generated")
	}
	if cfg.effective.TcpFragment.MaxSleepMs == nil {
		t.Error("expected tcpFragment.maxSleepMs to be generated")
	}

	// NoncePattern should be initialized
	if cfg.effective.Nonce == nil {
		t.Fatal("expected nonce to be initialized")
	}
	if cfg.effective.Nonce.Type == nil {
		t.Error("expected nonce.type to be generated")
	}
	if cfg.effective.Nonce.ApplyToAllUDPPacket == nil {
		t.Error("expected nonce.applyToAllUDPPacket to be generated")
	}
	if cfg.effective.Nonce.MinLen == nil {
		t.Error("expected nonce.minLen to be generated")
	}
	if cfg.effective.Nonce.MaxLen == nil {
		t.Error("expected nonce.maxLen to be generated")
	}

	// PaddingPattern should be initialized
	if cfg.effective.Padding == nil {
		t.Fatal("expected padding to be initialized")
	}
	if cfg.effective.Padding.MaxMiddlePaddingLen == nil {
		t.Error("expected padding.maxMiddlePaddingLen to be generated")
	}
	if cfg.effective.Padding.MaxEndPaddingLen == nil {
		t.Error("expected padding.maxEndPaddingLen to be generated")
	}

	// LowEntropyPattern should be initialized
	if cfg.effective.LowEntropy == nil {
		t.Fatal("expected lowEntropy to be initialized")
	}
	if cfg.effective.LowEntropy.Mode == nil {
		t.Error("expected lowEntropy.mode to be generated")
	}
	if cfg.effective.LowEntropy.MaskRotation == nil {
		t.Error("expected lowEntropy.maskRotation to be generated")
	}
}

func TestPartialTCPFragmentPreserved(t *testing.T) {
	// When only some TCPFragment fields are set, preserve those and generate others
	origin := &appctlpb.TrafficPattern{
		Seed: proto.Int32(42),
		TcpFragment: &appctlpb.TCPFragment{
			Enable: proto.Bool(true),
			// MaxSleepMs not set
		},
	}

	cfg, _ := NewConfig(origin)

	// Enable should be preserved
	if cfg.effective.TcpFragment.GetEnable() != true {
		t.Errorf("expected tcpFragment.enable to be preserved as true, got %v", cfg.effective.TcpFragment.GetEnable())
	}

	// MaxSleepMs should be generated
	if cfg.effective.TcpFragment.MaxSleepMs == nil {
		t.Error("expected tcpFragment.maxSleepMs to be generated")
	}
}

func TestPartialNoncePatternPreserved(t *testing.T) {
	// When only some NoncePattern fields are set, preserve those and generate others
	origin := &appctlpb.TrafficPattern{
		Seed: proto.Int32(42),
		Nonce: &appctlpb.NoncePattern{
			Type: appctlpb.NonceType_NONCE_TYPE_PRINTABLE_SUBSET.Enum(),
			// Other fields not set
		},
	}

	cfg, _ := NewConfig(origin)

	// Type should be preserved
	if cfg.effective.Nonce.GetType() != appctlpb.NonceType_NONCE_TYPE_PRINTABLE_SUBSET {
		t.Errorf("expected nonce.type to be preserved as PRINTABLE_SUBSET, got %v", cfg.effective.Nonce.GetType())
	}

	// Other fields should be generated
	if cfg.effective.Nonce.ApplyToAllUDPPacket == nil {
		t.Error("expected nonce.applyToAllUDPPacket to be generated")
	}
	if cfg.effective.Nonce.MinLen == nil {
		t.Error("expected nonce.minLen to be generated")
	}
	if cfg.effective.Nonce.MaxLen == nil {
		t.Error("expected nonce.maxLen to be generated")
	}
}

func TestPartialPaddingPatternPreserved(t *testing.T) {
	origin := &appctlpb.TrafficPattern{
		Seed: proto.Int32(42),
		Padding: &appctlpb.PaddingPattern{
			MaxMiddlePaddingLen: proto.Int32(99),
			// MaxEndPaddingLen not set
		},
	}

	cfg, _ := NewConfig(origin)

	if cfg.effective.Padding.GetMaxMiddlePaddingLen() != 99 {
		t.Errorf("expected padding.maxMiddlePaddingLen to be preserved as 99, got %d", cfg.effective.Padding.GetMaxMiddlePaddingLen())
	}
	if cfg.effective.Padding.MaxEndPaddingLen == nil {
		t.Error("expected padding.maxEndPaddingLen to be generated")
	}
}

func TestPartialLowEntropyPatternPreserved(t *testing.T) {
	origin := &appctlpb.TrafficPattern{
		Seed: proto.Int32(42),
		LowEntropy: &appctlpb.LowEntropyPattern{
			Mode: appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_40.Enum(),
		},
	}

	cfg, err := NewConfig(origin)
	if err != nil {
		t.Fatalf("NewConfig() failed: %v", err)
	}
	if cfg.effective.LowEntropy.GetMode() != appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_40 {
		t.Errorf("expected lowEntropy.mode to be preserved as LOW_ENTROPY_MODE_40, got %v", cfg.effective.LowEntropy.GetMode())
	}
	if cfg.effective.LowEntropy.MaskRotation == nil {
		t.Error("expected lowEntropy.maskRotation to be generated")
	}
}

func TestDeterministicGeneration(t *testing.T) {
	origin := &appctlpb.TrafficPattern{}

	cfg1, _ := NewConfig(origin)
	cfg2, _ := NewConfig(origin)

	if cfg1.effective.TcpFragment.GetEnable() != cfg2.effective.TcpFragment.GetEnable() {
		t.Error("tcpFragment.enable should be deterministic")
	}
	if cfg1.effective.TcpFragment.GetMaxSleepMs() != cfg2.effective.TcpFragment.GetMaxSleepMs() {
		t.Error("tcpFragment.maxSleepMs should be deterministic")
	}
	if cfg1.effective.Nonce.GetType() != cfg2.effective.Nonce.GetType() {
		t.Error("nonce.type should be deterministic")
	}
	if cfg1.effective.Nonce.GetApplyToAllUDPPacket() != cfg2.effective.Nonce.GetApplyToAllUDPPacket() {
		t.Error("nonce.applyToAllUDPPacket should be deterministic")
	}
	if cfg1.effective.Nonce.GetMinLen() != cfg2.effective.Nonce.GetMinLen() {
		t.Error("nonce.minLen should be deterministic")
	}
	if cfg1.effective.Nonce.GetMaxLen() != cfg2.effective.Nonce.GetMaxLen() {
		t.Error("nonce.maxLen should be deterministic")
	}
	if cfg1.effective.Padding.GetMaxMiddlePaddingLen() != cfg2.effective.Padding.GetMaxMiddlePaddingLen() {
		t.Error("padding.maxMiddlePaddingLen should be deterministic")
	}
	if cfg1.effective.Padding.GetMaxEndPaddingLen() != cfg2.effective.Padding.GetMaxEndPaddingLen() {
		t.Error("padding.maxEndPaddingLen should be deterministic")
	}
	if cfg1.effective.LowEntropy.GetMode() != cfg2.effective.LowEntropy.GetMode() {
		t.Error("lowEntropy.mode should be deterministic")
	}
	if cfg1.effective.LowEntropy.GetMaskRotation() != cfg2.effective.LowEntropy.GetMaskRotation() {
		t.Error("lowEntropy.maskRotation should be deterministic")
	}
}

func TestMinLenMaxLen(t *testing.T) {
	testSeeds := make([]int32, 100)
	for i := 0; i < 100; i++ {
		testSeeds[i] = mrand.Int31()
	}

	for _, seed := range testSeeds {
		for _, unlockAll := range []bool{false, true} {
			origin := &appctlpb.TrafficPattern{
				Seed:      proto.Int32(seed),
				UnlockAll: proto.Bool(unlockAll),
			}

			cfg, _ := NewConfig(origin)

			minLen := cfg.effective.Nonce.GetMinLen()
			maxLen := cfg.effective.Nonce.GetMaxLen()

			if minLen < 0 || minLen > 12 {
				t.Errorf("seed=%d, unlockAll=%v: minLen (%d) is out of range [0, 12]", seed, unlockAll, minLen)
			}
			if maxLen < 0 || maxLen > 12 {
				t.Errorf("seed=%d, unlockAll=%v: maxLen (%d) is out of range [0, 12]", seed, unlockAll, maxLen)
			}
			if minLen > maxLen {
				t.Errorf("seed=%d, unlockAll=%v: minLen (%d) > maxLen (%d)", seed, unlockAll, minLen, maxLen)
			}
		}
	}
}

func TestEncodeDecode(t *testing.T) {
	origin := &appctlpb.TrafficPattern{
		UnlockAll: proto.Bool(true),
		TcpFragment: &appctlpb.TCPFragment{
			Enable:     proto.Bool(true),
			MaxSleepMs: proto.Int32(50),
		},
		Nonce: &appctlpb.NoncePattern{
			Type:                appctlpb.NonceType_NONCE_TYPE_PRINTABLE.Enum(),
			ApplyToAllUDPPacket: proto.Bool(true),
			MinLen:              proto.Int32(6),
			MaxLen:              proto.Int32(8),
		},
		Padding: &appctlpb.PaddingPattern{
			MaxMiddlePaddingLen: proto.Int32(64),
			MaxEndPaddingLen:    proto.Int32(128),
		},
	}

	encoded := Encode(origin)

	if encoded == "" {
		t.Fatal("Encode() returned empty string")
	}
	t.Logf("Encoded traffic pattern: %s", encoded)

	restored, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode() failed: %v", err)
	}

	// Verify restored values match effective config
	if restored.GetSeed() != origin.GetSeed() {
		t.Errorf("seed mismatch: expected %d, got %d", origin.GetSeed(), restored.GetSeed())
	}
	if restored.GetUnlockAll() != origin.GetUnlockAll() {
		t.Errorf("unlockAll mismatch: expected %v, got %v", origin.GetUnlockAll(), restored.GetUnlockAll())
	}
	if restored.TcpFragment.GetEnable() != origin.TcpFragment.GetEnable() {
		t.Errorf("tcpFragment.enable mismatch: expected %v, got %v", origin.TcpFragment.GetEnable(), restored.TcpFragment.GetEnable())
	}
	if restored.TcpFragment.GetMaxSleepMs() != origin.TcpFragment.GetMaxSleepMs() {
		t.Errorf("tcpFragment.maxSleepMs mismatch: expected %d, got %d", origin.TcpFragment.GetMaxSleepMs(), restored.TcpFragment.GetMaxSleepMs())
	}
	if restored.Nonce.GetType() != origin.Nonce.GetType() {
		t.Errorf("nonce.type mismatch: expected %v, got %v", origin.Nonce.GetType(), restored.Nonce.GetType())
	}
	if restored.Nonce.GetApplyToAllUDPPacket() != origin.Nonce.GetApplyToAllUDPPacket() {
		t.Errorf("nonce.applyToAllUDPPacket mismatch: expected %v, got %v", origin.Nonce.GetApplyToAllUDPPacket(), restored.Nonce.GetApplyToAllUDPPacket())
	}
	if restored.Nonce.GetMinLen() != origin.Nonce.GetMinLen() {
		t.Errorf("nonce.minLen mismatch: expected %d, got %d", origin.Nonce.GetMinLen(), restored.Nonce.GetMinLen())
	}
	if restored.Nonce.GetMaxLen() != origin.Nonce.GetMaxLen() {
		t.Errorf("nonce.maxLen mismatch: expected %d, got %d", origin.Nonce.GetMaxLen(), restored.Nonce.GetMaxLen())
	}
	if restored.Padding.GetMaxMiddlePaddingLen() != origin.Padding.GetMaxMiddlePaddingLen() {
		t.Errorf("padding.maxMiddlePaddingLen mismatch: expected %d, got %d", origin.Padding.GetMaxMiddlePaddingLen(), restored.Padding.GetMaxMiddlePaddingLen())
	}
	if restored.Padding.GetMaxEndPaddingLen() != origin.Padding.GetMaxEndPaddingLen() {
		t.Errorf("padding.maxEndPaddingLen mismatch: expected %d, got %d", origin.Padding.GetMaxEndPaddingLen(), restored.Padding.GetMaxEndPaddingLen())
	}
}

func TestDecodeEmptyString(t *testing.T) {
	trafficPattern, err := Decode("")
	if err != nil {
		t.Fatalf("Failed to decode empty string: %v", err)
	}
	if trafficPattern == nil {
		t.Errorf("Returned nil traffic pattern")
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name          string
		pattern       *appctlpb.TrafficPattern
		wantErrString string
	}{
		{
			name:          "nil_pattern",
			pattern:       nil,
			wantErrString: "",
		},
		{
			name:          "empty_pattern",
			pattern:       &appctlpb.TrafficPattern{},
			wantErrString: "",
		},
		{
			name: "valid_tcp_fragment",
			pattern: &appctlpb.TrafficPattern{
				TcpFragment: &appctlpb.TCPFragment{
					Enable:     proto.Bool(true),
					MaxSleepMs: proto.Int32(100),
				},
			},
			wantErrString: "",
		},
		{
			name: "tcp_fragment_max_sleep_exceeds_100",
			pattern: &appctlpb.TrafficPattern{
				TcpFragment: &appctlpb.TCPFragment{
					Enable:     proto.Bool(true),
					MaxSleepMs: proto.Int32(101),
				},
			},
			wantErrString: "exceeds maximum value 100",
		},
		{
			name: "tcp_fragment_max_sleep_negative",
			pattern: &appctlpb.TrafficPattern{
				TcpFragment: &appctlpb.TCPFragment{
					MaxSleepMs: proto.Int32(-1),
				},
			},
			wantErrString: "is negative",
		},
		{
			name: "valid_nonce_pattern",
			pattern: &appctlpb.TrafficPattern{
				Nonce: &appctlpb.NoncePattern{
					MinLen: proto.Int32(12),
					MaxLen: proto.Int32(12),
				},
			},
			wantErrString: "",
		},
		{
			name: "nonce_max_len_exceeds_12",
			pattern: &appctlpb.TrafficPattern{
				Nonce: &appctlpb.NoncePattern{
					MaxLen: proto.Int32(13),
				},
			},
			wantErrString: "maxLen 13 exceeds maximum value 12",
		},
		{
			name: "nonce_min_len_exceeds_12",
			pattern: &appctlpb.TrafficPattern{
				Nonce: &appctlpb.NoncePattern{
					MinLen: proto.Int32(13),
				},
			},
			wantErrString: "minLen 13 exceeds maximum value 12",
		},
		{
			name: "nonce_max_len_negative",
			pattern: &appctlpb.TrafficPattern{
				Nonce: &appctlpb.NoncePattern{
					MaxLen: proto.Int32(-1),
				},
			},
			wantErrString: "maxLen -1 is negative",
		},
		{
			name: "nonce_min_len_negative",
			pattern: &appctlpb.TrafficPattern{
				Nonce: &appctlpb.NoncePattern{
					MinLen: proto.Int32(-1),
				},
			},
			wantErrString: "minLen -1 is negative",
		},
		{
			name: "nonce_min_len_greater_than_max_len",
			pattern: &appctlpb.TrafficPattern{
				Nonce: &appctlpb.NoncePattern{
					MinLen: proto.Int32(8),
					MaxLen: proto.Int32(4),
				},
			},
			wantErrString: "minLen 8 is greater than maxLen 4",
		},
		{
			name: "valid_custom_hex_strings",
			pattern: &appctlpb.TrafficPattern{
				Nonce: &appctlpb.NoncePattern{
					Type:             appctlpb.NonceType_NONCE_TYPE_FIXED.Enum(),
					CustomHexStrings: []string{"00010203", "aabbccdd"},
				},
			},
			wantErrString: "",
		},
		{
			name: "valid_custom_hex_strings_max_length",
			pattern: &appctlpb.TrafficPattern{
				Nonce: &appctlpb.NoncePattern{
					Type:             appctlpb.NonceType_NONCE_TYPE_FIXED.Enum(),
					CustomHexStrings: []string{"000102030405060708090a0b"},
				},
			},
			wantErrString: "",
		},
		{
			name: "custom_hex_string_exceeds_12_bytes",
			pattern: &appctlpb.TrafficPattern{
				Nonce: &appctlpb.NoncePattern{
					Type:             appctlpb.NonceType_NONCE_TYPE_FIXED.Enum(),
					CustomHexStrings: []string{"000102030405060708090a0b0c"},
				},
			},
			wantErrString: "exceeds maximum 12 bytes",
		},
		{
			name: "custom_hex_string_invalid",
			pattern: &appctlpb.TrafficPattern{
				Nonce: &appctlpb.NoncePattern{
					Type:             appctlpb.NonceType_NONCE_TYPE_FIXED.Enum(),
					CustomHexStrings: []string{"not_valid_hex"},
				},
			},
			wantErrString: "is not a valid hex string",
		},
		{
			name: "custom_hex_string_odd_length",
			pattern: &appctlpb.TrafficPattern{
				Nonce: &appctlpb.NoncePattern{
					Type:             appctlpb.NonceType_NONCE_TYPE_FIXED.Enum(),
					CustomHexStrings: []string{"abc"},
				},
			},
			wantErrString: "is not a valid hex string",
		},
		{
			name: "valid_padding_pattern",
			pattern: &appctlpb.TrafficPattern{
				Padding: &appctlpb.PaddingPattern{
					MaxMiddlePaddingLen: proto.Int32(0),
					MaxEndPaddingLen:    proto.Int32(255),
				},
			},
			wantErrString: "",
		},
		{
			name: "padding_max_middle_padding_len_exceeds_255",
			pattern: &appctlpb.TrafficPattern{
				Padding: &appctlpb.PaddingPattern{
					MaxMiddlePaddingLen: proto.Int32(256),
				},
			},
			wantErrString: "maxMiddlePaddingLen 256 exceeds maximum value 255",
		},
		{
			name: "padding_max_middle_padding_len_negative",
			pattern: &appctlpb.TrafficPattern{
				Padding: &appctlpb.PaddingPattern{
					MaxMiddlePaddingLen: proto.Int32(-1),
				},
			},
			wantErrString: "maxMiddlePaddingLen -1 is negative",
		},
		{
			name: "padding_max_end_padding_len_exceeds_255",
			pattern: &appctlpb.TrafficPattern{
				Padding: &appctlpb.PaddingPattern{
					MaxEndPaddingLen: proto.Int32(256),
				},
			},
			wantErrString: "maxEndPaddingLen 256 exceeds maximum value 255",
		},
		{
			name: "padding_max_end_padding_len_negative",
			pattern: &appctlpb.TrafficPattern{
				Padding: &appctlpb.PaddingPattern{
					MaxEndPaddingLen: proto.Int32(-1),
				},
			},
			wantErrString: "maxEndPaddingLen -1 is negative",
		},
		{
			name: "valid_low_entropy_pattern",
			pattern: &appctlpb.TrafficPattern{
				LowEntropy: &appctlpb.LowEntropyPattern{
					Mode:         appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_56.Enum(),
					MaskRotation: appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_RIGHT_15.Enum(),
				},
			},
			wantErrString: "",
		},
		{
			name: "low_entropy_mode_invalid",
			pattern: &appctlpb.TrafficPattern{
				LowEntropy: &appctlpb.LowEntropyPattern{
					Mode: appctlpb.LowEntropyMode(5).Enum(),
				},
			},
			wantErrString: "mode 5 is invalid",
		},
		{
			name: "low_entropy_mask_rotation_invalid",
			pattern: &appctlpb.TrafficPattern{
				LowEntropy: &appctlpb.LowEntropyPattern{
					MaskRotation: appctlpb.LowEntropyMaskRotation(17).Enum(),
				},
			},
			wantErrString: "maskRotation 17 is invalid",
		},
		{
			name: "valid_full_pattern",
			pattern: &appctlpb.TrafficPattern{
				Seed: proto.Int32(12345),
				TcpFragment: &appctlpb.TCPFragment{
					Enable:     proto.Bool(true),
					MaxSleepMs: proto.Int32(50),
				},
				Nonce: &appctlpb.NoncePattern{
					Type:                appctlpb.NonceType_NONCE_TYPE_FIXED.Enum(),
					ApplyToAllUDPPacket: proto.Bool(true),
					MinLen:              proto.Int32(4),
					MaxLen:              proto.Int32(8),
					CustomHexStrings:    []string{"00010203"},
				},
				Padding: &appctlpb.PaddingPattern{
					MaxMiddlePaddingLen: proto.Int32(127),
					MaxEndPaddingLen:    proto.Int32(255),
				},
			},
			wantErrString: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Validate(c.pattern)
			if c.wantErrString == "" {
				if err != nil {
					t.Errorf("Validate() returned unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("Validate() expected error to contain %q, got nil", c.wantErrString)
				} else if !strings.Contains(err.Error(), c.wantErrString) {
					t.Errorf("Validate() error = %q, want error to contain %q", err.Error(), c.wantErrString)
				}
			}
		})
	}
}
