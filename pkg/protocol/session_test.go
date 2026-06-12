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
	"time"

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

	if !s.recvBuf.Insert(testDataSegment(1, 1, []byte("a"), common.PacketTransport)) {
		t.Fatalf("insert segment to recvBuf failed")
	}
	if got := s.receiveWindowSize(); got != segmentTreeCapacity-1 {
		t.Fatalf("receiveWindowSize() with recvBuf = %d, want %d", got, segmentTreeCapacity-1)
	}

	if !s.recvQueue.Insert(testDataSegment(1, 2, []byte("b"), common.PacketTransport)) {
		t.Fatalf("insert segment to recvQueue failed")
	}
	if got := s.receiveWindowSize(); got != segmentTreeCapacity-2 {
		t.Fatalf("receiveWindowSize() with recvBuf and recvQueue = %d, want %d", got, segmentTreeCapacity-2)
	}
}

func TestDeliverSegmentToSessionUnblocksWhenSessionClosed(t *testing.T) {
	underlay := newBaseUnderlay(false, 1400, nil)
	session := NewSession(1, false, 1400, nil, nil)
	seg := testDataSegment(1, 1, []byte("a"), common.StreamTransport)

	for i := 0; i < segmentChanCapacity; i++ {
		session.recvChan <- seg
	}

	done := make(chan bool)
	go func() {
		done <- underlay.deliverSegmentToSession(session, seg)
	}()

	select {
	case delivered := <-done:
		t.Fatalf("deliverSegmentToSession() returned before close: %v", delivered)
	case <-time.After(10 * time.Millisecond):
	}

	if err := session.Close(); err != nil {
		t.Fatalf("session.Close() failed: %v", err)
	}

	select {
	case delivered := <-done:
		if delivered {
			t.Fatalf("deliverSegmentToSession() = true, want false")
		}
	case <-time.After(time.Second):
		t.Fatalf("deliverSegmentToSession() didn't return after session close")
	}
}

func TestStreamUnderlayIgnoresControlSegmentsForClosedClientSession(t *testing.T) {
	underlay := &StreamUnderlay{
		baseUnderlay: *newBaseUnderlay(true, 1400, nil),
	}
	session := NewSession(1, true, 1400, nil, nil)
	if err := underlay.baseUnderlay.AddSession(session, nil); err != nil {
		t.Fatalf("AddSession() failed: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("session.Close() failed: %v", err)
	}

	if err := underlay.onOpenSessionResponse(testSessionSegment(openSessionResponse, 1, common.StreamTransport)); err != nil {
		t.Fatalf("onOpenSessionResponse() failed: %v", err)
	}
	if err := underlay.onCloseSession(testSessionSegment(closeSessionResponse, 1, common.StreamTransport)); err != nil {
		t.Fatalf("onCloseSession() failed: %v", err)
	}
}

func TestPacketUnderlayIgnoresControlSegmentsForClosedClientSession(t *testing.T) {
	underlay := &PacketUnderlay{
		baseUnderlay: *newBaseUnderlay(true, 1400, nil),
	}
	session := NewSession(1, true, 1400, nil, nil)
	if err := underlay.baseUnderlay.AddSession(session, nil); err != nil {
		t.Fatalf("AddSession() failed: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("session.Close() failed: %v", err)
	}

	if err := underlay.onOpenSessionResponse(testSessionSegment(openSessionResponse, 1, common.PacketTransport)); err != nil {
		t.Fatalf("onOpenSessionResponse() failed: %v", err)
	}
	if err := underlay.onCloseSession(testSessionSegment(closeSessionResponse, 1, common.PacketTransport)); err != nil {
		t.Fatalf("onCloseSession() failed: %v", err)
	}
}

func testDataSegment(sessionID, seq uint32, payload []byte, transport common.TransportProtocol) *segment {
	return &segment{
		metadata: &dataAckStruct{
			baseStruct: baseStruct{
				protocol: uint8(dataClientToServer),
			},
			sessionID:  sessionID,
			seq:        seq,
			windowSize: uint16(segmentTreeCapacity),
			payloadLen: uint16(len(payload)),
		},
		payload:   append([]byte(nil), payload...),
		transport: transport,
	}
}

func testSessionSegment(protocol protocolType, sessionID uint32, transport common.TransportProtocol) *segment {
	return &segment{
		metadata: &sessionStruct{
			baseStruct: baseStruct{
				protocol: uint8(protocol),
			},
			sessionID: sessionID,
		},
		transport: transport,
	}
}
