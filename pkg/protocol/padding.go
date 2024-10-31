// Copyright (C) 2023  mieru authors
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
// along with this program.  If not, see <https://www.gnu.org/licenses/>

package protocol

import (
	crand "crypto/rand"
	"fmt"
	mrand "math/rand"

	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/mathext"
	"github.com/enfein/mieru/v3/pkg/rng"
)

var (
	recommendedConsecutiveASCIILen = 24 + rng.FixedIntPerHost(17)
	recommendedTargetProbability   = 0.325
)

type paddingOpts struct {
	// The maxinum length of padding.
	maxLen int

	ascii *asciiPaddingOpts

	entropy *entropyPaddingOpts
}

type asciiPaddingOpts struct {
	// The mininum length of consecutive ASCII characters.
	// This implies the mininum length of padding.
	minConsecutiveASCIILen int
}

type entropyPaddingOpts struct {
	// The existing data that the padding will be attached to.
	existingData []byte

	// The target probability of bit 0 or bit 1, whichever is lower,
	// in the existing data + padding.
	// This value should be in the range of (0.0, 0.5].
	//
	// We will try best effort to meet the target with the given
	// maximum padding length.
	targetProbability float64
}

// MaxPaddingSize returns the maximum padding size of a segment.
func MaxPaddingSize(mtu int, transport common.TransportProtocol, fragmentSize int, existingPaddingSize int) int {
	if transport == common.StreamTransport {
		// No limit.
		return 255
	}

	res := mtu - fragmentSize - packetOverhead
	if res <= int(existingPaddingSize) {
		return 0
	}
	return mathext.Min(res-int(existingPaddingSize), 255)
}

func buildRecommendedPaddingOpts(maxLen, randomDataLen int, strategySource string) paddingOpts {
	// strategySource decides the padding strategy.
	strategy := rng.FixedInt(2, strategySource)
	if strategy == 0 {
		// Use ASCII.
		return paddingOpts{
			maxLen: maxLen,
			ascii: &asciiPaddingOpts{
				minConsecutiveASCIILen: mathext.Min(maxLen, recommendedConsecutiveASCIILen),
			},
		}
	} else {
		// Use entropy.
		randomData := make([]byte, randomDataLen)
		for {
			if _, err := crand.Read(randomData); err == nil {
				break
			}
		}
		return paddingOpts{
			maxLen: maxLen,
			entropy: &entropyPaddingOpts{
				existingData:      randomData,
				targetProbability: recommendedTargetProbability,
			},
		}
	}
}

func newPadding(opts paddingOpts) []byte {
	if opts.ascii != nil {
		if opts.maxLen < opts.ascii.minConsecutiveASCIILen {
			panic(fmt.Sprintf("Invalid padding options: maxLen %d is smaller than minConsecutiveASCIILen %d", opts.maxLen, opts.ascii.minConsecutiveASCIILen))
		}

		length := rng.Intn(opts.maxLen-opts.ascii.minConsecutiveASCIILen+1) + opts.ascii.minConsecutiveASCIILen
		p := make([]byte, length)
		for {
			if _, err := crand.Read(p); err == nil {
				break
			}
		}
		beginIdx := 0
		if length > opts.ascii.minConsecutiveASCIILen {
			beginIdx = mrand.Intn(length - opts.ascii.minConsecutiveASCIILen)
		}
		common.ToPrintableChar(p, beginIdx, beginIdx+opts.ascii.minConsecutiveASCIILen)
		return p
	} else if opts.entropy != nil {
		if opts.entropy.targetProbability <= 0.0 || opts.entropy.targetProbability > 0.5 {
			panic(fmt.Sprintf("Invalid padding options: targetProbability %f is out of range (0.0, 0.5]", opts.entropy.targetProbability))
		}

		currentBitDistribution := common.ToBitDistribution(opts.entropy.existingData)

		// Determine which bit (0 or 1) has the lower probability.
		lowerBit := byte(0)
		lowerBitCount := currentBitDistribution.Bit0Count
		if currentBitDistribution.Bit1 < currentBitDistribution.Bit0 {
			lowerBit = byte(1)
			lowerBitCount = currentBitDistribution.Bit1Count
		}

		// Let x = lowerBitCount, t = targetProbability, e = existingData bits, p = padding bits.
		// We need to find p that satisfy x = t * (e + p).
		// Solve the equation gives p = x / t - e.
		minPaddingBits := int(float64(lowerBitCount)/opts.entropy.targetProbability - float64(len(opts.entropy.existingData)*8))
		minPaddingBits = mathext.Max(minPaddingBits, 0)
		minPaddingBytes := (minPaddingBits + 7) / 8

		// Determine the padding length.
		var length int
		if minPaddingBytes >= opts.maxLen {
			length = opts.maxLen
		} else {
			length = rng.Intn(opts.maxLen-minPaddingBytes+1) + minPaddingBytes
		}
		p := make([]byte, length)

		// Determine the number of bits that can flip in the padding.
		flip := mathext.Max(int(float64(len(opts.entropy.existingData)+length)*8*opts.entropy.targetProbability)-lowerBitCount, 0)
		flip = mathext.Min(flip, length*8)

		if lowerBit == 0 {
			// Set all the bits in the padding to be 1.
			common.FillBytes(p, 0xFF)

			// Change at most flip bits in p to bit 0.
			for i := 0; i < flip; i++ {
				j := mrand.Intn(len(p) * 8)
				byteIndex := j / 8
				bitIndex := j % 8
				p[byteIndex] &^= (1 << bitIndex)
			}
		} else {
			// Change at most flip bits in p to bit 1.
			for i := 0; i < flip; i++ {
				j := mrand.Intn(len(p) * 8)
				byteIndex := j / 8
				bitIndex := j % 8
				p[byteIndex] |= (1 << bitIndex)
			}
		}
		return p
	}
	panic("Detailed padding options are not provided")
}
