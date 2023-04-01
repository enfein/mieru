// Copyright (C) 2021  mieru authors
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

package kcp_test

import (
	"bytes"
	crand "crypto/rand"
	mrand "math/rand"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/enfein/mieru/pkg/kcp"
	"github.com/enfein/mieru/pkg/netutil"
	"github.com/enfein/mieru/pkg/stderror"
)

func newKCPPipe(t *testing.T) (*net.UDPConn, *net.UDPConn, *kcp.KCP, *kcp.KCP) {
	t.Helper()
	mrand.Seed(time.Now().UnixNano())

	port1, err := netutil.UnusedUDPPort()
	if err != nil {
		t.Fatalf("netutil.UnusedUDPPort() failed: %v", err)
	}
	port2, err := netutil.UnusedUDPPort()
	if err != nil {
		t.Fatalf("netutil.UnusedUDPPort() failed: %v", err)
	}

	convId := uint32(mrand.Int31())
	addr1, _ := net.ResolveUDPAddr("udp", "127.0.0.1:"+strconv.Itoa(port1))
	c1, err := net.ListenUDP("udp", addr1)
	if err != nil {
		t.Fatalf("net.ListenUDP() on %v failed: %v", addr1, err)
	}
	addr2, _ := net.ResolveUDPAddr("udp", "127.0.0.1:"+strconv.Itoa(port2))
	c2, err := net.ListenUDP("udp", addr2)
	if err != nil {
		t.Fatalf("net.ListenUDP() on %v failed: %v", addr2, err)
	}
	callback1 := func(buf []byte, size int) {
		if size >= kcp.IKCP_OVERHEAD {
			c1.WriteTo(buf[:size], addr2)
		}
	}
	callback2 := func(buf []byte, size int) {
		if size >= kcp.IKCP_OVERHEAD {
			c2.WriteTo(buf[:size], addr1)
		}
	}
	k1 := kcp.NewKCP(convId, callback1)
	k2 := kcp.NewKCP(convId, callback2)
	return c1, c2, k1, k2
}

// TestKCPSendRecv creates two KCP endpoints. One endpoint sends some data to
// the other endpoint. Verify the received data is the same as the sent data.
func TestKCPSendRecv(t *testing.T) {
	// KCP requires external mutex when calling methods.
	var mu1, mu2 sync.Mutex

	maxPayloadSize := kcp.IKCP_MTU_DEF
	var traffic [][]byte
	for i := 1; i <= maxPayloadSize; i++ {
		data := make([]byte, i)
		_, err := crand.Read(data)
		if err != nil {
			t.Fatalf("rand.Read() failed: %v", err)
		}
		traffic = append(traffic, data)
	}

	c1, c2, k1, k2 := newKCPPipe(t)
	time.Sleep(1 * time.Second)

	// Input received packets at endpoint 1.
	go func() {
		buf := make([]byte, kcp.MaxBufSize)
		for {
			n, _, err := c1.ReadFrom(buf)
			if err == nil && n > 0 {
				mu1.Lock()
				k1.Input(buf[:n], false)
				mu1.Unlock()
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Input received packets at endpoint 2.
	go func() {
		buf := make([]byte, kcp.MaxBufSize)
		for {
			n, _, err := c2.ReadFrom(buf)
			if err == nil && n > 0 {
				mu2.Lock()
				k2.Input(buf[:n], false)
				mu2.Unlock()
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Output egress packets at endpoint 1.
	go func() {
		for {
			mu1.Lock()
			k1.Output(false)
			mu1.Unlock()
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Output egress packets at endpoint 2.
	go func() {
		for {
			mu2.Lock()
			k2.Output(false)
			mu2.Unlock()
			time.Sleep(10 * time.Millisecond)
		}
	}()

	time.Sleep(1 * time.Second)

	for i := 0; i < maxPayloadSize; i++ {
		mu1.Lock()
		k1.Send(traffic[i])
		mu1.Unlock()
		time.Sleep(5 * time.Millisecond)

		recvBuf := make([]byte, kcp.MaxBufSize)
		for {
			mu2.Lock()
			n, err := k2.Recv(recvBuf)
			mu2.Unlock()
			if err != nil {
				if err != stderror.ErrNoEnoughData {
					t.Fatalf("error receive data in round %d: %v", i, err)
				} else {
					time.Sleep(5 * time.Millisecond)
				}
			} else {
				if n != i+1 {
					t.Fatalf("got %d bytes, want %d", n, i)
				}
				if !bytes.Equal(traffic[i], recvBuf[:n]) {
					t.Fatalf("data don't match")
				}
				break
			}
		}
	}
}
