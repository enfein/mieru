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
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package protocol

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/mathext"
)

type protocolType byte

const (
	closeConnRequest             protocolType = 0
	closeConnResponse            protocolType = 1
	openSessionRequest           protocolType = 2
	openSessionResponse          protocolType = 3
	closeSessionRequest          protocolType = 4
	closeSessionResponse         protocolType = 5
	dataClientToServer           protocolType = 6
	dataServerToClient           protocolType = 7
	ackClientToServer            protocolType = 8
	ackServerToClient            protocolType = 9
	dataClientToServerLowEntropy protocolType = 10
	dataServerToClientLowEntropy protocolType = 11
)

func (p protocolType) Equals(other byte) bool {
	return byte(p) == other
}

func (p protocolType) String() string {
	switch p {
	case closeConnRequest:
		return "closeConnRequest"
	case closeConnResponse:
		return "closeConnResponse"
	case openSessionRequest:
		return "openSessionRequest"
	case openSessionResponse:
		return "openSessionResponse"
	case closeSessionRequest:
		return "closeSessionRequest"
	case closeSessionResponse:
		return "closeSessionResponse"
	case dataClientToServer:
		return "dataClientToServer"
	case dataServerToClient:
		return "dataServerToClient"
	case ackClientToServer:
		return "ackClientToServer"
	case ackServerToClient:
		return "ackServerToClient"
	case dataClientToServerLowEntropy:
		return "dataClientToServerLowEntropy"
	case dataServerToClientLowEntropy:
		return "dataServerToClientLowEntropy"
	default:
		return "UNKNOWN"
	}
}

type statusCode byte

const (
	statusOK             statusCode = 0
	statusQuotaExhausted statusCode = 1
)

func (c statusCode) String() string {
	switch c {
	case statusOK:
		return "OK"
	case statusQuotaExhausted:
		return "quotaExhausted"
	default:
		return "UNKNOWN"
	}
}

const (
	// Number of bytes used by metadata before encryption.
	MetadataLength = 32

	// Maximum payload that cat be attached to open session request and open session response.
	MaxSessionOpenPayload = 1024
)

// metadata defines the methods supported by all metadata.
type metadata interface {

	// Protocol returns the protocol of metadata.
	Protocol() protocolType

	// Marshal serializes the metadata to a non-encrypted wire format.
	Marshal() []byte

	// Unmarshal constructs the metadata from the non-encrypted wire format.
	Unmarshal([]byte) error

	// String returns a human readable representation of the metadata.
	String() string
}

var (
	_ metadata = &sessionStruct{}
	_ metadata = &dataAckStruct{}
)

// baseStruct is shared by all metadata struct.
type baseStruct struct {
	protocol  uint8  // byte 0: protocol type
	_         uint8  // byte 1: type-specific; low entropy mode for low entropy data, otherwise reserved
	timestamp uint32 // byte 2 - 5: timestamp, number of minutes after UNIX epoch
}

// sessionStruct is used to open or close a session.
type sessionStruct struct {
	baseStruct
	sessionID  uint32 // byte 6 - 9: session ID number
	seq        uint32 // byte 10 - 13: sequence number
	statusCode uint8  // byte 14: status of opening or closing session
	payloadLen uint16 // byte 15 - 16: length of encapsulated payload, not including auth tag
	suffixLen  uint8  // byte 17: length of suffix padding
}

func (ss *sessionStruct) Protocol() protocolType {
	return protocolType(ss.baseStruct.protocol)
}

func (ss *sessionStruct) Marshal() []byte {
	b := make([]byte, MetadataLength)
	b[0] = ss.baseStruct.protocol
	ss.baseStruct.timestamp = uint32(time.Now().Unix() / 60)
	binary.BigEndian.PutUint32(b[2:], ss.baseStruct.timestamp)
	binary.BigEndian.PutUint32(b[6:], ss.sessionID)
	binary.BigEndian.PutUint32(b[10:], ss.seq)
	b[14] = ss.statusCode
	binary.BigEndian.PutUint16(b[15:], ss.payloadLen)
	b[17] = ss.suffixLen
	return b
}

