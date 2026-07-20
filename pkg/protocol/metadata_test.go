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
	mrand "math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
)

func TestSessionStruct(t *testing.T) {
	s := &sessionStruct{
		baseStruct: baseStruct{
			protocol: uint8(closeSessionRequest),
		},
		sessionID:  mrand.Uint32(),
		statusCode: uint8(mrand.Uint32()),
		seq:        mrand.Uint32(),
		payloadLen: uint16(mrand.Uint32()),
		suffixLen:  uint8(mrand.Uint32()),
	}
	b := s.Marshal()
	s2 := &sessionStruct{}
	if err := s2.Unmarshal(b); err != nil {
		t.Fatalf("Unmarshal() failed: %v", err)
	}
	if !reflect.DeepEqual(s, s2) {
		t.Errorf("Not equal:\n%v\n====\n%v", s, s2)
	}
	if s.String() != s2.String() {
		t.Errorf("Not equal:\n%s\n====\n%s", s.String(), s2.String())
	}
}

func TestDataAckStruct(t *testing.T) {
	s := &dataAckStruct{
		baseStruct: baseStruct{
			protocol: uint8(dataServerToClient),
		},
		sessionID:  mrand.Uint32(),
		seq:        mrand.Uint32(),
		unAckSeq:   mrand.Uint32(),
		windowSize: uint16(mrand.Uint32()),
		fragment:   uint8(mrand.Uint32()),
		prefixLen:  uint8(mrand.Uint32()),
		payloadLen: uint16(mrand.Uint32()),
		suffixLen:  uint8(mrand.Uint32()),
	}
	b := s.Marshal()
	s2 := &dataAckStruct{}
	if err := s2.Unmarshal(b); err != nil {
		t.Fatalf("Unmarshal() failed: %v", err)
	}
	if !reflect.DeepEqual(s, s2) {
		t.Errorf("Not equal:\n%v\n====\n%v", s, s2)
	}
	if s.String() != s2.String() {
		t.Errorf("Not equal:\n%s\n====\n%s", s.String(), s2.String())
	}
	for _, field := range []string{"lowEntropyMode=", "lowEntropyMask=", "extractedPayloadLen=", "lowEntropyMaskRotation="} {
		if strings.Contains(s.String(), field) {
			t.Errorf("String() = %q, want low entropy field %q omitted", s.String(), field)
		}
	}
}

func TestDataAckProtocolClassifiers(t *testing.T) {
	dataProtocols := []protocolType{dataClientToServer, dataServerToClient, dataClientToServerLowEntropy, dataServerToClientLowEntropy}
	for _, protocol := range dataProtocols {
		if !isDataProtocol(protocol) || !isDataAckProtocol(protocol) {
			t.Errorf("data protocol %v is not classified as data", protocol)
		}
	}
	for _, protocol := range []protocolType{dataClientToServerLowEntropy, dataServerToClientLowEntropy} {
		if !isLowEntropyProtocol(protocol) {
			t.Errorf("low entropy protocol %v is not classified as low entropy", protocol)
		}
		if protocol.String() == "UNKNOWN" {
			t.Errorf("low entropy protocol %d has no string form", protocol)
		}
	}
	for _, protocol := range []protocolType{ackClientToServer, ackServerToClient} {
		if !isAckProtocol(protocol) || !isDataAckProtocol(protocol) || isDataProtocol(protocol) {
			t.Errorf("ACK protocol %v has incorrect classification", protocol)
		}
	}
}

func TestDataAckStructAllowsZeroPayload(t *testing.T) {
	tests := []*dataAckStruct{
		{
			baseStruct: baseStruct{protocol: uint8(dataServerToClient)},
		},
		{
			baseStruct:             baseStruct{protocol: uint8(dataServerToClientLowEntropy)},
			lowEntropyMode:         uint8(appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_32),
			lowEntropyMask:         0x0f0f0f0f,
			lowEntropyMaskRotation: uint8(appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_NO_ROTATION),
		},
	}
	for _, test := range tests {
		t.Run(test.Protocol().String(), func(t *testing.T) {
			got := &dataAckStruct{}
			if err := got.Unmarshal(test.Marshal()); err != nil {
				t.Fatalf("Unmarshal() failed: %v", err)
			}
			if !reflect.DeepEqual(test, got) {
				t.Errorf("Not equal:\n%v\n====\n%v", test, got)
			}
		})
	}
}

