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
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/bits"
	"net"
	"testing"
	"time"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/metrics"
	"github.com/enfein/mieru/v3/pkg/stderror"
	"google.golang.org/protobuf/proto"
)

func TestStreamLowEntropyReceive(t *testing.T) {
	payload := []byte("passive low entropy stream payload")
	wire, recvBlock, wantMetadata, _, _ := buildStreamLowEntropyWire(t, payload, 3, 5)
	reader, writer := net.Pipe()
	defer reader.Close()
	defer writer.Close()

	underlay := &StreamUnderlay{
		baseUnderlay: *newBaseUnderlay(true, 1400, nil),
		conn:         reader,
		block:        recvBlock,
	}
	writeDone := make(chan error, 1)
	go func() {
		_, err := writer.Write(wire)
		writeDone <- err
	}()

	seg, err := underlay.readOneSegment()
	if err != nil {
		t.Fatalf("readOneSegment() failed: %v", err)
	}
	if seg.Protocol() != dataServerToClientLowEntropy {
		t.Errorf("received protocol = %v, want %v", seg.Protocol(), dataServerToClientLowEntropy)
	}
	if !bytes.Equal(seg.payload, payload) {
		t.Errorf("received payload = %q, want %q", seg.payload, payload)
	}
	if got := seg.metadata.(*dataAckStruct); got.lowEntropyMask != wantMetadata.lowEntropyMask || got.extractedPayloadLen != uint16(len(payload)) {
		t.Errorf("received metadata = %v, want mask %08x and extracted length %d", got, wantMetadata.lowEntropyMask, len(payload))
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("writer.Write() failed: %v", err)
	}
}

func TestStreamLowEntropyReceiveMaxPayload(t *testing.T) {
	payload := make([]byte, 32764)
	for i := range payload {
		payload[i] = byte(i)
	}
	wire, recvBlock, wantMetadata, _, _ := buildStreamLowEntropyWire(t, payload, 0, 0)
	if wantMetadata.payloadLen != 65528 {
		t.Fatalf("encoded payload length = %d, want 65528", wantMetadata.payloadLen)
	}

	reader, writer := net.Pipe()
	defer reader.Close()
	defer writer.Close()
	underlay := &StreamUnderlay{
		baseUnderlay: *newBaseUnderlay(true, 1400, nil),
		conn:         reader,
		block:        recvBlock,
	}
	writeDone := make(chan error, 1)
	go func() {
		_, err := writer.Write(wire)
		writeDone <- err
	}()

	seg, err := underlay.readOneSegment()
	if err != nil {
		t.Fatalf("readOneSegment() failed: %v", err)
	}
	if !bytes.Equal(seg.payload, payload) {
		t.Fatal("received payload doesn't match maximum mode 32 payload")
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("writer.Write() failed: %v", err)
	}
}