func (ss *sessionStruct) Unmarshal(b []byte) error {
	// Check errors.
	if len(b) != MetadataLength {
		return fmt.Errorf("input bytes: %d, want %d", len(b), MetadataLength)
	}
	if !openSessionRequest.Equals(b[0]) && !openSessionResponse.Equals(b[0]) && !closeSessionRequest.Equals(b[0]) && !closeSessionResponse.Equals(b[0]) {
		return fmt.Errorf("invalid protocol %d", b[0])
	}
	originalTimestamp := binary.BigEndian.Uint32(b[2:])
	currentTimestamp := uint32(time.Now().Unix() / 60)
	if !mathext.WithinRange(currentTimestamp, originalTimestamp, 1) {
		return fmt.Errorf("invalid timestamp %d", originalTimestamp*60)
	}
	if ss.payloadLen > MaxSessionOpenPayload {
		return fmt.Errorf("payload size %d exceed maximum value %d", ss.payloadLen, MaxSessionOpenPayload)
	}

	// Do unmarshal.
	ss.baseStruct.protocol = b[0]
	ss.baseStruct.timestamp = originalTimestamp
	ss.sessionID = binary.BigEndian.Uint32(b[6:])
	ss.seq = binary.BigEndian.Uint32(b[10:])
	ss.statusCode = b[14]
	ss.payloadLen = binary.BigEndian.Uint16(b[15:])
	ss.suffixLen = b[17]
	return nil
}

func (ss *sessionStruct) String() string {
	return fmt.Sprintf("sessionStruct{protocol=%v, sessionID=%v, seq=%v, statusCode=%v, payloadLen=%v, suffixLen=%v}", protocolType(ss.protocol), ss.sessionID, ss.seq, ss.statusCode, ss.payloadLen, ss.suffixLen)
}

func isSessionProtocol(p protocolType) bool {
	return p == openSessionRequest || p == openSessionResponse || p == closeSessionRequest || p == closeSessionResponse
}

func toSessionStruct(m metadata) (*sessionStruct, bool) {
	if isSessionProtocol(m.Protocol()) {
		return m.(*sessionStruct), true
	}
	return nil, false
}

// dataAckStruct is used by data and ack protocols.
type dataAckStruct struct {
	baseStruct
	lowEntropyMode         uint8  // byte 1 for low entropy data: low entropy mode
	sessionID              uint32 // byte 6 - 9: session ID number
	seq                    uint32 // byte 10 - 13: sequence number or ack number
	unAckSeq               uint32 // byte 14 - 17: unacknowledged sequence number; sequences smaller than this are acknowledged
	windowSize             uint16 // byte 18 - 19: receive window size, in number of segments
	fragment               uint8  // byte 20: fragment number; this number reduces over time, 0 is the last fragment
	prefixLen              uint8  // byte 21: length of prefix padding
	payloadLen             uint16 // byte 22 - 23: length of encapsulated payload, not including auth tag
	suffixLen              uint8  // byte 24: length of suffix padding
	lowEntropyMask         uint32 // byte 25 - 28: initial 32-bit half-mask
	extractedPayloadLen    uint16 // byte 29 - 30: payload length after low entropy padding is removed
	lowEntropyMaskRotation uint8  // byte 31: low entropy mask rotation
}

func (das *dataAckStruct) Protocol() protocolType {
	return protocolType(das.baseStruct.protocol)
}

func (das *dataAckStruct) Marshal() []byte {
	b := make([]byte, MetadataLength)
	b[0] = das.baseStruct.protocol
	if isLowEntropyProtocol(das.Protocol()) {
		b[1] = das.lowEntropyMode
	}
	das.baseStruct.timestamp = uint32(time.Now().Unix() / 60)
	binary.BigEndian.PutUint32(b[2:], das.baseStruct.timestamp)
	binary.BigEndian.PutUint32(b[6:], das.sessionID)
	binary.BigEndian.PutUint32(b[10:], das.seq)
	binary.BigEndian.PutUint32(b[14:], das.unAckSeq)
	binary.BigEndian.PutUint16(b[18:], das.windowSize)
	b[20] = das.fragment
	b[21] = das.prefixLen
	binary.BigEndian.PutUint16(b[22:], das.payloadLen)
	b[24] = das.suffixLen
	if isLowEntropyProtocol(das.Protocol()) {
		binary.BigEndian.PutUint32(b[25:], das.lowEntropyMask)
		binary.BigEndian.PutUint16(b[29:], das.extractedPayloadLen)
		b[31] = das.lowEntropyMaskRotation
	}
	return b
}

