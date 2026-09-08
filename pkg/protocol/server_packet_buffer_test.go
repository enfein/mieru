// Copyright (C) 2026  mieru authors
// SPDX-License-Identifier: GPL-3.0-or-later

package protocol

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

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

type blockingBufferConn struct {
	net.PacketConn
	blockedSetter          string
	setterEntered, release chan struct{}
}

func (c *blockingBufferConn) SetReadBuffer(int) error {
	if c.blockedSetter == "read" {
		close(c.setterEntered)
		<-c.release
	}
	return nil
}

func (c *blockingBufferConn) SetWriteBuffer(int) error {
	if c.blockedSetter == "write" {
		close(c.setterEntered)
		<-c.release
	}
	return nil
}

func (c *bufferErrorConn) SetReadBuffer(n int) error {
	c.readBytes = n
	return c.readErr
}

func (c *bufferErrorConn) SetWriteBuffer(n int) error {
	c.writeBytes = n
	return c.writeErr
}

func newBufferTestMux(t *testing.T, conn net.PacketConn) *Mux {
	t.Helper()
	m := NewMux(false)
	t.Cleanup(func() { m.Close() })
	m.SetServerUsers(users)
	m.SetPacketListenerFactory(bufferListenerFactory{conn})
	m.SetEndpoints([]UnderlayProperties{NewUnderlayProperties(1400, common.PacketTransport, conn.LocalAddr(), nil)})
	return m
}

func startBufferTestMux(t *testing.T, conn net.PacketConn) *Mux {
	t.Helper()
	m := newBufferTestMux(t, conn)
	if err := m.Start(); err != nil {
		t.Fatalf("server listener rejected usable socket: %v", err)
	}
	return m
}

func releaseBufferSetter(release chan struct{}) {
	select {
	case <-release:
	default:
		close(release)
	}
}

func requireMuxStartBlocked(t *testing.T, startDone <-chan error, blockedSetter string) {
	t.Helper()
	select {
	case err := <-startDone:
		t.Fatalf("Mux.Start() returned while the %s buffer setter was blocked: %v", blockedSetter, err)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestServerPacketBuffersConfiguredBeforeReadiness(t *testing.T) {
	for _, blockedSetter := range []string{"read", "write"} {
		t.Run(blockedSetter, func(t *testing.T) {
			raw, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { raw.Close() })
			conn := &blockingBufferConn{
				PacketConn:    raw,
				blockedSetter: blockedSetter,
				setterEntered: make(chan struct{}),
				release:       make(chan struct{}),
			}
			defer releaseBufferSetter(conn.release)

			m := newBufferTestMux(t, conn)
			startDone := make(chan error, 1)
			go func() {
				startDone <- m.Start()
			}()

			select {
			case <-conn.setterEntered:
			case err := <-startDone:
				t.Fatalf("Mux.Start() returned before the %s buffer setter ran: %v", blockedSetter, err)
			case <-time.After(time.Second):
				t.Fatalf("%s buffer setter did not run", blockedSetter)
			}
			requireMuxStartBlocked(t, startDone, blockedSetter)

			releaseBufferSetter(conn.release)
			select {
			case err := <-startDone:
				if err != nil {
					t.Fatalf("Mux.Start() failed after releasing the %s buffer setter: %v", blockedSetter, err)
				}
			case <-time.After(time.Second):
				t.Fatalf("Mux.Start() did not return after releasing the %s buffer setter", blockedSetter)
			}
		})
	}
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