// TestStreamLowEntropySendReceive verifies payload preservation, mask generation,
// padding metrics, and wire byte accounting across all low-entropy modes.
func TestStreamLowEntropySendReceive(t *testing.T) {
	tests := []struct {
		name       string
		protocol   protocolType
		mode       appctlpb.LowEntropyMode
		rotation   appctlpb.LowEntropyMaskRotation
		senderSide bool
	}{
		{"client mode 32 no rotation", dataClientToServerLowEntropy, appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_32, appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_NO_ROTATION, true},
		{"server mode 32 rotate right", dataServerToClientLowEntropy, appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_32, appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_RIGHT_7, false},
		{"client mode 40 rotate right", dataClientToServerLowEntropy, appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_40, appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_RIGHT_15, true},
		{"server mode 40 rotate left", dataServerToClientLowEntropy, appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_40, appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_LEFT_3, false},
		{"client mode 48 rotate left", dataClientToServerLowEntropy, appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_48, appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_LEFT_1, true},
		{"server mode 48 no rotation", dataServerToClientLowEntropy, appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_48, appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_NO_ROTATION, false},
		{"client mode 56 rotate right", dataClientToServerLowEntropy, appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_56, appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_RIGHT_1, true},
		{"server mode 56 rotate left", dataServerToClientLowEntropy, appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_56, appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_LEFT_15, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := []byte("stream low entropy outbound payload with a partial final chunk")
			payloadLen, err := lowEntropyEncodedPayloadLen(len(payload), test.mode)
			if err != nil {
				t.Fatalf("lowEntropyEncodedPayloadLen() failed: %v", err)
			}
			das := &dataAckStruct{
				baseStruct:             baseStruct{protocol: uint8(test.protocol)},
				lowEntropyMode:         uint8(test.mode),
				sessionID:              7,
				seq:                    11,
				windowSize:             segmentTreeCapacity,
				payloadLen:             payloadLen,
				extractedPayloadLen:    uint16(len(payload)),
				lowEntropyMaskRotation: uint8(test.rotation),
			}
			seg := &segment{metadata: das, payload: append([]byte(nil), payload...)}

			password := []byte(fmt.Sprintf("%s-password", t.Name()))
			block, err := cipher.BlockCipherFromPassword(password, false)
			if err != nil {
				t.Fatalf("BlockCipherFromPassword() failed: %v", err)
			}
			left, right := net.Pipe()
			defer left.Close()
			defer right.Close()
			zero := int32(0)
			trafficPattern := &appctlpb.TrafficPattern{Padding: &appctlpb.PaddingPattern{
				MaxMiddlePaddingLen: &zero,
				MaxEndPaddingLen:    &zero,
			}}
			sender := &StreamUnderlay{
				baseUnderlay: *newBaseUnderlay(test.senderSide, 1400, trafficPattern),
				conn:         left,
			}
			receiver := &StreamUnderlay{
				baseUnderlay: *newBaseUnderlay(!test.senderSide, 1400, nil),
				conn:         right,
			}
			if test.senderSide {
				sender.block = block.Clone()
				receiver.users = map[string]*appctlpb.User{"user": {
					Name:           proto.String("user"),
					HashedPassword: proto.String(hex.EncodeToString(password)),
				}}
			} else {
				sender.recv = block.Clone()
				receiver.block = block.Clone()
			}

			paddingBefore := metrics.OutputPaddingBytes.Load()
			lowEntropyPaddingBefore := metrics.OutputLowEntropyPaddingBytes.Load()
			writeDone := make(chan error, 1)
			go func() {
				writeDone <- sender.writeOneSegment(seg)
			}()
			received, err := receiver.readOneSegment()
			if err != nil {
				t.Fatalf("readOneSegment() failed: %v", err)
			}
			if err := <-writeDone; err != nil {
				t.Fatalf("writeOneSegment() failed: %v", err)
			}
			if received.Protocol() != test.protocol || !bytes.Equal(received.payload, payload) {
				t.Fatalf("received segment = %v, want protocol %v and original payload", received, test.protocol)
			}
			gotMetadata := received.metadata.(*dataAckStruct)
			params, err := buildLowEntropyParams(test.mode)
			if err != nil {
				t.Fatalf("buildLowEntropyParams() failed: %v", err)
			}
			if bits.OnesCount32(gotMetadata.lowEntropyMask) != params.halfMaskOnes {
				t.Errorf("half-mask has %d one-bits, want %d", bits.OnesCount32(gotMetadata.lowEntropyMask), params.halfMaskOnes)
			}
			if got := metrics.OutputPaddingBytes.Load() - paddingBefore; got != 0 {
				t.Errorf("OutputPaddingBytes delta = %d, want 0", got)
			}
			if got, want := metrics.OutputLowEntropyPaddingBytes.Load()-lowEntropyPaddingBefore, int64(payloadLen)-int64(len(payload)); got != want {
				t.Errorf("OutputLowEntropyPaddingBytes delta = %d, want %d", got, want)
			}
			if sender.OutBytes() != receiver.InBytes() {
				t.Errorf("stream wire byte counts = sent %d, received %d", sender.OutBytes(), receiver.InBytes())
			}
			if sender.OutBytes() <= int64(len(payload)) {
				t.Errorf("stream wire byte count = %d, want more than %d application bytes", sender.OutBytes(), len(payload))
			}
		})
	}
}

func TestPacketLowEntropyReceive(t *testing.T) {
	payload := []byte("passive low entropy packet payload")
	wire, block, _, _, _ := buildPacketLowEntropyWire(t, payload, 2, 4)
	receiverConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.ListenPacket() receiver failed: %v", err)
	}
	defer receiverConn.Close()
	senderConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.ListenPacket() sender failed: %v", err)
	}
	defer senderConn.Close()

	underlay := &PacketUnderlay{
		baseUnderlay: *newBaseUnderlay(true, 1400, nil),
		conn:         receiverConn,
		serverAddr:   senderConn.LocalAddr(),
		block:        block,
	}
	if _, err := senderConn.WriteTo(wire, receiverConn.LocalAddr()); err != nil {
		t.Fatalf("WriteTo() failed: %v", err)
	}
	seg, addr, err := underlay.readOneSegment()
	if err != nil {
		t.Fatalf("readOneSegment() failed: %v", err)
	}
	if addr.String() != senderConn.LocalAddr().String() {
		t.Errorf("sender address = %v, want %v", addr, senderConn.LocalAddr())
	}
	if seg.Protocol() != dataServerToClientLowEntropy {
		t.Errorf("received protocol = %v, want %v", seg.Protocol(), dataServerToClientLowEntropy)
	}
	if !bytes.Equal(seg.payload, payload) {
		t.Errorf("received payload = %q, want %q", seg.payload, payload)
	}
}

