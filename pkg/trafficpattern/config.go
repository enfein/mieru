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
	"fmt"
	"math"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/rng"
	"google.golang.org/protobuf/proto"
)

// Config stores the traffic pattern configuration.
type Config struct {
	origin    *appctlpb.TrafficPattern
	effective *appctlpb.TrafficPattern
}

// NewConfig creates a new traffic pattern configuration from a protobuf message.
// Assume the origin protobuf message is valid.
func NewConfig(origin *appctlpb.TrafficPattern) *Config {
	if origin == nil {
		panic("TrafficPattern is nil")
	}
	c := &Config{
		origin:    origin,
		effective: proto.Clone(origin).(*appctlpb.TrafficPattern),
	}
	c.generateImplicitTrafficPattern()
	return c
}

func (c *Config) generateImplicitTrafficPattern() {
	seed := int(c.origin.GetSeed())
	if c.origin.Seed == nil {
		seed = rng.FixedIntVH(math.MaxInt32)
	}
	unlockAll := c.origin.GetUnlockAll()
	c.generateTCPFragment(seed, unlockAll)
	c.generateNoncePattern(seed, unlockAll)
}

func (c *Config) generateTCPFragment(seed int, unlockAll bool) {
	if c.effective.TcpFragment == nil {
		c.effective.TcpFragment = &appctlpb.TCPFragment{}
	}
	f := c.effective.TcpFragment

	if c.origin.TcpFragment == nil || c.origin.TcpFragment.Enable == nil {
		if unlockAll {
			f.Enable = proto.Bool(rng.FixedInt(2, fmt.Sprintf("%d:tcpFragment.enable", seed)) == 1)
		} else {
			f.Enable = proto.Bool(false)
		}
	}

	if c.origin.TcpFragment == nil || c.origin.TcpFragment.MaxSleepMs == nil {
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

	if c.origin.Nonce == nil || c.origin.Nonce.Type == nil {
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

	if c.origin.Nonce == nil || c.origin.Nonce.ApplyToAllUDPPacket == nil {
		if unlockAll {
			n.ApplyToAllUDPPacket = proto.Bool(rng.FixedInt(2, fmt.Sprintf("%d:nonce.applyToAllUDPPacket", seed)) == 1)
		} else {
			n.ApplyToAllUDPPacket = proto.Bool(false)
		}
	}

	if c.origin.Nonce == nil || c.origin.Nonce.MinLen == nil {
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

	if c.origin.Nonce == nil || c.origin.Nonce.MaxLen == nil {
		minLen := int(n.GetMinLen())
		n.MaxLen = proto.Int32(int32(minLen + rng.FixedInt(13-minLen, fmt.Sprintf("%d:nonce.maxLen", seed))))
	}
}
