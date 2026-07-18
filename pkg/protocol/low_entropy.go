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

package protocol

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/bits"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/mathext"
	"github.com/enfein/mieru/v3/pkg/rng"
)

// lowEntropyChunkLen is the number of bytes of each low entropy chunk.
const lowEntropyChunkLen = 8

// lowEntropyPaddingBit is stable on a host for a given mieru version. It is
// camouflage rather than a secret.
var lowEntropyPaddingBit = uint8(rng.FixedIntVH(2))

type lowEntropyModeParams struct {
	sourceBytesPerChunk int
	halfMaskOnes        int
}

func buildLowEntropyParams(mode appctlpb.LowEntropyMode) (lowEntropyModeParams, error) {
	switch mode {
	case appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_32:
		return lowEntropyModeParams{sourceBytesPerChunk: 4, halfMaskOnes: 16}, nil
	case appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_40:
		return lowEntropyModeParams{sourceBytesPerChunk: 5, halfMaskOnes: 20}, nil
	case appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_48:
		return lowEntropyModeParams{sourceBytesPerChunk: 6, halfMaskOnes: 24}, nil
	case appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_56:
		return lowEntropyModeParams{sourceBytesPerChunk: 7, halfMaskOnes: 28}, nil
	default:
		return lowEntropyModeParams{}, fmt.Errorf("invalid low entropy mode %d", mode)
	}
}

// lowEntropyEncodedPayloadLen returns the encoded body length after checking
// that it is representable by the uint16 metadata field.
func lowEntropyEncodedPayloadLen(extractedPayloadLen int, mode appctlpb.LowEntropyMode) (uint16, error) {
	params, err := buildLowEntropyParams(mode)
	if err != nil {
		return 0, err
	}
	if extractedPayloadLen <= 0 {
		return 0, fmt.Errorf("invalid extracted payload length %d", extractedPayloadLen)
	}

	chunkCount := extractedPayloadLen / params.sourceBytesPerChunk
	if extractedPayloadLen%params.sourceBytesPerChunk != 0 {
		chunkCount++
	}
	if chunkCount > math.MaxUint16/lowEntropyChunkLen {
		return 0, fmt.Errorf("encoded payload length for %d bytes exceeds %d", extractedPayloadLen, uint16(math.MaxUint16))
	}
	return uint16(chunkCount * lowEntropyChunkLen), nil
}

func isValidLowEntropyRotation(rotation appctlpb.LowEntropyMaskRotation) bool {
	return rotation == appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_NO_ROTATION ||
		(rotation >= appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_RIGHT_1 &&
			rotation <= appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_RIGHT_15) ||
		(rotation >= appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_LEFT_1 &&
			rotation <= appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_LEFT_15 &&
			rotation%16 == 0)
}

// lowEntropyChunkMask returns the mask for chunkIndex. The first chunk is
// unrotated. Right-rotation values occupy the low nibble; left-rotation values
// occupy the high nibble and encode the shift after division by 16.
func lowEntropyChunkMask(initialMask uint64, rotation appctlpb.LowEntropyMaskRotation, chunkIndex int) (uint64, error) {
	if !isValidLowEntropyRotation(rotation) {
		return 0, fmt.Errorf("invalid low entropy mask rotation %d", rotation)
	}
	if chunkIndex < 0 {
		return 0, fmt.Errorf("invalid low entropy chunk index %d", chunkIndex)
	}
	return rotateLowEntropyMask(initialMask, rotation, chunkIndex), nil
}

func rotateLowEntropyMask(initialMask uint64, rotation appctlpb.LowEntropyMaskRotation, chunkIndex int) uint64 {
	if rotation == appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_NO_ROTATION || chunkIndex == 0 {
		return initialMask
	}
	if rotation <= appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_RIGHT_15 {
		return bits.RotateLeft64(initialMask, -((chunkIndex % 64) * int(rotation)))
	}
	return bits.RotateLeft64(initialMask, (chunkIndex%64)*int(rotation/16))
}

func newLowEntropyHalfMask(mode appctlpb.LowEntropyMode) (uint32, error) {
	params, err := buildLowEntropyParams(mode)
	if err != nil {
		return 0, err
	}
	return rng.Uint32WithBits(params.halfMaskOnes), nil
}

// encodeLowEntropyPayload is the production codec wrapper. Tests use
// encodeLowEntropyPayloadWithPaddingBit directly to cover both polarities.
func encodeLowEntropyPayload(src []byte, mode appctlpb.LowEntropyMode, halfMask uint32, rotation appctlpb.LowEntropyMaskRotation) ([]byte, error) {
	return encodeLowEntropyPayloadWithPaddingBit(src, mode, halfMask, rotation, lowEntropyPaddingBit)
}