// TestPacketLowEntropySendReceive verifies payload preservation, MTU bounds,
// padding metrics, and wire byte accounting across all low-entropy modes.
func TestPacketLowEntropySendReceive(t *testing.T) {
	const mtu = 517
	tests := []struct {
		name     string
		protocol protocolType
		mode     appctlpb.LowEntropyMode
		rotation appctlpb.LowEntropyMaskRotation
	}{
		{"mode 32 no rotation", dataClientToServerLowEntropy, appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_32, appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_NO_ROTATION},
		{"mode 40 rotate right", dataServerToClientLowEntropy, appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_40, appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_RIGHT_7},
		{"mode 48 rotate left", dataClientToServerLowEntropy, appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_48, appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_LEFT_3},
		{"mode 56 rotate right", dataServerToClientLowEntropy, appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_56, appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_RIGHT_15},
	}
	for _, test := range tests {
		for _, adjustment := range []int{0, -1} {
			caseName := "maximum"
			if adjustment < 0 {
				caseName = "partial final chunk"
			}
			t.Run(test.name+"/"+caseName, func(t *testing.T) {
				fragmentSize, err := maxFragmentSize(mtu, common.PacketTransport, test.mode)
				if err != nil {
					t.Fatalf("maxFragmentSize() failed: %v", err)
				}
				payload := make([]byte, fragmentSize+adjustment)
				for i := range payload {
					payload[i] = byte(i)
				}
				payloadLen, err := lowEntropyEncodedPayloadLen(len(payload), test.mode)
				if err != nil {
					t.Fatalf("lowEntropyEncodedPayloadLen() failed: %v", err)
				}
				das := &dataAckStruct{
					baseStruct:             baseStruct{protocol: uint8(test.protocol)},
					lowEntropyMode:         uint8(test.mode),
					sessionID:              7,
					seq:                    11,
					windowSize:             segmentTreeCapacity,
					fragment:               2,
					payloadLen:             payloadLen,
					extractedPayloadLen:    uint16(len(payload)),
					lowEntropyMaskRotation: uint8(test.rotation),
				}
				seg := &segment{metadata: das, payload: append([]byte(nil), payload...), transport: common.PacketTransport}

				password := []byte(t.Name())
				block, err := cipher.BlockCipherFromPassword(password, true)
				if err != nil {
					t.Fatalf("BlockCipherFromPassword() failed: %v", err)
				}
				receiverConn, err := net.ListenPacket("udp", "127.0.0.1:0")
				if err != nil {
					t.Fatalf("net.ListenPacket() receiver failed: %v", err)
				}
				defer receiverConn.Close()
				senderConn, err := net.ListenPacket("udp", "127.0.0.1:0")
				if err != nil {
					t.Fatalf("net.ListenPacket() sender failed: %v", err)
				}
				defer senderConn.Close()
				sender := &PacketUnderlay{
					baseUnderlay: *newBaseUnderlay(true, mtu, nil),
					conn:         senderConn,
					serverAddr:   receiverConn.LocalAddr(),
					block:        block,
				}
				receiver := &PacketUnderlay{
					baseUnderlay: *newBaseUnderlay(true, mtu, nil),
					conn:         receiverConn,
					serverAddr:   senderConn.LocalAddr(),
					block:        block.Clone(),
				}

				paddingBefore := metrics.OutputPaddingBytes.Load()
				lowEntropyPaddingBefore := metrics.OutputLowEntropyPaddingBytes.Load()
				if err := sender.writeOneSegment(seg, receiverConn.LocalAddr()); err != nil {
					t.Fatalf("writeOneSegment() failed: %v", err)
				}
				received, _, err := receiver.readOneSegment()
				if err != nil {
					t.Fatalf("readOneSegment() failed: %v", err)
				}
				if received.Protocol() != test.protocol || !bytes.Equal(received.payload, payload) {
					t.Fatalf("received segment = %v, want protocol %v and original payload", received, test.protocol)
				}
				params, err := buildLowEntropyParams(test.mode)
				if err != nil {
					t.Fatalf("buildLowEntropyParams() failed: %v", err)
				}
				if bits.OnesCount32(received.metadata.(*dataAckStruct).lowEntropyMask) != params.halfMaskOnes {
					t.Errorf("received mask has wrong one-bit count")
				}
				wireLen := int(sender.OutBytes())
				minimumWireLen := packetOverhead + int(payloadLen)
				if wireLen < minimumWireLen || wireLen > mtu {
					t.Errorf("datagram length = %d, want [%d, %d]", wireLen, minimumWireLen, mtu)
				}
				if got, want := metrics.OutputPaddingBytes.Load()-paddingBefore, int64(wireLen-minimumWireLen); got != want {
					t.Errorf("OutputPaddingBytes delta = %d, want %d", got, want)
				}
				if got, want := metrics.OutputLowEntropyPaddingBytes.Load()-lowEntropyPaddingBefore, int64(payloadLen)-int64(len(payload)); got != want {
					t.Errorf("OutputLowEntropyPaddingBytes delta = %d, want %d", got, want)
				}
				if sender.OutBytes() != receiver.InBytes() {
					t.Errorf("packet wire byte counts = sent %d, received %d", sender.OutBytes(), receiver.InBytes())
				}
				if sender.OutBytes() <= int64(len(payload)) {
					t.Errorf("packet wire byte count = %d, want more than %d application bytes", sender.OutBytes(), len(payload))
				}
			})
		}
	}
}

