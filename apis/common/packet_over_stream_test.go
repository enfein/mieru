// Copyright (C) 2025  mieru authors
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

package common_test

import (
	"bytes"
	"net"
	"sync"
	"testing"

	"github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/internal/testtool"
)

func TestUDPAssociateTunnelConn(t *testing.T) {
	in, out := testtool.BufPipe()
	inConn := common.NewPacketOverStreamTunnel(in)
	outConn := common.NewPacketOverStreamTunnel(out)

	data := []byte{8, 9, 6, 4}
	n, err := inConn.Write(data)
	if err != nil {
		t.Fatalf("Write() failed: %v", err)
	}
	if n != 4 {
		t.Fatalf("Write() returns %d, want %d", n, 4)
	}

	buf := make([]byte, 16)
	n, err = outConn.Read(buf)
	if err != nil {
		t.Fatalf("Read() failed: %v", err)
	}
	if n != 4 {
		t.Fatalf("Read() returns %d, want %d", n, 4)
	}

	if err = inConn.Close(); err != nil {
		t.Errorf("Close() failed: %v", err)
	}
	if err = outConn.Close(); err != nil {
		t.Errorf("Close() failed: %v", err)
	}
}

// writeFuncConn uses a custom Write function in net.Conn.
type writeFuncConn struct {
	net.Conn
	write func([]byte) (int, error)
}

func (c writeFuncConn) Write(p []byte) (int, error) {
	return c.write(p)
}

func TestPacketTunnelConcurrentWrites(t *testing.T) {
	const writers = 16
	entered := make(chan struct{}, writers)
	release := make(chan struct{})
	conn := writeFuncConn{write: func(p []byte) (int, error) {
		want := append([]byte(nil), p...)
		entered <- struct{}{}
		<-release
		if !bytes.Equal(p, want) {
			t.Error("frame changed before underlying Write returned")
		}
		return len(p), nil
	}}
	tunnel := common.PacketOverStreamTunnel{Conn: conn}
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			payload := bytes.Repeat([]byte{byte(i)}, 1400)
			if _, err := tunnel.Write(payload); err != nil {
				t.Error(err)
			}
			for j := range payload {
				payload[j] = 0
			}
		}(i)
	}
	for i := 0; i < writers; i++ {
		<-entered
	}
	close(release)
	wg.Wait()
}
