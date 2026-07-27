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
	"context"
	"encoding/hex"
	"net"
	"testing"
	"time"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/enfein/mieru/v3/pkg/common"
	"google.golang.org/protobuf/proto"
)

func TestPacketUnderlayServerDropsInvalidControlMessage(t *testing.T) {
	serverConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.ListenPacket() server failed: %v", err)
	}
	senderConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		serverConn.Close()
		t.Fatalf("net.ListenPacket() sender failed: %v", err)
	}
	defer senderConn.Close()

	password := []byte(t.Name())
	userName := "user"
	block, err := cipher.BlockCipherFromPassword(password, true)
	if err != nil {
		serverConn.Close()
		t.Fatalf("BlockCipherFromPassword() failed: %v", err)
	}
	block.SetBlockContext(cipher.BlockContext{UserName: userName})

	server := &PacketUnderlay{
		baseUnderlay:       *newBaseUnderlay(false, 1400, nil),
		conn:               serverConn,
		sessionCleanTicker: time.NewTicker(sessionCleanInterval),
		users: map[string]*appctlpb.User{userName: {
			Name:           proto.String(userName),
			HashedPassword: proto.String(hex.EncodeToString(password)),
		}},
	}
	sender := &PacketUnderlay{
		baseUnderlay: *newBaseUnderlay(true, 1400, nil),
		conn:         senderConn,
		serverAddr:   serverConn.LocalAddr(),
		block:        block.Clone(),
	}

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- server.RunEventLoop(context.Background())
	}()
	defer func() {
		_ = server.Close()
		_ = serverConn.Close()
		select {
		case <-loopDone:
		case <-time.After(time.Second):
			t.Error("PacketUnderlay.RunEventLoop() didn't stop")
		}
	}()

	invalid := testSessionSegment(openSessionResponse, 1, common.PacketTransport)
	if err := sender.writeOneSegment(invalid, serverConn.LocalAddr()); err != nil {
		t.Fatalf("writeOneSegment(invalid control) failed: %v", err)
	}
	select {
	case err := <-loopDone:
		t.Fatalf("RunEventLoop() stopped after invalid control message: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	valid := testSessionSegment(openSessionRequest, 2, common.PacketTransport)
	if err := sender.writeOneSegment(valid, serverConn.LocalAddr()); err != nil {
		t.Fatalf("writeOneSegment(valid control) failed: %v", err)
	}
	acceptDone := make(chan net.Conn, 1)
	go func() {
		conn, _ := server.Accept()
		acceptDone <- conn
	}()
	select {
	case conn := <-acceptDone:
		if conn == nil {
			t.Fatal("Accept() returned a nil connection")
		}
		if got := conn.(*Session).id; got != 2 {
			t.Fatalf("accepted session ID = %d, want 2", got)
		}
	case <-time.After(time.Second):
		t.Fatal("server didn't accept a valid session after invalid control message")
	}
}

func TestPacketUnderlayClientClosesUnknownSession(t *testing.T) {
	clientConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.ListenPacket() client failed: %v", err)
	}
	serverConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		clientConn.Close()
		t.Fatalf("net.ListenPacket() server failed: %v", err)
	}
	defer serverConn.Close()

	block, err := cipher.BlockCipherFromPassword([]byte(t.Name()), true)
	if err != nil {
		clientConn.Close()
		t.Fatalf("BlockCipherFromPassword() failed: %v", err)
	}
	block.SetBlockContext(cipher.BlockContext{UserName: "user"})

	client := &PacketUnderlay{
		baseUnderlay:       *newBaseUnderlay(true, 1400, nil),
		conn:               clientConn,
		sessionCleanTicker: time.NewTicker(sessionCleanInterval),
		serverAddr:         serverConn.LocalAddr(),
		block:              block.Clone(),
	}
	sender := &PacketUnderlay{
		baseUnderlay: *newBaseUnderlay(true, 1400, nil),
		conn:         serverConn,
		serverAddr:   clientConn.LocalAddr(),
		block:        block.Clone(),
	}

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- client.RunEventLoop(context.Background())
	}()
	defer func() {
		_ = client.Close()
		_ = clientConn.Close()
		select {
		case <-loopDone:
		case <-time.After(time.Second):
			t.Error("PacketUnderlay.RunEventLoop() didn't stop")
		}
	}()

	const sessionID = 42
	payload := []byte("data for a stale session")
	data := &segment{
		metadata: &dataAckStruct{
			baseStruct: baseStruct{protocol: uint8(dataServerToClient)},
			sessionID:  sessionID,
			seq:        10,
			unAckSeq:   7,
			windowSize: segmentTreeCapacity,
			payloadLen: uint16(len(payload)),
		},
		payload:   payload,
		transport: common.PacketTransport,
	}
	if err := sender.writeOneSegment(data, clientConn.LocalAddr()); err != nil {
		t.Fatalf("writeOneSegment(data) failed: %v", err)
	}

	wire := readPacketForTest(t, serverConn)
	if len(wire) < packetNonHeaderPosition {
		t.Fatalf("close request length = %d, want at least %d", len(wire), packetNonHeaderPosition)
	}
	decryptedMetadata, err := block.Decrypt(wire[:packetNonHeaderPosition])
	if err != nil {
		t.Fatalf("Decrypt(close request metadata) failed: %v", err)
	}
	closeMetadata := &sessionStruct{}
	if err := closeMetadata.Unmarshal(decryptedMetadata); err != nil {
		t.Fatalf("Unmarshal(close request metadata) failed: %v", err)
	}
	if closeMetadata.Protocol() != closeSessionRequest || closeMetadata.sessionID != sessionID {
		t.Fatalf("received metadata = %v, want closeSessionRequest for session %d", closeMetadata, sessionID)
	}
}