// TestLowEntropyUnderlaysRejectCorruption verifies that stream corruption
// returns the expected error and packet corruption does not block later data.
func TestLowEntropyUnderlaysRejectCorruption(t *testing.T) {
	type corruptionCase struct {
		name           string
		mutateMetadata func(*dataAckStruct)
		mutateWire     func([]byte, int, int, *dataAckStruct)
		streamError    stderror.ErrorType
	}
	tests := []corruptionCase{
		{
			name: "mask metadata",
			mutateMetadata: func(das *dataAckStruct) {
				das.lowEntropyMask ^= 1
			},
			streamError: stderror.PROTOCOL_ERROR,
		},
		{
			name: "selected ciphertext bit",
			mutateWire: func(wire []byte, bodyStart, _ int, das *dataAckStruct) {
				mask := uint64(das.lowEntropyMask)<<32 | uint64(das.lowEntropyMask)
				flipLowEntropyChunkBit(wire[bodyStart:], mask&-mask)
			},
			streamError: stderror.CRYPTO_ERROR,
		},
		{
			name: "mixed padding bits",
			mutateWire: func(wire []byte, bodyStart, _ int, das *dataAckStruct) {
				mask := uint64(das.lowEntropyMask)<<32 | uint64(das.lowEntropyMask)
				paddingMask := ^mask
				flipLowEntropyChunkBit(wire[bodyStart:], paddingMask&-paddingMask)
			},
			streamError: stderror.PROTOCOL_ERROR,
		},
		{
			name: "extracted length metadata",
			mutateMetadata: func(das *dataAckStruct) {
				das.extractedPayloadLen = 1
			},
			streamError: stderror.PROTOCOL_ERROR,
		},
		{
			name: "authentication tag",
			mutateWire: func(wire []byte, bodyStart, bodyLen int, _ *dataAckStruct) {
				wire[bodyStart+bodyLen] ^= 1
			},
			streamError: stderror.CRYPTO_ERROR,
		},
		{
			name: "suffix length metadata",
			mutateMetadata: func(das *dataAckStruct) {
				das.suffixLen++
			},
			streamError: stderror.NETWORK_ERROR,
		},
	}

	for _, test := range tests {
		t.Run(test.name+"/stream", func(t *testing.T) {
			payload := []byte("stream corruption must not reach the application")
			wire, recvBlock, das, bodyStart, bodyLen := buildStreamLowEntropyWireWithMetadataMutation(t, payload, 2, 4, test.mutateMetadata)
			if test.mutateWire != nil {
				test.mutateWire(wire, bodyStart, bodyLen, das)
			}

			reader, writer := net.Pipe()
			underlay := &StreamUnderlay{
				baseUnderlay: *newBaseUnderlay(true, 1400, nil),
				conn:         reader,
				block:        recvBlock,
			}
			writeDone := make(chan struct{})
			go func() {
				_, _ = writer.Write(wire)
				_ = writer.Close()
				close(writeDone)
			}()
			if _, err := underlay.readOneSegment(); err == nil {
				t.Fatal("readOneSegment() succeeded, want corruption error")
			} else if got := stderror.GetErrorType(err); got != test.streamError {
				t.Fatalf("readOneSegment() error type = %v, want %v: %v", got, test.streamError, err)
			}
			_ = reader.Close()
			<-writeDone
		})

		t.Run(test.name+"/packet", func(t *testing.T) {
			payload := []byte("packet corruption must not reach the application")
			wire, block, das, bodyStart, bodyLen := buildPacketLowEntropyWireWithMetadataMutation(t, payload, 2, 4, test.mutateMetadata)
			if test.mutateWire != nil {
				test.mutateWire(wire, bodyStart, bodyLen, das)
			}
			validPayload := []byte("valid packet after corruption")
			validWire, _, _, _, _ := buildPacketLowEntropyWire(t, validPayload, 0, 0)

			receiverConn, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("net.ListenPacket() receiver failed: %v", err)
			}
			defer receiverConn.Close()
			senderConn, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("net.ListenPacket() sender failed: %v", err)
			}
			defer senderConn.Close()
			underlay := &PacketUnderlay{
				baseUnderlay: *newBaseUnderlay(true, 1400, nil),
				conn:         receiverConn,
				serverAddr:   senderConn.LocalAddr(),
				block:        block,
			}
			if _, err := senderConn.WriteTo(wire, receiverConn.LocalAddr()); err != nil {
				t.Fatalf("WriteTo() failed: %v", err)
			}
			if _, err := senderConn.WriteTo(validWire, receiverConn.LocalAddr()); err != nil {
				t.Fatalf("WriteTo() valid packet failed: %v", err)
			}
			seg, _, err := underlay.readOneSegment()
			if err != nil {
				t.Fatalf("readOneSegment() failed after corrupted packet: %v", err)
			}
			if !bytes.Equal(seg.payload, validPayload) {
				t.Fatalf("received payload = %q, want %q", seg.payload, validPayload)
			}
		})
	}
}