func TestLowEntropyDataAckStruct(t *testing.T) {
	s := &dataAckStruct{
		baseStruct: baseStruct{
			protocol: uint8(dataClientToServerLowEntropy),
		},
		lowEntropyMode:         uint8(appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_32),
		sessionID:              0x01020304,
		seq:                    0x05060708,
		unAckSeq:               0x090a0b0c,
		windowSize:             0x0d0e,
		fragment:               0x0f,
		prefixLen:              0x10,
		payloadLen:             8,
		suffixLen:              0x11,
		lowEntropyMask:         0x0f0f0f0f,
		extractedPayloadLen:    4,
		lowEntropyMaskRotation: uint8(appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_LEFT_3),
	}
	b := s.Marshal()
	if got := b[0]; got != byte(dataClientToServerLowEntropy) {
		t.Errorf("protocol byte = %d, want %d", got, dataClientToServerLowEntropy)
	}
	if got := b[1]; got != byte(appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_32) {
		t.Errorf("low entropy mode byte = %d, want %d", got, appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_32)
	}
	if got := binary.BigEndian.Uint32(b[25:29]); got != 0x0f0f0f0f {
		t.Errorf("low entropy mask = %08x, want 0f0f0f0f", got)
	}
	if got := binary.BigEndian.Uint16(b[29:31]); got != 4 {
		t.Errorf("extracted payload length = %d, want 4", got)
	}
	if got := b[31]; got != byte(appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_LEFT_3) {
		t.Errorf("low entropy rotation byte = %d, want %d", got, appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_LEFT_3)
	}

	got := &dataAckStruct{}
	if err := got.Unmarshal(b); err != nil {
		t.Fatalf("Unmarshal() failed: %v", err)
	}
	if !reflect.DeepEqual(s, got) {
		t.Errorf("Not equal:\n%v\n====\n%v", s, got)
	}
	for _, field := range []string{"lowEntropyMode=", "lowEntropyMask=", "extractedPayloadLen=", "lowEntropyMaskRotation="} {
		if !strings.Contains(s.String(), field) {
			t.Errorf("String() = %q, want low entropy field %q included", s.String(), field)
		}
	}
}

func TestLowEntropyDataAckStructRejectsInvalidMetadata(t *testing.T) {
	valid := (&dataAckStruct{
		baseStruct:             baseStruct{protocol: uint8(dataServerToClientLowEntropy)},
		lowEntropyMode:         uint8(appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_32),
		payloadLen:             8,
		lowEntropyMask:         0x0f0f0f0f,
		extractedPayloadLen:    4,
		lowEntropyMaskRotation: uint8(appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_NO_ROTATION),
	}).Marshal()

	tests := []struct {
		name   string
		mutate func([]byte)
	}{
		{"mode_off", func(b []byte) { b[1] = 0 }},
		{"mode_out_of_range", func(b []byte) { b[1] = 5 }},
		{"zero_payload_with_nonzero_extracted_payload", func(b []byte) { binary.BigEndian.PutUint16(b[22:24], 0) }},
		{"unaligned_payload", func(b []byte) { binary.BigEndian.PutUint16(b[22:24], 7) }},
		{"inconsistent_payload", func(b []byte) { binary.BigEndian.PutUint16(b[22:24], 16) }},
		{"zero_extracted_payload_with_nonzero_payload", func(b []byte) { binary.BigEndian.PutUint16(b[29:31], 0) }},
		{"oversized_extracted_payload", func(b []byte) { binary.BigEndian.PutUint16(b[29:31], maxPDU+1) }},
		{"wrong_mask_popcount", func(b []byte) { binary.BigEndian.PutUint32(b[25:29], 0x0f0f0f0e) }},
		{"invalid_rotation", func(b []byte) { b[31] = 17 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			b := append([]byte(nil), valid...)
			test.mutate(b)
			if err := (&dataAckStruct{}).Unmarshal(b); err == nil {
				t.Fatal("Unmarshal() succeeded, want error")
			}
		})
	}
}
