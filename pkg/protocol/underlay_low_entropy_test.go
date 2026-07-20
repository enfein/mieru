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
	"fmt"
	"net"
	"testing"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
)

func TestStreamUnderlayPassiveLowEntropyReceive(t *testing.T) {
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

func TestStreamUnderlayPassiveLowEntropyReceiveMaxPayload(t *testing.T) {
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

func TestPacketUnderlayPassiveLowEntropyReceive(t *testing.T) {
	payload := []byte("passive low entropy packet payload")
	wire, block, _, _ := buildPacketLowEntropyWire(t, payload, 2, 4)
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

func buildStreamLowEntropyWire(t *testing.T, payload []byte, prefixLen, suffixLen int) ([]byte, cipher.BlockCipher, *dataAckStruct, int, int) {
	t.Helper()
	password := []byte(fmt.Sprintf("%s-stream", t.Name()))
	sendBlock, err := cipher.BlockCipherFromPassword(password, false)
	if err != nil {
		t.Fatalf("BlockCipherFromPassword() failed: %v", err)
	}
	recvBlock := sendBlock.Clone()
	das := testLowEntropyMetadata(t, dataServerToClientLowEntropy, len(payload), prefixLen, suffixLen)
	encryptedMetadata, err := sendBlock.Encrypt(das.Marshal())
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
	return wire, recvBlock, das, bodyStart, len(encoded)
}

func buildPacketLowEntropyWire(t *testing.T, payload []byte, prefixLen, suffixLen int) ([]byte, cipher.BlockCipher, *dataAckStruct, []byte) {
	t.Helper()
	password := []byte(fmt.Sprintf("%s-packet", t.Name()))
	block, err := cipher.BlockCipherFromPassword(password, true)
	if err != nil {
		t.Fatalf("BlockCipherFromPassword() failed: %v", err)
	}
	das := testLowEntropyMetadata(t, dataServerToClientLowEntropy, len(payload), prefixLen, suffixLen)
	encryptedMetadata, err := block.Encrypt(das.Marshal())
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
	return wire, block, das, remaining
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