func TestPacketLowEntropyWaitsForSessionEstablishment(t *testing.T) {
	const mtu = 517
	mode := appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_32
	rotation := appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_LEFT_3
	zero := int32(0)
	trafficPattern := testLowEntropyTrafficPattern(mode, rotation)
	trafficPattern.Padding = &appctlpb.PaddingPattern{
		MaxMiddlePaddingLen: &zero,
		MaxEndPaddingLen:    &zero,
	}
	block, err := cipher.BlockCipherFromPassword([]byte(t.Name()), true)
	if err != nil {
		t.Fatalf("BlockCipherFromPassword() failed: %v", err)
	}
	receiverConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.ListenPacket() receiver failed: %v", err)
	}
	defer receiverConn.Close()
	senderConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.ListenPacket() sender failed: %v", err)
	}
	defer senderConn.Close()
	sender := &PacketUnderlay{
		baseUnderlay: *newBaseUnderlay(true, mtu, trafficPattern),
		conn:         senderConn,
		serverAddr:   receiverConn.LocalAddr(),
		block:        block,
	}
	session := NewSession(7, true, mtu, nil, trafficPattern)
	session.conn = sender
	session.transportProtocol = common.PacketTransport
	session.remoteAddr = receiverConn.LocalAddr()
	session.forwardStateTo(sessionAttached)

	payload := []byte("data must wait for the open-session response")
	writeDone := make(chan error, 1)
	go func() {
		n, err := session.Write(payload)
		if err == nil && n != len(payload) {
			err = fmt.Errorf("Write() wrote %d bytes, want %d", n, len(payload))
		}
		writeDone <- err
	}()
	deadline := time.Now().Add(time.Second)
	for session.sendQueue.Len() != 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := session.sendQueue.Len(); got != 2 {
		t.Fatalf("sendQueue length before output = %d, want 2", got)
	}

	decryptBlock := block.Clone()
	readProtocol := func() protocolType {
		wire := readPacketForTest(t, receiverConn)
		decryptedMetadata, err := decryptBlock.Decrypt(wire[:packetNonHeaderPosition])
		if err != nil {
			t.Fatalf("Decrypt(metadata) failed: %v", err)
		}
		return protocolType(decryptedMetadata[0])
	}

	// The first output pass may send only the open request. The queued data
	// remains in sendQueue while the session is attached.
	session.runOutputOncePacket()
	if got := readProtocol(); got != openSessionRequest {
		t.Fatalf("first transmitted protocol = %v, want %v", got, openSessionRequest)
	}
	if session.sendQueue.Len() != 1 || session.sendBuf.Len() != 1 {
		t.Fatalf("queue lengths after open request = (%d, %d), want (1, 1)", session.sendQueue.Len(), session.sendBuf.Len())
	}
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("Write() failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Write() didn't return after the open request advanced the send queue")
	}

	// Simulate loss of the first open request. Its timeout retransmission is
	// still allowed, but the data remains withheld.
	var openRequest *segment
	session.sendBuf.Ascend(func(iter *segment) bool {
		openRequest = iter
		return false
	})
	if openRequest == nil || openRequest.Protocol() != openSessionRequest {
		t.Fatalf("sendBuf does not contain the open-session request: %v", openRequest)
	}
	openRequest.txTime = time.Now().Add(-time.Second).UnixMicro()
	openRequest.txTimeout = time.Nanosecond
	session.nextRetransmissionTime.Store(0)
	session.runOutputOncePacket()
	if got := readProtocol(); got != openSessionRequest {
		t.Fatalf("retransmitted protocol = %v, want %v", got, openSessionRequest)
	}
	if got := session.sendQueue.Len(); got != 1 {
		t.Fatalf("sendQueue length before session establishment = %d, want 1", got)
	}

	// Once the heartbeat interval expires, an attached client still emits
	// nothing except open-request retransmissions. Keep a pending ACK set to
	// verify that suppression does not discard it.
	now := time.Now()
	openRequest.txTime = now.UnixMicro()
	openRequest.txTimeout = maxBackOffDuration
	session.nextRetransmissionTime.Store(now.Add(maxBackOffDuration).UnixMicro())
	session.lastTXTime.Store(now.Add(-2 * sessionHeartbeatInterval).UnixMicro())
	session.ackOnDataRecv.Store(true)
	session.runOutputOncePacket()
	expectNoPacketForTest(t, receiverConn)
	if !session.ackOnDataRecv.Load() {
		t.Fatal("suppressed ACK state was cleared before session establishment")
	}

	// An authenticated open response establishes the UDP session immediately
	// and releases the queued low-entropy data.
	response := &segment{
		metadata: &sessionStruct{
			baseStruct: baseStruct{protocol: uint8(openSessionResponse)},
			sessionID:  session.id,
			seq:        0,
		},
		transport: common.PacketTransport,
	}
	if err := session.input(response); err != nil {
		t.Fatalf("input(openSessionResponse) failed: %v", err)
	}
	if !session.isState(sessionEstablished) {
		t.Fatalf("session state = %v, want %v", sessionState(session.state.Load()), sessionEstablished)
	}
	session.ackOnDataRecv.Store(false)
	session.lastTXTime.Store(time.Now().UnixMicro())
	session.runOutputOncePacket()
	if got := readProtocol(); got != dataClientToServerLowEntropy {
		t.Fatalf("protocol after session establishment = %v, want %v", got, dataClientToServerLowEntropy)
	}
	if got := session.sendQueue.Len(); got != 0 {
		t.Fatalf("sendQueue length after session establishment = %d, want 0", got)
	}
}

