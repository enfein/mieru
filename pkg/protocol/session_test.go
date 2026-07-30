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
	"bytes"
	"testing"
	"time"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/enfein/mieru/v3/pkg/common"
	"google.golang.org/protobuf/proto"
)

func TestDeliverSegmentUnblocksOnSessionClose(t *testing.T) {
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
	case <-time.After(100 * time.Millisecond):
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

func TestStreamUnderlayIgnoresResponsesForClosedSession(t *testing.T) {
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

func TestPacketUnderlayIgnoresResponsesForClosedSession(t *testing.T) {
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

// TestStreamLowEntropyWriteChunk verifies framing metadata, fragmentation,
// and plaintext preservation for all low-entropy modes.
func TestStreamLowEntropyWriteChunk(t *testing.T) {
	tests := []struct {
		mode              appctlpb.LowEntropyMode
		rotation          appctlpb.LowEntropyMaskRotation
		wantFragments     int
		wantFirstPartLen  int
		wantSecondPartLen int
	}{
		{appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_32, appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_NO_ROTATION, 2, 32764, 4},
		{appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_40, appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_RIGHT_7, 1, maxPDU, 0},
		{appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_48, appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_LEFT_3, 1, maxPDU, 0},
		{appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_56, appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_RIGHT_15, 1, maxPDU, 0},
	}
	payload := make([]byte, maxPDU)
	for i := range payload {
		payload[i] = byte(i)
	}
	for _, test := range tests {
		t.Run(test.mode.String(), func(t *testing.T) {
			s := NewSession(1, true, 1400, nil, testLowEntropyTrafficPattern(test.mode, test.rotation))
			s.transportProtocol = common.StreamTransport
			segments, n, err := collectSessionSegments(t, s, test.wantFragments, func() (int, error) {
				return s.writeChunk(payload)
			})
			if err != nil || n != len(payload) {
				t.Fatalf("writeChunk() = (%d, %v), want (%d, nil)", n, err, len(payload))
			}
			var reconstructed []byte
			for i, seg := range segments {
				das := seg.metadata.(*dataAckStruct)
				if das.Protocol() != dataClientToServerLowEntropy {
					t.Errorf("fragment %d protocol = %v, want %v", i, das.Protocol(), dataClientToServerLowEntropy)
				}
				if das.lowEntropyMode != uint8(test.mode) || das.lowEntropyMaskRotation != uint8(test.rotation) {
					t.Errorf("fragment %d low entropy config = (%d, %d), want (%d, %d)", i, das.lowEntropyMode, das.lowEntropyMaskRotation, test.mode, test.rotation)
				}
				if das.extractedPayloadLen != uint16(len(seg.payload)) {
					t.Errorf("fragment %d extracted length = %d, want %d", i, das.extractedPayloadLen, len(seg.payload))
				}
				wantEncodedLen, err := lowEntropyEncodedPayloadLen(len(seg.payload), test.mode)
				if err != nil {
					t.Fatalf("lowEntropyEncodedPayloadLen() failed: %v", err)
				}
				if das.payloadLen != wantEncodedLen || das.lowEntropyMask != 0 {
					t.Errorf("fragment %d metadata = %v, want encoded length %d and deferred mask", i, das, wantEncodedLen)
				}
				reconstructed = append(reconstructed, seg.payload...)
			}
			if len(segments[0].payload) != test.wantFirstPartLen {
				t.Errorf("first fragment length = %d, want %d", len(segments[0].payload), test.wantFirstPartLen)
			}
			if test.wantSecondPartLen > 0 && len(segments[1].payload) != test.wantSecondPartLen {
				t.Errorf("second fragment length = %d, want %d", len(segments[1].payload), test.wantSecondPartLen)
			}
			if !bytes.Equal(reconstructed, payload) {
				t.Fatal("fragment payloads don't reconstruct the original plaintext")
			}
		})
	}
}

// TestPacketLowEntropyWriteChunk verifies framing metadata, MTU-bounded
// fragmentation, and plaintext preservation for all low-entropy modes.
func TestPacketLowEntropyWriteChunk(t *testing.T) {
	tests := []struct {
		mode     appctlpb.LowEntropyMode
		rotation appctlpb.LowEntropyMaskRotation
	}{
		{appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_32, appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_NO_ROTATION},
		{appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_40, appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_RIGHT_7},
		{appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_48, appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_LEFT_3},
		{appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_56, appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_RIGHT_15},
	}
	payload := make([]byte, maxPDU)
	for i := range payload {
		payload[i] = byte(i)
	}
	for _, test := range tests {
		t.Run(test.mode.String(), func(t *testing.T) {
			s := NewSession(1, true, 1400, nil, testLowEntropyTrafficPattern(test.mode, test.rotation))
			s.transportProtocol = common.PacketTransport
			fragmentSize, err := maxFragmentSize(s.mtu, s.transportProtocol, test.mode)
			if err != nil {
				t.Fatalf("maxFragmentSize() failed: %v", err)
			}
			wantFragments := (len(payload)-1)/fragmentSize + 1
			segments, n, err := collectSessionSegments(t, s, wantFragments, func() (int, error) {
				return s.writeChunk(payload)
			})
			if err != nil || n != len(payload) {
				t.Fatalf("writeChunk() = (%d, %v), want (%d, nil)", n, err, len(payload))
			}
			var reconstructed []byte
			for i, seg := range segments {
				das := seg.metadata.(*dataAckStruct)
				if das.Protocol() != dataClientToServerLowEntropy {
					t.Errorf("fragment %d protocol = %v, want %v", i, das.Protocol(), dataClientToServerLowEntropy)
				}
				if das.lowEntropyMode != uint8(test.mode) || das.lowEntropyMaskRotation != uint8(test.rotation) {
					t.Errorf("fragment %d low entropy config = (%d, %d), want (%d, %d)", i, das.lowEntropyMode, das.lowEntropyMaskRotation, test.mode, test.rotation)
				}
				if len(seg.payload) > fragmentSize || das.extractedPayloadLen != uint16(len(seg.payload)) {
					t.Errorf("fragment %d plaintext length = %d, limit %d, extracted length %d", i, len(seg.payload), fragmentSize, das.extractedPayloadLen)
				}
				wantEncodedLen, err := lowEntropyEncodedPayloadLen(len(seg.payload), test.mode)
				if err != nil {
					t.Fatalf("lowEntropyEncodedPayloadLen() failed: %v", err)
				}
				if das.payloadLen != wantEncodedLen || int(das.payloadLen)+packetOverhead > s.mtu {
					t.Errorf("fragment %d encoded length = %d, want %d within MTU %d", i, das.payloadLen, wantEncodedLen, s.mtu)
				}
				reconstructed = append(reconstructed, seg.payload...)
			}
			if !bytes.Equal(reconstructed, payload) {
				t.Fatal("fragment payloads don't reconstruct the original plaintext")
			}
		})
	}
}

func TestFirstPacketWritePiggyback(t *testing.T) {
	payload := []byte("first packet write")
	s := NewSession(1, true, 1400, nil, nil)
	s.transportProtocol = common.PacketTransport
	s.forwardStateTo(sessionAttached)
	segments, n, err := collectSessionSegments(t, s, 1, func() (int, error) {
		return s.Write(payload)
	})
	if err != nil || n != len(payload) {
		t.Fatalf("Write() = (%d, %v), want (%d, nil)", n, err, len(payload))
	}
	ss := segments[0].metadata.(*sessionStruct)
	if ss.Protocol() != openSessionRequest || ss.payloadLen != uint16(len(payload)) || !bytes.Equal(segments[0].payload, payload) {
		t.Fatalf("openSessionRequest segment = %v, want piggyback payload", segments[0])
	}
}

func TestLowEntropyFirstPacketNoPiggyback(t *testing.T) {
	payload := []byte("low entropy first packet write")
	s := NewSession(1, true, 1400, nil, testLowEntropyTrafficPattern(appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_32, appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_LEFT_3))
	s.transportProtocol = common.PacketTransport
	s.forwardStateTo(sessionAttached)
	segments, n, err := collectSessionSegments(t, s, 2, func() (int, error) {
		return s.Write(payload)
	})
	if err != nil || n != len(payload) {
		t.Fatalf("Write() = (%d, %v), want (%d, nil)", n, err, len(payload))
	}
	ss := segments[0].metadata.(*sessionStruct)
	if ss.Protocol() != openSessionRequest || ss.payloadLen != 0 || len(segments[0].payload) != 0 {
		t.Fatalf("openSessionRequest segment = %v, want empty payload", segments[0])
	}
	if das := segments[1].metadata.(*dataAckStruct); das.Protocol() != dataClientToServerLowEntropy || !bytes.Equal(segments[1].payload, payload) {
		t.Fatalf("data segment = %v, want low entropy metadata and original payload", segments[1])
	}
}

func TestPacketLowEntropyDuplicateAndReordering(t *testing.T) {
	s := NewSession(1, true, 1400, nil, nil)
	s.transportProtocol = common.PacketTransport
	newSegment := func(seq uint32, payload string) *segment {
		return &segment{
			metadata: &dataAckStruct{
				baseStruct: baseStruct{protocol: uint8(dataServerToClientLowEntropy)},
				sessionID:  1,
				seq:        seq,
				windowSize: segmentTreeCapacity,
				payloadLen: uint16(len(payload)),
				fragment:   0,
			},
			payload:   []byte(payload),
			transport: common.PacketTransport,
		}
	}
	if err := s.input(newSegment(1, "second")); err != nil {
		t.Fatalf("input(out of order) failed: %v", err)
	}
	if err := s.input(newSegment(1, "second")); err != nil {
		t.Fatalf("input(duplicate) failed: %v", err)
	}
	if got := s.recvQueue.Len(); got != 0 {
		t.Fatalf("recvQueue length before gap closes = %d, want 0", got)
	}
	if err := s.input(newSegment(0, "first")); err != nil {
		t.Fatalf("input(first) failed: %v", err)
	}

	// Received segments should be in order and have no duplicates.
	for i, want := range []string{"first", "second"} {
		got, ok := s.recvQueue.DeleteMin()
		if !ok {
			t.Fatalf("recvQueue item %d is missing", i)
		}
		if string(got.payload) != want {
			t.Errorf("recvQueue item %d = %q, want %q", i, got.payload, want)
		}
	}
	if got := s.recvBuf.Len(); got != 0 {
		t.Errorf("recvBuf length = %d, want 0", got)
	}
}

// TestServerLowEntropySignalIsSticky verifies that the server keeps using
// low entropy after a client switches back to regular data segments.
func TestServerLowEntropySignalIsSticky(t *testing.T) {
	s := NewSession(1, false, 1400, nil, testLowEntropyTrafficPattern(
		appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_48,
		appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_LEFT_1,
	))
	s.transportProtocol = common.StreamTransport

	for seq, protocol := range []protocolType{dataClientToServerLowEntropy, dataClientToServer} {
		seg := testDataSegment(1, uint32(seq), []byte{byte(seq)}, common.StreamTransport)
		seg.metadata.(*dataAckStruct).baseStruct.protocol = uint8(protocol)
		if err := s.input(seg); err != nil {
			t.Fatalf("input(%v) failed: %v", protocol, err)
		}
		if !s.clientUseLowEntropy.Load() {
			t.Fatalf("client signal is false after receiving %v", protocol)
		}
	}

	segments, _, err := collectSessionSegments(t, s, 1, func() (int, error) {
		return s.writeChunk([]byte("sticky response"))
	})
	if err != nil {
		t.Fatalf("writeChunk() failed: %v", err)
	}
	if got := segments[0].Protocol(); got != dataServerToClientLowEntropy {
		t.Fatalf("response protocol = %v, want %v", got, dataServerToClientLowEntropy)
	}
}

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

// TestLowEntropyApplicationAccountingAndQuota verifies that accounting
// and quota checks use application byte counts rather than encoded wire sizes.
func TestLowEntropyApplicationAccountingAndQuota(t *testing.T) {
	userName := t.Name() + "-" + time.Now().Format("150405.000000000")
	user := &appctlpb.User{
		Name: proto.String(userName),
		Quotas: []*appctlpb.Quota{{
			Days:      proto.Int32(1),
			Megabytes: proto.Int32(0),
		}},
	}
	s := NewSession(1, false, 1400, map[string]*appctlpb.User{userName: user}, testLowEntropyTrafficPattern(
		appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_56,
		appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_RIGHT_1,
	))
	s.transportProtocol = common.StreamTransport
	s.forwardStateTo(sessionEstablished)

	block, err := cipher.BlockCipherFromPassword([]byte(t.Name()), false)
	if err != nil {
		t.Fatalf("BlockCipherFromPassword() failed: %v", err)
	}
	block.SetBlockContext(cipher.BlockContext{UserName: userName})
	request := []byte("application request bytes")
	incoming := testDataSegment(1, 0, request, common.StreamTransport)
	incoming.metadata.(*dataAckStruct).baseStruct.protocol = uint8(dataClientToServerLowEntropy)
	incoming.block = block
	if err := s.input(incoming); err != nil {
		t.Fatalf("input() failed: %v", err)
	}

	readBuf := make([]byte, len(request))
	if n, err := s.Read(readBuf); err != nil || n != len(request) || !bytes.Equal(readBuf, request) {
		t.Fatalf("Read() = (%d, %v, %q), want (%d, nil, %q)", n, err, readBuf, len(request), request)
	}
	if got := s.uploadBytes.Load(); got != int64(len(request)) {
		t.Fatalf("user upload bytes = %d, want %d application bytes", got, len(request))
	}

	response := []byte("application response bytes")
	segments, n, err := collectSessionSegments(t, s, 1, func() (int, error) {
		return s.Write(response)
	})
	if err != nil || n != len(response) {
		t.Fatalf("Write() = (%d, %v), want (%d, nil)", n, err, len(response))
	}
	if segments[0].Protocol() != dataServerToClientLowEntropy {
		t.Fatalf("response protocol = %v, want %v", segments[0].Protocol(), dataServerToClientLowEntropy)
	}
	if got := s.downloadBytes.Load(); got != int64(len(response)) {
		t.Fatalf("user download bytes = %d, want %d application bytes", got, len(response))
	}

	if ok, err := s.checkQuota(userName); err != nil || !ok {
		t.Fatalf("checkQuota() before application limit = (%v, %v), want (true, nil)", ok, err)
	}
	s.uploadBytes.Add(1 << 20)
	if ok, err := s.checkQuota(userName); err != nil || ok {
		t.Fatalf("checkQuota() after application limit = (%v, %v), want (false, nil)", ok, err)
	}
}

func testLowEntropyTrafficPattern(mode appctlpb.LowEntropyMode, rotation appctlpb.LowEntropyMaskRotation) *appctlpb.TrafficPattern {
	return &appctlpb.TrafficPattern{LowEntropy: &appctlpb.LowEntropyPattern{
		Mode:         mode.Enum(),
		MaskRotation: rotation.Enum(),
	}}
}

func collectSessionSegments(t *testing.T, s *Session, want int, write func() (int, error)) ([]*segment, int, error) {
	t.Helper()
	type writeResult struct {
		n   int
		err error
	}
	resultCh := make(chan writeResult, 1)
	go func() {
		n, err := write()
		resultCh <- writeResult{n: n, err: err}
	}()

	segments := make([]*segment, 0, want)
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for len(segments) < want {
		if seg, ok := s.sendQueue.DeleteMin(); ok {
			segments = append(segments, seg)
			continue
		}
		select {
		case <-s.sendQueue.chanNotEmptyEvent:
		case <-deadline.C:
			t.Fatalf("timed out collecting session segments: got %d, want %d", len(segments), want)
		}
	}
	select {
	case result := <-resultCh:
		return segments, result.n, result.err
	case <-deadline.C:
		t.Fatal("timed out waiting for session write")
		return nil, 0, nil
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
