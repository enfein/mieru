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

package protocolv2

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/enfein/mieru/pkg/mathext"
)

const (

	// Number of bytes used by metadata before encryption.
	metadataLength = 32

	// Protocols.
	closeConnRequest     byte = 0
	closeConnResponse    byte = 1
	openSessionRequest   byte = 2
	openSessionResponse  byte = 3
	closeSessionRequest  byte = 4
	closeSessionResponse byte = 5
	dataClientToServer   byte = 6
	dataServerToClient   byte = 7
	ackClientToServer    byte = 8
	ackServerToClient    byte = 9
)

// Metadata defines the methods supported by all metadata.
type Metadata interface {

	// Protocol returns the protocol of metadata.
	Protocol() byte

	// Marshal serializes the metadata to a non-encrypted wire format.
	Marshal() ([]byte, error)

	// Unmarshal constructs the metadata from the non-encrypted wire format.
	Unmarshal([]byte) error
}

var (
	_ Metadata = &sessionStruct{}
	_ Metadata = &dataAckStruct{}
)

// BaseStruct is shared by all metadata struct.
type BaseStruct struct {
	Protocol  uint8  // byte 0: protocol type
	Epoch     uint8  // byte 1: epoch, it changes when the underlay address is changed
	Timestamp uint32 // byte 2 - 5: timestamp, number of minutes after UNIX epoch
}

// sessionStruct is used to open or close a session.
type sessionStruct struct {
	BaseStruct
	RequestID  uint32 // byte 6 - 9: request ID number
	SessionID  uint32 // byte 10 - 13: session ID number
	StatusCode uint8  // byte 14: reason of closing a session
	PaddingLen uint8  // byte 15: length of padding
	// TODO: allow carry extra data to complete socks5 handshake.
}

func (ss *sessionStruct) Protocol() byte {
	return ss.BaseStruct.Protocol
}

func (ss *sessionStruct) Marshal() ([]byte, error) {
	b := make([]byte, metadataLength)
	b[0] = ss.BaseStruct.Protocol
	b[1] = ss.BaseStruct.Epoch
	ss.BaseStruct.Timestamp = uint32(time.Now().Unix() / 60)
	binary.BigEndian.PutUint32(b[2:], ss.BaseStruct.Timestamp)
	binary.BigEndian.PutUint32(b[6:], ss.RequestID)
	binary.BigEndian.PutUint32(b[10:], ss.SessionID)
	b[14] = ss.StatusCode
	b[15] = ss.PaddingLen
	return b, nil
}

func (ss *sessionStruct) Unmarshal(b []byte) error {
	// Check errors.
	if len(b) != metadataLength {
		return fmt.Errorf("input bytes: %d, want %d", len(b), metadataLength)
	}
	if b[0] != openSessionRequest && b[0] != openSessionResponse && b[0] != closeSessionRequest && b[0] != closeSessionResponse {
		return fmt.Errorf("invalid protocol %d", b[0])
	}
	originalTimestamp := binary.BigEndian.Uint32(b[2:])
	currentTimestamp := uint32(time.Now().Unix() / 60)
	if !mathext.WithinRange(currentTimestamp, originalTimestamp, 1) {
		return fmt.Errorf("invalid timestamp %d", originalTimestamp*60)
	}

	// Do unmarshal.
	ss.BaseStruct.Protocol = b[0]
	ss.BaseStruct.Epoch = b[1]
	ss.BaseStruct.Timestamp = originalTimestamp
	ss.RequestID = binary.BigEndian.Uint32(b[6:])
	ss.SessionID = binary.BigEndian.Uint32(b[10:])
	ss.StatusCode = b[14]
	ss.PaddingLen = b[15]
	return nil
}

func isSessionProtocol(p byte) bool {
	return p == openSessionRequest || p == openSessionResponse || p == closeSessionRequest || p == closeSessionResponse
}

func toSessionStruct(m Metadata) (*sessionStruct, bool) {
	if isSessionProtocol(m.Protocol()) {
		return m.(*sessionStruct), true
	}
	return nil, false
}

// dataAckStruct is used by data and ack protocols.
type dataAckStruct struct {
	BaseStruct
	SessionID  uint32 // byte 6 - 9: session ID number
	Seq        uint32 // byte 10 - 13: sequence number or ack number
	UnAckSeq   uint32 // byte 14 - 17: unacknowledged sequence number -- sequences smaller than this are acknowledged
	WindowSize uint16 // byte 18 - 19: receive window size, in number of segments
	Fragment   uint8  // byte 20: fragment number, 0 is the last fragment
	PrefixLen  uint8  // byte 21: length of prefix padding
	PayloadLen uint16 // byte 22 - 23: length of encapsulated payload, not including auth tag
	SuffixLen  uint8  // byte 24: length of suffix padding
}

func (das *dataAckStruct) Protocol() byte {
	return das.BaseStruct.Protocol
}

func (das *dataAckStruct) Marshal() ([]byte, error) {
	b := make([]byte, metadataLength)
	b[0] = das.BaseStruct.Protocol
	b[1] = das.BaseStruct.Epoch
	das.BaseStruct.Timestamp = uint32(time.Now().Unix() / 60)
	binary.BigEndian.PutUint32(b[2:], das.BaseStruct.Timestamp)
	binary.BigEndian.PutUint32(b[6:], das.SessionID)
	binary.BigEndian.PutUint32(b[10:], das.Seq)
	binary.BigEndian.PutUint32(b[14:], das.UnAckSeq)
	binary.BigEndian.PutUint16(b[18:], das.WindowSize)
	b[20] = das.Fragment
	b[21] = das.PrefixLen
	binary.BigEndian.PutUint16(b[22:], das.PayloadLen)
	b[24] = das.SuffixLen
	return b, nil
}

func (das *dataAckStruct) Unmarshal(b []byte) error {
	// Check errors.
	if len(b) != metadataLength {
		return fmt.Errorf("input bytes: %d, want %d", len(b), metadataLength)
	}
	if b[0] != dataClientToServer && b[0] != dataServerToClient && b[0] != ackClientToServer && b[0] != ackServerToClient {
		return fmt.Errorf("invalid protocol %d", b[0])
	}
	originalTimestamp := binary.BigEndian.Uint32(b[2:])
	currentTimestamp := uint32(time.Now().Unix() / 60)
	if !mathext.WithinRange(currentTimestamp, originalTimestamp, 1) {
		return fmt.Errorf("invalid timestamp %d", originalTimestamp*60)
	}

	// Do unmarshal.
	das.BaseStruct.Protocol = b[0]
	das.BaseStruct.Epoch = b[1]
	das.BaseStruct.Timestamp = originalTimestamp
	das.SessionID = binary.BigEndian.Uint32(b[6:])
	das.Seq = binary.BigEndian.Uint32(b[10:])
	das.UnAckSeq = binary.BigEndian.Uint32(b[14:])
	das.WindowSize = binary.BigEndian.Uint16(b[18:])
	das.Fragment = b[20]
	das.PrefixLen = b[21]
	das.PayloadLen = binary.BigEndian.Uint16(b[22:])
	das.SuffixLen = b[24]
	return nil
}

func isDataAckProtocol(p byte) bool {
	return p == dataClientToServer || p == dataServerToClient || p == ackClientToServer || p == ackServerToClient
}

func toDataAckStruct(m Metadata) (*dataAckStruct, bool) {
	if isDataAckProtocol(m.Protocol()) {
		return m.(*dataAckStruct), true
	}
	return nil, false
}