// TestPacketLowEntropyRetransmissionReencoding verifies that retransmissions
// retain logical data while refreshing the mask, nonce, and ciphertext.
func TestPacketLowEntropyRetransmissionReencoding(t *testing.T) {
	const mtu = 517
	mode := appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_32
	payload := []byte("plaintext retained across packet retransmissions")
	payloadLen, err := lowEntropyEncodedPayloadLen(len(payload), mode)
	if err != nil {
		t.Fatalf("lowEntropyEncodedPayloadLen() failed: %v", err)
	}
	das := &dataAckStruct{
		baseStruct:             baseStruct{protocol: uint8(dataClientToServerLowEntropy)},
		lowEntropyMode:         uint8(mode),
		sessionID:              7,
		seq:                    19,
		windowSize:             segmentTreeCapacity,
		fragment:               3,
		payloadLen:             payloadLen,
		extractedPayloadLen:    uint16(len(payload)),
		lowEntropyMaskRotation: uint8(appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_LEFT_3),
	}
	seg := &segment{metadata: das, payload: append([]byte(nil), payload...), transport: common.PacketTransport}

	zero := int32(0)
	trafficPattern := &appctlpb.TrafficPattern{Padding: &appctlpb.PaddingPattern{
		MaxMiddlePaddingLen: &zero,
		MaxEndPaddingLen:    &zero,
	}}
	block, err := cipher.BlockCipherFromPassword([]byte(t.Name()), true)
	if err != nil {
		t.Fatalf("BlockCipherFromPassword() failed: %v", err)
	}
	receiverConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.ListenPacket() receiver failed: %v", err)
	}
	defer receiverConn.Close()
	senderConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.ListenPacket() sender failed: %v", err)
	}
	defer senderConn.Close()
	sender := &PacketUnderlay{
		baseUnderlay: *newBaseUnderlay(true, mtu, trafficPattern),
		conn:         senderConn,
		serverAddr:   receiverConn.LocalAddr(),
		block:        block,
	}
	session := NewSession(7, true, mtu, nil, nil)
	session.conn = sender
	session.transportProtocol = common.PacketTransport
	session.remoteAddr = receiverConn.LocalAddr()
	if !session.sendQueue.Insert(seg) {
		t.Fatal("failed to queue segment for transmission")
	}

	lowEntropyPaddingBefore := metrics.OutputLowEntropyPaddingBytes.Load()
	wires := make([][]byte, 2)
	metadatas := make([]*dataAckStruct, 2)
	decryptBlock := block.Clone()
	for i := range wires {
		if i == 1 {
			// No ACK arrived for the first datagram. Force its retransmission
			// deadline to expire before the second output pass.
			seg.txTime = time.Now().Add(-time.Second).UnixMicro()
			seg.txTimeout = time.Nanosecond
			session.nextRetransmissionTime.Store(0)
		}
		session.runOutputOncePacket()
		wires[i] = readPacketForTest(t, receiverConn)
		decryptedMetadata, err := decryptBlock.Decrypt(wires[i][:packetNonHeaderPosition])
		if err != nil {
			t.Fatalf("Decrypt(metadata) transmission %d failed: %v", i, err)
		}
		metadatas[i] = &dataAckStruct{}
		if err := metadatas[i].Unmarshal(decryptedMetadata); err != nil {
			t.Fatalf("Unmarshal(metadata) transmission %d failed: %v", i, err)
		}
		receiver := &PacketUnderlay{baseUnderlay: *newBaseUnderlay(true, mtu, nil)}
		received, err := receiver.parseDataAckSegment(metadatas[i], wires[i][:cipher.DefaultNonceSize], wires[i][packetNonHeaderPosition:], decryptBlock)
		if err != nil {
			t.Fatalf("parseDataAckSegment() transmission %d failed: %v", i, err)
		}
		if !bytes.Equal(received.payload, payload) {
			t.Fatalf("transmission %d payload = %q, want %q", i, received.payload, payload)
		}
	}
	if session.sendBuf.Len() != 1 || seg.txCount != 2 {
		t.Errorf("sendBuf length and transmission count = (%d, %d), want (1, 2)", session.sendBuf.Len(), seg.txCount)
	}
	if bytes.Equal(wires[0][:cipher.DefaultNonceSize], wires[1][:cipher.DefaultNonceSize]) {
		t.Error("retransmission reused the metadata nonce")
	}
	if metadatas[0].lowEntropyMask == metadatas[1].lowEntropyMask {
		t.Error("retransmission reused the low entropy mask")
	}
	if bytes.Equal(wires[0], wires[1]) {
		t.Error("retransmission reused the complete ciphertext datagram")
	}
	for i, got := range metadatas {
		if got.seq != 19 || got.fragment != 3 {
			t.Errorf("transmission %d changed sequence or fragment: %v", i, got)
		}
	}
	session.sendBuf.Ascend(func(got *segment) bool {
		if !bytes.Equal(got.payload, payload) {
			t.Errorf("sendBuf payload = %q, want retained plaintext %q", got.payload, payload)
		}
		return true
	})
	if got, want := metrics.OutputLowEntropyPaddingBytes.Load()-lowEntropyPaddingBefore, 2*(int64(payloadLen)-int64(len(payload))); got != want {
		t.Errorf("OutputLowEntropyPaddingBytes delta = %d, want %d", got, want)
	}
}

