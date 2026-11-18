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
// along with this program.  If not, see <https://www.gnu.org/licenses/>

package protocol

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/log"
)

type auditTestBuffer struct {
	sync.Mutex
	bytes.Buffer
}

type auditTestConn struct{ *serverUserTestRemoteConn }

func (c *auditTestConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("203.0.113.10"), Port: 2053}
}

func newAuditTestConnection(mux *Mux, sourcePort int) (*StreamUnderlay, net.Conn) {
	reader, writer := net.Pipe()
	remote := &net.TCPAddr{IP: net.ParseIP("198.51.100.20"), Port: sourcePort}
	conn := &auditTestConn{&serverUserTestRemoteConn{Conn: reader, remoteAddr: remote}}
	return mux.serverWrapTCPConn(conn, 1400, nil).(*StreamUnderlay), writer
}

func (b *auditTestBuffer) Write(p []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	return b.Buffer.Write(p)
}

func (b *auditTestBuffer) String() string {
	b.Lock()
	defer b.Unlock()
	return b.Buffer.String()
}

func (b *auditTestBuffer) events() []string {
	var events []string
	for _, line := range strings.Split(b.String(), "\n") {
		if _, event, found := strings.Cut(line, "TCP authentication "); found {
			events = append(events, event)
		}
	}
	return events
}

func captureAuthAudit(t *testing.T) *auditTestBuffer {
	t.Helper()
	buffer := &auditTestBuffer{}
	logger := log.StandardLogger()
	previousOutput, previousLevel, previousFormatter := logger.Out, logger.GetLevel(), logger.Formatter
	log.SetOutput(buffer)
	log.SetFormatter(&log.DaemonFormatter{})
	t.Cleanup(func() {
		log.SetOutput(previousOutput)
		logger.SetLevel(previousLevel)
		log.SetFormatter(previousFormatter)
	})
	return buffer
}

func TestServerAuthAuditSharedPort(t *testing.T) {
	buffer := captureAuthAudit(t)
	log.SetLevel("DEBUG")

	firstUser, secondUser := "alice", "bob\n\"quoted"
	firstKey := cipher.HashPassword([]byte("first secret"), []byte(firstUser))
	secondKey := cipher.HashPassword([]byte("second secret"), []byte(secondUser))
	wrongKey := cipher.HashPassword([]byte("unregistered secret"), []byte(firstUser))
	mux := NewMux(false).SetServerUsers(userMap(makeTestUser(firstUser, firstKey), makeTestUser(secondUser, secondKey)))
	t.Cleanup(func() { _ = mux.Close() })

	tests := []struct {
		name, user string
		key        []byte
		port       int
		protocol   protocolType
		reason     string
	}{
		{"first_user", firstUser, firstKey, 12001, openSessionRequest, ""},
		{"second_user_same_ip", secondUser, secondKey, 12002, openSessionRequest, ""},
		{"wrong_password", firstUser, wrongKey, 12003, openSessionRequest, "invalid_credentials_or_corrupt_data"},
		{"invalid_role", firstUser, firstKey, 12004, openSessionResponse, "invalid_handshake"},
	}
	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := len(buffer.events())
			server, writer := newAuditTestConnection(mux, test.port)
			wire := buildServerUserStreamWire(t, test.key, test.user, testSessionSegment(test.protocol, uint32(i+1000), common.StreamTransport), nil, nil)
			go func() { _, _ = writer.Write(wire); _ = writer.Close() }()
			_ = server.RunEventLoop(context.Background())
			_ = server.Close()
			events := buffer.events()[before:]
			if len(events) != 1 {
				t.Fatalf("got %d auth events, want one", len(events))
			}
			endpoints := fmt.Sprintf("from 198.51.100.20:%d to 203.0.113.10:2053", test.port)
			var want string
			if test.reason == "" {
				want = fmt.Sprintf("succeeded for user %q %s, session %d", test.user, endpoints, i+1000)
			} else {
				want = fmt.Sprintf("failed %s (%s)", endpoints, test.reason)
			}
			if events[0] != want {
				t.Fatalf("got auth event %q, want %q", events[0], want)
			}
			if strings.Contains(buffer.String(), "first secret") || strings.Contains(buffer.String(), "second secret") {
				t.Fatal("audit output contains a password")
			}
		})
	}
}

func TestServerAuthAuditReusedConnection(t *testing.T) {
	buffer := captureAuthAudit(t)
	log.SetLevel("DEBUG")
	const user = "alice"
	key := cipher.HashPassword([]byte(t.Name()), []byte(user))
	block, err := cipher.BlockCipherFromPassword(key, false)
	if err != nil {
		t.Fatal(err)
	}
	block.SetBlockContext(cipher.BlockContext{UserName: user})
	mux := NewMux(false).SetServerUsers(userMap(makeTestUser(user, key)))
	t.Cleanup(func() { _ = mux.Close() })

	server, writer := newAuditTestConnection(mux, 12001)
	client := &StreamUnderlay{baseUnderlay: *newBaseUnderlay(true, 1400, nil), conn: writer, block: block}
	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		_ = server.RunEventLoop(context.Background())
	}()
	defer func() {
		_ = writer.Close()
		<-loopDone
		_ = server.Close()
	}()
	if err := writer.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint32{1000, 1001} {
		if err := client.writeOneSegment(testSessionSegment(openSessionRequest, id, common.StreamTransport)); err != nil {
			t.Fatalf("write session %d: %v", id, err)
		}
	}
	for _, id := range []uint32{1000, 1001} {
		select {
		case session := <-server.readySessions:
			if session.id != id {
				t.Fatalf("dispatched session %d, want %d", session.id, id)
			}
		case <-time.After(time.Second):
			t.Fatalf("session %d was not dispatched", id)
		}
	}
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- server.writeOneSegment(testSessionSegment(openSessionResponse, 1002, common.StreamTransport))
	}()
	for {
		response, err := client.readOneSegment()
		if err != nil || response == nil {
			t.Fatalf("read session response: segment=%v, err=%v", response, err)
		}
		if id, _ := response.SessionID(); id == 1002 {
			break
		}
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("write session response: %v", err)
	}
	_ = writer.Close()
	<-loopDone
	const want = `succeeded for user "alice" from 198.51.100.20:12001 to 203.0.113.10:2053, session 1000`
	if events := buffer.events(); len(events) != 1 || events[0] != want {
		t.Fatalf("got auth events %q, want only %q", events, want)
	}
	upload, download := client.OutBytes(), client.InBytes()
	if upload == 0 || download == 0 {
		t.Fatalf("expected both directions to carry traffic, got upload=%d download=%d", upload, download)
	}
	wantUsage := fmt.Sprintf(`TCP connection closed for user "alice" from 198.51.100.20, uploaded %d bytes, downloaded %d bytes`, upload, download)
	if logs := buffer.String(); strings.Count(logs, "TCP connection closed for user ") != 1 || !strings.Contains(logs, wantUsage+"\n") {
		t.Fatalf("want one connection usage log %q, got:\n%s", wantUsage, logs)
	}
}
