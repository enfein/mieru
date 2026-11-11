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
	"encoding/json"
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

func (b *auditTestBuffer) events(t *testing.T) []map[string]interface{} {
	t.Helper()
	b.Lock()
	defer b.Unlock()
	var result []map[string]interface{}
	for _, line := range strings.Split(b.Buffer.String(), "\n") {
		_, data, found := strings.Cut(line, "AUTH_AUDIT ")
		if !found {
			continue
		}
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			t.Fatalf("invalid audit JSON: %v", err)
		}
		result = append(result, event)
	}
	return result
}

func captureAuthAudit(t *testing.T) *auditTestBuffer {
	t.Helper()
	buffer := &auditTestBuffer{}
	logger := log.StandardLogger()
	previousOutput, previousLevel := logger.Out, logger.GetLevel()
	log.SetOutput(buffer)
	log.SetLevel("INFO")
	t.Cleanup(func() { log.SetOutput(previousOutput); logger.SetLevel(previousLevel) })
	return buffer
}

func TestServerAuthAuditSharedPort(t *testing.T) {
	t.Setenv("MITA_AUTH_AUDIT", "1")
	buffer := captureAuthAudit(t)

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
		success    bool
	}{
		{"first_user", firstUser, firstKey, 12001, openSessionRequest, true},
		{"second_user_same_ip", secondUser, secondKey, 12002, openSessionRequest, true},
		{"wrong_password", firstUser, wrongKey, 12003, openSessionRequest, false},
		{"invalid_role", firstUser, firstKey, 12004, openSessionResponse, false},
	}
	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := len(buffer.events(t))
			reader, writer := net.Pipe()
			conn := &auditTestConn{&serverUserTestRemoteConn{Conn: reader, remoteAddr: &net.TCPAddr{IP: net.ParseIP("198.51.100.20"), Port: test.port}}}
			server := mux.serverWrapTCPConn(conn, 1400, nil).(*StreamUnderlay)
			wire := buildServerUserStreamWire(t, test.key, test.user, testSessionSegment(test.protocol, uint32(i+1000), common.StreamTransport), nil, nil)
			go func() { _, _ = writer.Write(wire); _ = writer.Close() }()
			_ = server.RunEventLoop(context.Background())
			_ = server.Close()
			events := buffer.events(t)[before:]
			if len(events) != 1 {
				t.Fatalf("got %d auth events, want one", len(events))
			}
			event := events[0]
			if event["source_ip"] != "198.51.100.20" {
				t.Fatalf("wrong source IP: %v", event)
			}
			if event["source_port"] != fmt.Sprint(test.port) || event["local_port"] != "2053" || event["protocol"] != "TCP" {
				t.Fatalf("wrong transport endpoints: %v", event)
			}
			if _, err := time.Parse(time.RFC3339Nano, event["time_utc"].(string)); err != nil {
				t.Fatal(err)
			}
			if test.success {
				if event["event"] != "auth_success" || event["username"] != test.user {
					t.Fatalf("wrong identity: %v", event)
				}
			} else if event["event"] != "auth_failure" || event["username"] != nil {
				t.Fatalf("rejected handshake was assigned an identity: %v", event)
			}
			if strings.Contains(buffer.String(), "first secret") || strings.Contains(buffer.String(), "second secret") {
				t.Fatal("audit output contains a password")
			}
		})
	}
}

func TestServerAuthAuditOptions(t *testing.T) {
	buffer := captureAuthAudit(t)
	local := &net.TCPAddr{IP: net.ParseIP("2001:db8::10"), Port: 2053}
	remote := &net.TCPAddr{IP: net.ParseIP("2001:db8::20"), Port: 12001}
	tests := []struct {
		name, enabled, level string
		local, remote        net.Addr
		wantEvent            bool
	}{
		{"disabled", "", "INFO", local, remote, false},
		{"other_value", "true", "INFO", local, remote, false},
		{"warning_level", "1", "WARN", local, remote, false},
		{"missing_local", "1", "INFO", nil, remote, false},
		{"missing_remote", "1", "INFO", local, nil, false},
		{"ipv6", "1", "INFO", local, remote, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("MITA_AUTH_AUDIT", test.enabled)
			log.SetLevel(test.level)
			before := len(buffer.events(t))
			auditServerAuth("auth_success", "alice", "", test.local, test.remote, 123)
			events := buffer.events(t)[before:]
			if !test.wantEvent {
				if len(events) != 0 {
					t.Fatalf("unexpected audit event: %v", events)
				}
				return
			}
			if len(events) != 1 {
				t.Fatalf("got %d audit events, want one", len(events))
			}
			event := events[0]
			if event["source_ip"] != "2001:db8::20" || event["local_ip"] != "2001:db8::10" || event["session_id"] != float64(123) {
				t.Fatalf("wrong IPv6 endpoints or session ID: %v", event)
			}
		})
	}
}