func encodeLowEntropyPayloadWithPaddingBit(src []byte, mode appctlpb.LowEntropyMode, halfMask uint32, rotation appctlpb.LowEntropyMaskRotation, paddingBit uint8) ([]byte, error) {
	params, err := validateLowEntropyCodecParams(mode, halfMask, rotation)
	if err != nil {
		return nil, err
	}
	if paddingBit > 1 {
		return nil, fmt.Errorf("invalid low entropy padding bit %d", paddingBit)
	}
	encodedLen, err := lowEntropyEncodedPayloadLen(len(src), mode)
	if err != nil {
		return nil, err
	}

	encoded := make([]byte, int(encodedLen))
	initialMask := mathext.RepeatUint32(halfMask)
	for chunkIndex, srcOffset := 0, 0; srcOffset < len(src); chunkIndex, srcOffset = chunkIndex+1, srcOffset+params.sourceBytesPerChunk {
		sourceLen := params.sourceBytesPerChunk
		if remaining := len(src) - srcOffset; remaining < sourceLen {
			sourceLen = remaining
		}

		var scratch [lowEntropyChunkLen]byte
		copy(scratch[lowEntropyChunkLen-sourceLen:], src[srcOffset:srcOffset+sourceLen])
		source := binary.BigEndian.Uint64(scratch[:])
		chunkMask := rotateLowEntropyMask(initialMask, rotation, chunkIndex)
		dataMask := mathext.PDEP(lowBits(sourceLen*8), chunkMask)
		chunk := mathext.PDEP(source, chunkMask)
		if paddingBit == 1 {
			chunk |= ^dataMask
		}
		binary.BigEndian.PutUint64(encoded[chunkIndex*lowEntropyChunkLen:], chunk)
	}
	return encoded, nil
}

// decodeLowEntropyPayload validates canonical uniform padding while extracting
// the original ciphertext body. The padding polarity is inferred from the first
// chunk and must remain the same in every non-data position.
func decodeLowEntropyPayload(encoded []byte, extractedPayloadLen int, mode appctlpb.LowEntropyMode, halfMask uint32, rotation appctlpb.LowEntropyMaskRotation) ([]byte, error) {
	params, err := validateLowEntropyCodecParams(mode, halfMask, rotation)
	if err != nil {
		return nil, err
	}
	expectedEncodedLen, err := lowEntropyEncodedPayloadLen(extractedPayloadLen, mode)
	if err != nil {
		return nil, err
	}
	if len(encoded) != int(expectedEncodedLen) {
		return nil, fmt.Errorf("encoded payload length is %d, want %d", len(encoded), expectedEncodedLen)
	}

	decoded := make([]byte, extractedPayloadLen)
	initialMask := mathext.RepeatUint32(halfMask)
	var paddingBit uint8
	for chunkIndex, dstOffset := 0, 0; dstOffset < extractedPayloadLen; chunkIndex, dstOffset = chunkIndex+1, dstOffset+params.sourceBytesPerChunk {
		sourceLen := params.sourceBytesPerChunk
		if remaining := extractedPayloadLen - dstOffset; remaining < sourceLen {
			sourceLen = remaining
		}

		chunk := binary.BigEndian.Uint64(encoded[chunkIndex*lowEntropyChunkLen:])
		chunkMask := rotateLowEntropyMask(initialMask, rotation, chunkIndex)
		dataMask := mathext.PDEP(lowBits(sourceLen*8), chunkMask)
		paddingMask := ^dataMask
		padding := chunk & paddingMask
		if chunkIndex == 0 {
			switch padding {
			case 0:
				paddingBit = 0
			case paddingMask:
				paddingBit = 1
			default:
				return nil, fmt.Errorf("low entropy chunk %d has mixed padding bits", chunkIndex)
			}
		} else if (paddingBit == 0 && padding != 0) || (paddingBit == 1 && padding != paddingMask) {
			return nil, fmt.Errorf("low entropy chunk %d has non-uniform padding bits", chunkIndex)
		}

		source := mathext.PEXT(chunk, chunkMask)
		var scratch [lowEntropyChunkLen]byte
		binary.BigEndian.PutUint64(scratch[:], source)
		copy(decoded[dstOffset:dstOffset+sourceLen], scratch[lowEntropyChunkLen-sourceLen:])
	}
	return decoded, nil
}

func validateLowEntropyCodecParams(mode appctlpb.LowEntropyMode, halfMask uint32, rotation appctlpb.LowEntropyMaskRotation) (lowEntropyModeParams, error) {
	params, err := buildLowEntropyParams(mode)
	if err != nil {
		return lowEntropyModeParams{}, err
	}
	if bits.OnesCount32(halfMask) != params.halfMaskOnes {
		return lowEntropyModeParams{}, fmt.Errorf("low entropy mask has %d one-bits, want %d", bits.OnesCount32(halfMask), params.halfMaskOnes)
	}
	if !isValidLowEntropyRotation(rotation) {
		return lowEntropyModeParams{}, fmt.Errorf("invalid low entropy mask rotation %d", rotation)
	}
	return params, nil
}

func lowBits(n int) uint64 {
	if n >= 64 {
		return math.MaxUint64
	}
	return uint64(1)<<n - 1
}
