// Copyright (C) 2026  mieru authors
// SPDX-License-Identifier: GPL-3.0-or-later

package protocol

import (
	"net"
	"syscall"
	"testing"
	"time"
)

type pausedPacketReader struct {
	*net.UDPConn
	release       chan struct{}
	entered       chan struct{}
	readBufferSet bool
}

func (c *pausedPacketReader) ReadFrom(b []byte) (int, net.Addr, error) {
	select {
	case c.entered <- struct{}{}:
	default:
	}
	<-c.release
	return c.UDPConn.ReadFrom(b)
}

func (c *pausedPacketReader) SetReadBuffer(bytes int) error {
	c.readBufferSet = true
	return c.UDPConn.SetReadBuffer(bytes)
}

func udpReadBufferSize(conn *net.UDPConn) (int, error) {
	rawConn, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var size int
	var socketErr error
	if err := rawConn.Control(func(fd uintptr) {
		size, socketErr = syscall.GetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF)
	}); err != nil {
		return 0, err
	}
	return size, socketErr
}

// A short scheduling pause must not drop a modest upload burst on the shared
// server listener. The factory recreates the production 208 KiB starting buffer;
// the real Mux must enlarge it before reporting readiness.
func TestServerMuxRetainsPacketBurstDuringReaderPause(t *testing.T) {
	raw, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { raw.Close() })
	if err := raw.SetReadBuffer(106496); err != nil {
		t.Fatal(err)
	}
	conn := &pausedPacketReader{UDPConn: raw, release: make(chan struct{}), entered: make(chan struct{}, 1)}
	// Release the real underlay's reader even when a test assertion fails.
	defer close(conn.release)
	startBufferTestMux(t, conn)
	if !conn.readBufferSet {
		t.Fatal("server did not attempt to enlarge the receive buffer")
	}
	const (
		packets     = 160
		packetBytes = 1000
		// Linux reports SO_RCVBUF as twice the requested capacity to account
		// for socket bookkeeping. Require that same conservative margin.
		requiredBuffer = 2 * packets * packetBytes
	)
	effectiveBuffer, err := udpReadBufferSize(raw)
	if err != nil {
		t.Fatalf("read effective UDP receive buffer: %v", err)
	}
	if effectiveBuffer < requiredBuffer {
		t.Skipf("kernel effective UDP receive buffer is %d bytes; burst test requires at least %d bytes", effectiveBuffer, requiredBuffer)
	}
	select {
	case <-conn.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("server packet reader did not start")
	}
	sender, err := net.DialUDP("udp4", nil, raw.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	payload := make([]byte, packetBytes)
	for i := 0; i < packets; i++ {
		payload[0] = byte(i)
		if _, err := sender.Write(payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := raw.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	seen := make(map[byte]bool)
	for len(seen) < packets {
		n, _, err := raw.ReadFromUDP(payload)
		if err != nil {
			t.Fatalf("shared listener lost upload burst during reader pause: received %d/%d: %v", len(seen), packets, err)
		}
		if n != packetBytes || seen[payload[0]] {
			t.Fatalf("unexpected burst datagram")
		}
		seen[payload[0]] = true
	}
	if err := raw.SetReadDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
}