func readPacketForTest(t *testing.T, conn net.PacketConn) []byte {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline() failed: %v", err)
	}
	b := make([]byte, 1500)
	n, _, err := conn.ReadFrom(b)
	if err != nil {
		t.Fatalf("ReadFrom() failed: %v", err)
	}
	return append([]byte(nil), b[:n]...)
}

func expectNoPacketForTest(t *testing.T, conn net.PacketConn) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline() failed: %v", err)
	}
	b := make([]byte, 1500)
	if _, _, err := conn.ReadFrom(b); err == nil {
		t.Fatal("received an unexpected packet")
	} else if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
		t.Fatalf("ReadFrom() error = %v, want timeout", err)
	}
}

func buildStreamLowEntropyWire(t *testing.T, payload []byte, prefixLen, suffixLen int) ([]byte, cipher.BlockCipher, *dataAckStruct, int, int) {
	return buildStreamLowEntropyWireWithMetadataMutation(t, payload, prefixLen, suffixLen, nil)
}

func buildStreamLowEntropyWireWithMetadataMutation(t *testing.T, payload []byte, prefixLen, suffixLen int, mutateMetadata func(*dataAckStruct)) ([]byte, cipher.BlockCipher, *dataAckStruct, int, int) {
	t.Helper()
	password := []byte(fmt.Sprintf("%s-stream", t.Name()))
	sendBlock, err := cipher.BlockCipherFromPassword(password, false)
	if err != nil {
		t.Fatalf("BlockCipherFromPassword() failed: %v", err)
	}
	recvBlock := sendBlock.Clone()
	das := testLowEntropyMetadata(t, dataServerToClientLowEntropy, len(payload), prefixLen, suffixLen)
	wireMetadata := *das
	if mutateMetadata != nil {
		mutateMetadata(&wireMetadata)
	}
	encryptedMetadata, err := sendBlock.Encrypt(wireMetadata.Marshal())
	if err != nil {
		t.Fatalf("Encrypt(metadata) failed: %v", err)
	}
	encryptedPayload, err := sendBlock.Encrypt(payload)
	if err != nil {
		t.Fatalf("Encrypt(payload) failed: %v", err)
	}
	if len(encryptedPayload) != len(payload)+cipher.DefaultOverhead {
		t.Fatalf("encrypted payload length = %d, want %d", len(encryptedPayload), len(payload)+cipher.DefaultOverhead)
	}
	encoded, err := encodeLowEntropyPayloadWithPaddingBit(encryptedPayload[:len(payload)], appctlpb.LowEntropyMode(das.lowEntropyMode), das.lowEntropyMask, appctlpb.LowEntropyMaskRotation(das.lowEntropyMaskRotation), 0)
	if err != nil {
		t.Fatalf("encodeLowEntropyPayloadWithPaddingBit() failed: %v", err)
	}
	bodyStart := len(encryptedMetadata) + prefixLen
	wire := append([]byte(nil), encryptedMetadata...)
	wire = append(wire, make([]byte, prefixLen)...)
	wire = append(wire, encoded...)
	wire = append(wire, encryptedPayload[len(payload):]...)
	wire = append(wire, make([]byte, suffixLen)...)
	return wire, recvBlock, &wireMetadata, bodyStart, len(encoded)
}

