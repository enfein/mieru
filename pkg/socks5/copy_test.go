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

package socks5

import (
	"errors"
	"net"
	"testing"
	"time"
)

func TestWriteTimeoutConnStopsBlockedWrite(t *testing.T) {
	conn, peer := net.Pipe()
	defer conn.Close()
	defer peer.Close()

	timeout := 20 * time.Millisecond
	wrapped := newWriteTimeoutConn(conn, timeout)
	started := time.Now()
	_, err := wrapped.Write([]byte("blocked"))
	if err == nil {
		t.Fatal("Write() succeeded while the peer was not reading")
	}
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("Write() error = %v, want a timeout", err)
	}
	if elapsed := time.Since(started); elapsed < timeout {
		t.Fatalf("Write() returned after %v, before timeout %v", elapsed, timeout)
	}
}

func TestWriteTimeoutConnDoesNotLimitQuietConnectionLifetime(t *testing.T) {
	conn, peer := net.Pipe()
	defer conn.Close()
	defer peer.Close()

	timeout := 20 * time.Millisecond
	wrapped := newWriteTimeoutConn(conn, timeout)
	readDone := make(chan error, 1)
	go func() {
		buf := make([]byte, 1)
		if _, err := peer.Read(buf); err != nil {
			readDone <- err
			return
		}
		time.Sleep(2 * timeout)
		_, err := peer.Read(buf)
		readDone <- err
	}()

	if _, err := wrapped.Write([]byte{1}); err != nil {
		t.Fatalf("first Write() failed: %v", err)
	}
	time.Sleep(2 * timeout)
	if _, err := wrapped.Write([]byte{2}); err != nil {
		t.Fatalf("second Write() failed after a quiet interval: %v", err)
	}
	if err := <-readDone; err != nil {
		t.Fatalf("peer.Read() failed: %v", err)
	}
}
