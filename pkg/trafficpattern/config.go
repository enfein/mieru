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
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/rng"
	"google.golang.org/protobuf/proto"
)

// Config stores the traffic pattern configuration.
type Config struct {
	original  *appctlpb.TrafficPattern
	effective *appctlpb.TrafficPattern
}

// NewConfig creates a new traffic pattern configuration from a protobuf message.
// It assumes the original protobuf message is valid.
func NewConfig(original *appctlpb.TrafficPattern) *Config {
	if original == nil {
		// Use an empty traffic pattern.
		original = &appctlpb.TrafficPattern{}
	}
	c := &Config{
		original:  original,
		effective: proto.Clone(original).(*appctlpb.TrafficPattern),
	}
	c.generateImplicitTrafficPattern()
	return c
}

// Original returns the original traffic pattern.
func (c *Config) Original() *appctlpb.TrafficPattern {
	return c.original
}

// Effective returns the effective traffic pattern.
func (c *Config) Effective() *appctlpb.TrafficPattern {
	return c.effective
}

// Encode returns the base64 encoded string of the traffic pattern
// from the protobuf message binary.
func Encode(pattern *appctlpb.TrafficPattern) string {
	b, err := proto.Marshal(pattern)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(b)
}

// Decode decodes the base64 encoded string of the traffic pattern
// into the protobuf message.
func Decode(encoded string) (*appctlpb.TrafficPattern, error) {
	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode base64 string failed: %w", err)
	}
	pattern := &appctlpb.TrafficPattern{}
	if err := proto.Unmarshal(b, pattern); err != nil {
		return nil, fmt.Errorf("proto.Unmarshal() failed: %w", err)
	}
	return pattern, nil
}

// Validate validates the traffic pattern protobuf message.
func Validate(pattern *appctlpb.TrafficPattern) error {
	if pattern == nil {
		return nil
	}
	if err := validateTCPFragment(pattern.GetTcpFragment()); err != nil {
		return err
	}
	if err := validateNoncePattern(pattern.GetNonce()); err != nil {
		return err
	}
	return nil
}

func (c *Config) generateImplicitTrafficPattern() {
	seed := int(c.original.GetSeed())
	if c.original.Seed == nil {
		seed = rng.FixedIntVH(math.MaxInt32)
	}
	unlockAll := c.original.GetUnlockAll()
	c.generateTCPFragment(seed, unlockAll)
	c.generateNoncePattern(seed, unlockAll)
}

func (c *Config) generateTCPFragment(seed int, unlockAll bool) {
	if c.effective.TcpFragment == nil {
		c.effective.TcpFragment = &appctlpb.TCPFragment{}
	}
	f := c.effective.TcpFragment

	if c.original.TcpFragment == nil || c.original.TcpFragment.Enable == nil {
		if unlockAll {
			f.Enable = proto.Bool(rng.FixedInt(2, fmt.Sprintf("%d:tcpFragment.enable", seed)) == 1)
		} else {
			f.Enable = proto.Bool(false)
		}
	}

	if c.original.TcpFragment == nil || c.original.TcpFragment.MaxSleepMs == nil {
		maxRange := 100
		// Generate a random number in [1, 100]
		f.MaxSleepMs = proto.Int32(int32(rng.FixedInt(maxRange, fmt.Sprintf("%d:tcpFragment.maxSleepMs", seed))) + 1)
	}
}

func (c *Config) generateNoncePattern(seed int, unlockAll bool) {
	if c.effective.Nonce == nil {
		c.effective.Nonce = &appctlpb.NoncePattern{}
	}
	n := c.effective.Nonce

	if c.original.Nonce == nil || c.original.Nonce.Type == nil {
		// Never generate NONCE_TYPE_FIXED (3) since it requires customHexStrings.
		if unlockAll {
			// Generate a random number in [0, 2]
			typeRange := 3
			n.Type = appctlpb.NonceType(rng.FixedInt(typeRange, fmt.Sprintf("%d:nonce.type", seed))).Enum()
		} else {
			// Generate a random number in [1, 2]
			typeRange := 2
			n.Type = appctlpb.NonceType(rng.FixedInt(typeRange, fmt.Sprintf("%d:nonce.type", seed)) + 1).Enum()
		}
	}

	if c.original.Nonce == nil || c.original.Nonce.ApplyToAllUDPPacket == nil {
		if unlockAll {
			n.ApplyToAllUDPPacket = proto.Bool(rng.FixedInt(2, fmt.Sprintf("%d:nonce.applyToAllUDPPacket", seed)) == 1)
		} else {
			n.ApplyToAllUDPPacket = proto.Bool(false)
		}
	}

	if c.original.Nonce == nil || c.original.Nonce.MinLen == nil {
		if unlockAll {
			// Generate a random number in [0, 12]
			minRange := 13
			n.MinLen = proto.Int32(int32(rng.FixedInt(minRange, fmt.Sprintf("%d:nonce.minLen", seed))))
		} else {
			// Generate a random number in [6, 12]
			minRange := 7
			n.MinLen = proto.Int32(int32(rng.FixedInt(minRange, fmt.Sprintf("%d:nonce.minLen", seed))) + 6)
		}
	}

	if c.original.Nonce == nil || c.original.Nonce.MaxLen == nil {
		minLen := int(n.GetMinLen())
		n.MaxLen = proto.Int32(int32(minLen + rng.FixedInt(13-minLen, fmt.Sprintf("%d:nonce.maxLen", seed))))
	}
}

func validateTCPFragment(fragment *appctlpb.TCPFragment) error {
	if fragment == nil {
		return nil
	}
	if fragment.MaxSleepMs != nil {
		if fragment.GetMaxSleepMs() < 0 {
			return fmt.Errorf("TCPFragment maxSleepMs %d is negative", fragment.GetMaxSleepMs())
		}
		if fragment.GetMaxSleepMs() > 100 {
			return fmt.Errorf("TCPFragment maxSleepMs %d exceeds maximum value 100", fragment.GetMaxSleepMs())
		}
	}
	return nil
}

func validateNoncePattern(nonce *appctlpb.NoncePattern) error {
	if nonce == nil {
		return nil
	}
	if nonce.MinLen != nil {
		if nonce.GetMinLen() < 0 {
			return fmt.Errorf("NoncePattern minLen %d is negative", nonce.GetMinLen())
		}
		if nonce.GetMinLen() > 12 {
			return fmt.Errorf("NoncePattern minLen %d exceeds maximum value 12", nonce.GetMinLen())
		}
	}
	if nonce.MaxLen != nil {
		if nonce.GetMaxLen() < 0 {
			return fmt.Errorf("NoncePattern maxLen %d is negative", nonce.GetMaxLen())
		}
		if nonce.GetMaxLen() > 12 {
			return fmt.Errorf("NoncePattern maxLen %d exceeds maximum value 12", nonce.GetMaxLen())
		}
	}
	if nonce.MinLen != nil && nonce.MaxLen != nil {
		if nonce.GetMinLen() > nonce.GetMaxLen() {
			return fmt.Errorf("NoncePattern minLen %d is greater than maxLen %d", nonce.GetMinLen(), nonce.GetMaxLen())
		}
	}
	for i, hexStr := range nonce.GetCustomHexStrings() {
		decoded, err := hex.DecodeString(hexStr)
		if err != nil {
			return fmt.Errorf("NoncePattern customHexStrings[%d] %q is not a valid hex string: %w", i, hexStr, err)
		}
		if len(decoded) > 12 {
			return fmt.Errorf("NoncePattern customHexStrings[%d] decoded length %d exceeds maximum 12 bytes", i, len(decoded))
		}
	}
	return nil
}