func buildPacketLowEntropyWire(t *testing.T, payload []byte, prefixLen, suffixLen int) ([]byte, cipher.BlockCipher, *dataAckStruct, int, int) {
	return buildPacketLowEntropyWireWithMetadataMutation(t, payload, prefixLen, suffixLen, nil)
}

func buildPacketLowEntropyWireWithMetadataMutation(t *testing.T, payload []byte, prefixLen, suffixLen int, mutateMetadata func(*dataAckStruct)) ([]byte, cipher.BlockCipher, *dataAckStruct, int, int) {
	t.Helper()
	password := []byte(fmt.Sprintf("%s-packet", t.Name()))
	block, err := cipher.BlockCipherFromPassword(password, true)
	if err != nil {
		t.Fatalf("BlockCipherFromPassword() failed: %v", err)
	}
	das := testLowEntropyMetadata(t, dataServerToClientLowEntropy, len(payload), prefixLen, suffixLen)
	wireMetadata := *das
	if mutateMetadata != nil {
		mutateMetadata(&wireMetadata)
	}
	encryptedMetadata, err := block.Encrypt(wireMetadata.Marshal())
	if err != nil {
		t.Fatalf("Encrypt(metadata) failed: %v", err)
	}
	nonce := encryptedMetadata[:cipher.DefaultNonceSize]
	encryptedPayload, err := block.EncryptWithNonce(payload, nonce)
	if err != nil {
		t.Fatalf("EncryptWithNonce(payload) failed: %v", err)
	}
	encoded, err := encodeLowEntropyPayloadWithPaddingBit(encryptedPayload[:len(payload)], appctlpb.LowEntropyMode(das.lowEntropyMode), das.lowEntropyMask, appctlpb.LowEntropyMaskRotation(das.lowEntropyMaskRotation), 0)
	if err != nil {
		t.Fatalf("encodeLowEntropyPayloadWithPaddingBit() failed: %v", err)
	}
	remaining := make([]byte, 0, prefixLen+len(encoded)+cipher.DefaultOverhead+suffixLen)
	remaining = append(remaining, make([]byte, prefixLen)...)
	remaining = append(remaining, encoded...)
	remaining = append(remaining, encryptedPayload[len(payload):]...)
	remaining = append(remaining, make([]byte, suffixLen)...)
	wire := append(append([]byte(nil), encryptedMetadata...), remaining...)
	return wire, block, &wireMetadata, packetNonHeaderPosition + prefixLen, len(encoded)
}

func flipLowEntropyChunkBit(chunk []byte, bit uint64) {
	value := binary.BigEndian.Uint64(chunk)
	binary.BigEndian.PutUint64(chunk, value^bit)
}

func testLowEntropyMetadata(t *testing.T, protocol protocolType, extractedLen, prefixLen, suffixLen int) *dataAckStruct {
	t.Helper()
	mode := appctlpb.LowEntropyMode_LOW_ENTROPY_MODE_32
	payloadLen, err := lowEntropyEncodedPayloadLen(extractedLen, mode)
	if err != nil {
		t.Fatalf("lowEntropyEncodedPayloadLen() failed: %v", err)
	}
	return &dataAckStruct{
		baseStruct:             baseStruct{protocol: uint8(protocol)},
		lowEntropyMode:         uint8(mode),
		sessionID:              7,
		seq:                    11,
		windowSize:             segmentTreeCapacity,
		prefixLen:              uint8(prefixLen),
		payloadLen:             payloadLen,
		suffixLen:              uint8(suffixLen),
		lowEntropyMask:         0x0f0f0f0f,
		extractedPayloadLen:    uint16(extractedLen),
		lowEntropyMaskRotation: uint8(appctlpb.LowEntropyMaskRotation_LOW_ENTROPY_MASK_ROTATE_LEFT_3),
	}
}
