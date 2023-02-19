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

const (
	// Number of bytes used by metadata before encryption.
	MetadataLength = 32
)

const (
	// Protocols.

	OpenSessionRequest     byte = 0
	OpenSessionResponse    byte = 1
	OpenSessionResponseAck byte = 2

	CloseSessionRequest  byte = 4
	CloseSessionResponse byte = 5

	ReliableDataClientToServer byte = 6
	ReliableDataServerToClient byte = 7

	ReliableAckClientToServer byte = 8
	ReliableAckServerToClient byte = 9

	DataClientToServer byte = 10
	DataServerToClient byte = 11

	AckClientToServer byte = 12
	AckServerToClient byte = 13
)

// Metadata defines the methods supported by all metadata.
type Metadata interface {

	// Marshal serializes the metadata to a non-encrypted wire format.
	Marshal() ([]byte, error)

	// Unmarshal constructs the metadata from the non-encrypted wire format.
	Unmarshal([]byte) error
}

var allSupportedProtocolTypes = map[byte]struct{}{
	OpenSessionRequest:         {},
	OpenSessionResponse:        {},
	OpenSessionResponseAck:     {},
	CloseSessionRequest:        {},
	CloseSessionResponse:       {},
	ReliableDataClientToServer: {},
	ReliableDataServerToClient: {},
	ReliableAckClientToServer:  {},
	ReliableAckServerToClient:  {},
	DataClientToServer:         {},
	DataServerToClient:         {},
	AckClientToServer:          {},
	AckServerToClient:          {},
}

// sessionLifecycleStruct is used by open and close sessions.
type sessionLifecycleStruct struct {
	protocol          byte   // byte 0: protocol type
	sessionID         uint32 // byte 1 - 4: session ID number
	originalSessionID uint32 // byte 5 - 8: original session ID number proposed in request
	errorType         uint8  // byte 9: error message type
	paddingLen        uint8  // byte 10: length of padding
}

// dataAckStruct is used by data and ack protocols.
type dataAckStruct struct {
	protocol   byte   // byte 0: protocol type
	sessionID  uint32 // byte 1 - 4: session ID number
	seq        uint32 // byte 5 - 8: sequence number or ack number
	unAckSeq   uint32 // byte 9 - 12: unacknowledged sequence number
	windowSize uint16 // byte 13 - 14: receive window size
	prefixLen  uint8  // byte 15: length of prefix padding
	payloadLen uint16 // byte 16 - 17: length of encapsulated payload, not including auth tag
	suffixLen  uint8  // byte 18: length of suffix padding
}
