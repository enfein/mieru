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

package appctlcommon

import (
	"strings"
	"testing"

	pb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"google.golang.org/protobuf/proto"
)

func TestValidateTrafficPattern(t *testing.T) {
	cases := []struct {
		name          string
		pattern       *pb.TrafficPattern
		wantErrString string
	}{
		{
			name:          "nil_pattern",
			pattern:       nil,
			wantErrString: "",
		},
		{
			name:          "empty_pattern",
			pattern:       &pb.TrafficPattern{},
			wantErrString: "",
		},
		{
			name: "valid_tcp_fragment",
			pattern: &pb.TrafficPattern{
				TcpFragment: &pb.TCPFragment{
					Enable:     proto.Bool(true),
					MaxSleepMs: proto.Int32(100),
				},
			},
			wantErrString: "",
		},
		{
			name: "tcp_fragment_max_sleep_exceeds_100",
			pattern: &pb.TrafficPattern{
				TcpFragment: &pb.TCPFragment{
					Enable:     proto.Bool(true),
					MaxSleepMs: proto.Int32(101),
				},
			},
			wantErrString: "exceeds maximum value 100",
		},
		{
			name: "tcp_fragment_max_sleep_negative",
			pattern: &pb.TrafficPattern{
				TcpFragment: &pb.TCPFragment{
					MaxSleepMs: proto.Int32(-1),
				},
			},
			wantErrString: "is negative",
		},
		{
			name: "valid_nonce_pattern",
			pattern: &pb.TrafficPattern{
				Nonce: &pb.NoncePattern{
					MinLen: proto.Int32(12),
					MaxLen: proto.Int32(12),
				},
			},
			wantErrString: "",
		},
		{
			name: "nonce_max_len_exceeds_12",
			pattern: &pb.TrafficPattern{
				Nonce: &pb.NoncePattern{
					MaxLen: proto.Int32(13),
				},
			},
			wantErrString: "maxLen 13 exceeds maximum value 12",
		},
		{
			name: "nonce_min_len_exceeds_12",
			pattern: &pb.TrafficPattern{
				Nonce: &pb.NoncePattern{
					MinLen: proto.Int32(13),
				},
			},
			wantErrString: "minLen 13 exceeds maximum value 12",
		},
		{
			name: "nonce_max_len_negative",
			pattern: &pb.TrafficPattern{
				Nonce: &pb.NoncePattern{
					MaxLen: proto.Int32(-1),
				},
			},
			wantErrString: "maxLen -1 is negative",
		},
		{
			name: "nonce_min_len_negative",
			pattern: &pb.TrafficPattern{
				Nonce: &pb.NoncePattern{
					MinLen: proto.Int32(-1),
				},
			},
			wantErrString: "minLen -1 is negative",
		},
		{
			name: "nonce_min_len_greater_than_max_len",
			pattern: &pb.TrafficPattern{
				Nonce: &pb.NoncePattern{
					MinLen: proto.Int32(8),
					MaxLen: proto.Int32(4),
				},
			},
			wantErrString: "minLen 8 is greater than maxLen 4",
		},
		{
			name: "valid_custom_hex_strings",
			pattern: &pb.TrafficPattern{
				Nonce: &pb.NoncePattern{
					Type:             pb.NonceType_NONCE_TYPE_FIXED.Enum(),
					CustomHexStrings: []string{"00010203", "aabbccdd"},
				},
			},
			wantErrString: "",
		},
		{
			name: "valid_custom_hex_strings_max_length",
			pattern: &pb.TrafficPattern{
				Nonce: &pb.NoncePattern{
					Type:             pb.NonceType_NONCE_TYPE_FIXED.Enum(),
					CustomHexStrings: []string{"000102030405060708090a0b"},
				},
			},
			wantErrString: "",
		},
		{
			name: "custom_hex_string_exceeds_12_bytes",
			pattern: &pb.TrafficPattern{
				Nonce: &pb.NoncePattern{
					Type:             pb.NonceType_NONCE_TYPE_FIXED.Enum(),
					CustomHexStrings: []string{"000102030405060708090a0b0c"},
				},
			},
			wantErrString: "exceeds maximum 12 bytes",
		},
		{
			name: "custom_hex_string_invalid",
			pattern: &pb.TrafficPattern{
				Nonce: &pb.NoncePattern{
					Type:             pb.NonceType_NONCE_TYPE_FIXED.Enum(),
					CustomHexStrings: []string{"not_valid_hex"},
				},
			},
			wantErrString: "is not a valid hex string",
		},
		{
			name: "custom_hex_string_odd_length",
			pattern: &pb.TrafficPattern{
				Nonce: &pb.NoncePattern{
					Type:             pb.NonceType_NONCE_TYPE_FIXED.Enum(),
					CustomHexStrings: []string{"abc"},
				},
			},
			wantErrString: "is not a valid hex string",
		},
		{
			name: "valid_full_pattern",
			pattern: &pb.TrafficPattern{
				Seed: proto.Int32(12345),
				TcpFragment: &pb.TCPFragment{
					Enable:     proto.Bool(true),
					MaxSleepMs: proto.Int32(50),
				},
				Nonce: &pb.NoncePattern{
					Type:                pb.NonceType_NONCE_TYPE_FIXED.Enum(),
					ApplyToAllUDPPacket: proto.Bool(true),
					MinLen:              proto.Int32(4),
					MaxLen:              proto.Int32(8),
					CustomHexStrings:    []string{"00010203"},
				},
			},
			wantErrString: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateTrafficPattern(c.pattern)
			if c.wantErrString == "" {
				if err != nil {
					t.Errorf("ValidateTrafficPattern() returned unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("ValidateTrafficPattern() expected error to contain %q, got nil", c.wantErrString)
				} else if !strings.Contains(err.Error(), c.wantErrString) {
					t.Errorf("ValidateTrafficPattern() error = %q, want error to contain %q", err.Error(), c.wantErrString)
				}
			}
		})
	}
}
