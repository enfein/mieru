// Copyright (C) 2026  mieru authors
// SPDX-License-Identifier: GPL-3.0-or-later

package protocol

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/enfein/mieru/v3/pkg/common"
)

type bufferListenerFactory struct {
	conn net.PacketConn
}

func (f bufferListenerFactory) ListenPacket(context.Context, string, string) (net.PacketConn, error) {
	return f.conn, nil
}

type bufferErrorConn struct {
	net.PacketConn
	readErr, writeErr     error
	readBytes, writeBytes int
}

func (c *bufferErrorConn) SetReadBuffer(n int) error {
	c.readBytes = n
	return c.readErr
}

func (c *bufferErrorConn) SetWriteBuffer(n int) error {
	c.writeBytes = n
	return c.writeErr
}

func startBufferTestMux(t *testing.T, conn net.PacketConn) *Mux {
	t.Helper()
	m := NewMux(false)
	t.Cleanup(func() { m.Close() })
	m.SetServerUsers(users)
	m.SetPacketListenerFactory(bufferListenerFactory{conn})
	m.SetEndpoints([]UnderlayProperties{NewUnderlayProperties(1400, common.PacketTransport, conn.LocalAddr(), nil)})
	if err := m.Start(); err != nil {
		t.Fatalf("server listener rejected usable socket: %v", err)
	}
	return m
}

func TestServerPacketBufferErrorsDoNotPreventStartup(t *testing.T) {
	for _, tc := range []struct {
		name              string
		readErr, writeErr error
	}{
		{"read failure", errors.New("receive buffer rejected"), nil},
		{"write failure", nil, errors.New("send buffer rejected")},
		{"both failures", errors.New("receive buffer rejected"), errors.New("send buffer rejected")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { raw.Close() })
			conn := &bufferErrorConn{PacketConn: raw, readErr: tc.readErr, writeErr: tc.writeErr}
			startBufferTestMux(t, conn)
			if conn.readBytes != 4<<20 || conn.writeBytes != 1<<20 {
				t.Fatalf("server did not attempt both buffers before readiness: receive=%d send=%d", conn.readBytes, conn.writeBytes)
			}
		})
	}
}

func TestServerPacketBufferOptionalInterfaces(t *testing.T) {
	raw, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { raw.Close() })
	// An embedding application may provide a PacketConn without socket setters.
	startBufferTestMux(t, struct{ net.PacketConn }{raw})
}