func (das *dataAckStruct) Unmarshal(b []byte) error {
	// Check errors.
	if len(b) != MetadataLength {
		return fmt.Errorf("input bytes: %d, want %d", len(b), MetadataLength)
	}
	protocol := protocolType(b[0])
	if !isDataAckProtocol(protocol) {
		return fmt.Errorf("invalid protocol %d", b[0])
	}
	originalTimestamp := binary.BigEndian.Uint32(b[2:])
	currentTimestamp := uint32(time.Now().Unix() / 60)
	if !mathext.WithinRange(currentTimestamp, originalTimestamp, 1) {
		return fmt.Errorf("invalid timestamp %d", originalTimestamp*60)
	}

	var lowEntropyMode uint8
	var lowEntropyMask uint32
	var extractedPayloadLen uint16
	var lowEntropyMaskRotation uint8
	if isLowEntropyProtocol(protocol) {
		lowEntropyMode = b[1]
		lowEntropyMask = binary.BigEndian.Uint32(b[25:])
		extractedPayloadLen = binary.BigEndian.Uint16(b[29:])
		lowEntropyMaskRotation = b[31]
		candidate := &dataAckStruct{
			baseStruct:             baseStruct{protocol: b[0]},
			lowEntropyMode:         lowEntropyMode,
			payloadLen:             binary.BigEndian.Uint16(b[22:]),
			lowEntropyMask:         lowEntropyMask,
			extractedPayloadLen:    extractedPayloadLen,
			lowEntropyMaskRotation: lowEntropyMaskRotation,
		}
		if err := validateLowEntropyDataAckMetadata(candidate); err != nil {
			return err
		}
	}

	// Do unmarshal.
	das.baseStruct.protocol = uint8(protocol)
	das.baseStruct.timestamp = originalTimestamp
	das.lowEntropyMode = lowEntropyMode
	das.sessionID = binary.BigEndian.Uint32(b[6:])
	das.seq = binary.BigEndian.Uint32(b[10:])
	das.unAckSeq = binary.BigEndian.Uint32(b[14:])
	das.windowSize = binary.BigEndian.Uint16(b[18:])
	das.fragment = b[20]
	das.prefixLen = b[21]
	das.payloadLen = binary.BigEndian.Uint16(b[22:])
	das.suffixLen = b[24]
	das.lowEntropyMask = lowEntropyMask
	das.extractedPayloadLen = extractedPayloadLen
	das.lowEntropyMaskRotation = lowEntropyMaskRotation
	return nil
}

func (das *dataAckStruct) String() string {
	result := fmt.Sprintf("dataAckStruct{protocol=%v, sessionID=%v, seq=%v, unAckSeq=%v, windowSize=%v, fragment=%v, prefixLen=%v, payloadLen=%v, suffixLen=%v", protocolType(das.protocol), das.sessionID, das.seq, das.unAckSeq, das.windowSize, das.fragment, das.prefixLen, das.payloadLen, das.suffixLen)
	if das.lowEntropyMode == uint8(appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_OFF) {
		return result + "}"
	}
	return fmt.Sprintf("%s, lowEntropyMode=%v, lowEntropyMask=%08x, extractedPayloadLen=%v, lowEntropyMaskRotation=%v}", result, appctlpb.LowEntropyMode(das.lowEntropyMode), das.lowEntropyMask, das.extractedPayloadLen, appctlpb.LowEntropyMaskRotation(das.lowEntropyMaskRotation))
}

func isLowEntropyProtocol(p protocolType) bool {
	return p == dataClientToServerLowEntropy || p == dataServerToClientLowEntropy
}

func isDataProtocol(p protocolType) bool {
	return p == dataClientToServer || p == dataServerToClient || isLowEntropyProtocol(p)
}

func isAckProtocol(p protocolType) bool {
	return p == ackClientToServer || p == ackServerToClient
}

func isDataAckProtocol(p protocolType) bool {
	return isDataProtocol(p) || isAckProtocol(p)
}

func toDataAckStruct(m metadata) (*dataAckStruct, bool) {
	if isDataAckProtocol(m.Protocol()) {
		return m.(*dataAckStruct), true
	}
	return nil, false
}

func validateLowEntropyDataAckMetadata(das *dataAckStruct) error {
	if !isLowEntropyProtocol(das.Protocol()) {
		return fmt.Errorf("protocol %d is not low entropy data", das.Protocol())
	}
	if int(das.extractedPayloadLen) > maxPDU {
		return fmt.Errorf("invalid extracted payload length %d", das.extractedPayloadLen)
	}
	if das.payloadLen%lowEntropyChunkLen != 0 {
		return fmt.Errorf("invalid low entropy payload length %d", das.payloadLen)
	}
	if _, err := validateLowEntropyCodecParams(appctlpb.LowEntropyMode(das.lowEntropyMode), das.lowEntropyMask, appctlpb.LowEntropyMaskRotation(das.lowEntropyMaskRotation)); err != nil {
		return err
	}
	if das.extractedPayloadLen == 0 {
		if das.payloadLen != 0 {
			return fmt.Errorf("low entropy payload length is %d, want 0", das.payloadLen)
		}
		return nil
	}
	expectedPayloadLen, err := lowEntropyEncodedPayloadLen(int(das.extractedPayloadLen), appctlpb.LowEntropyMode(das.lowEntropyMode))
	if err != nil {
		return err
	}
	if das.payloadLen != expectedPayloadLen {
		return fmt.Errorf("low entropy payload length is %d, want %d", das.payloadLen, expectedPayloadLen)
	}
	return nil
}
