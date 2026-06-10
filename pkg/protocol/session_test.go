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
	"testing"

	"github.com/enfein/mieru/v3/pkg/common"
)

func TestSessionReceiveWindowSize(t *testing.T) {
	s := NewSession(1, false, 1400, nil, nil)
	if got := s.recvQueue.Remaining(); got != segmentTreeCapacity {
		t.Fatalf("recvQueue capacity = %d, want %d", got, segmentTreeCapacity)
	}
	if got := s.receiveWindowSize(); got != segmentTreeCapacity {
		t.Fatalf("receiveWindowSize() = %d, want %d", got, segmentTreeCapacity)
	}

	if !s.recvBuf.Insert(testDataSegment(1, []byte("a"), common.PacketTransport)) {
		t.Fatalf("insert segment to recvBuf failed")
	}
	if got := s.receiveWindowSize(); got != segmentTreeCapacity-1 {
		t.Fatalf("receiveWindowSize() with recvBuf = %d, want %d", got, segmentTreeCapacity-1)
	}

	if !s.recvQueue.Insert(testDataSegment(2, []byte("b"), common.PacketTransport)) {
		t.Fatalf("insert segment to recvQueue failed")
	}
	if got := s.receiveWindowSize(); got != segmentTreeCapacity-2 {
		t.Fatalf("receiveWindowSize() with recvBuf and recvQueue = %d, want %d", got, segmentTreeCapacity-2)
	}
}

func testDataSegment(seq uint32, payload []byte, transport common.TransportProtocol) *segment {
	return &segment{
		metadata: &dataAckStruct{
			baseStruct: baseStruct{
				protocol: uint8(dataClientToServer),
			},
			sessionID:  1,
			seq:        seq,
			windowSize: uint16(segmentTreeCapacity),
			payloadLen: uint16(len(payload)),
		},
		payload:   append([]byte(nil), payload...),
		transport: transport,
	}
}
