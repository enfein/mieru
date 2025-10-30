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

package internal

import (
	"bytes"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/apis/model"
)

// EarlyConn implements net.Conn interface.
// When the Write() method on the net.Conn is called for the first time,
// it performs the initial handshake and writes
// the request to the peer within the same network packet.
type EarlyConn struct {
	net.Conn
	request       atomic.Pointer[model.Request]
	peerResponse  atomic.Pointer[model.Response]
	handshakeOnce sync.Once
	handshakeErr  error
	handshaked    chan struct{}
}

// NewEarlyConn creates a new EarlyConn.
func NewEarlyConn(conn net.Conn) *EarlyConn {
	return &EarlyConn{
		Conn:       conn,
		handshaked: make(chan struct{}),
	}
}

// SetRequest sets the lazy request to be sent to the peer.
func (c *EarlyConn) SetRequest(request *model.Request) {
	select {
	case <-c.handshaked:
		panic("can't set request when handshake already done")
	default:
		c.request.Store(request)
	}
}

// Read will block until a message is received or an error occurs.
// It waits for the handshake to complete.
func (c *EarlyConn) Read(b []byte) (n int, err error) {
	<-c.handshaked
	if c.handshakeErr != nil {
		return 0, c.handshakeErr
	}
	return c.Conn.Read(b)
}

// Write will block until the message is sent or an error occurs.
// It triggers the initial handshake if it has not been performed yet,
// and sends the data in the same packet as the handshake request.
func (c *EarlyConn) Write(b []byte) (n int, err error) {
	var writtenDuringHandshake bool
	c.handshakeOnce.Do(func() {
		c.handshakeErr = c.doHandshakeAndWrite(b)
		close(c.handshaked)
		writtenDuringHandshake = true
	})

	if c.handshakeErr != nil {
		return 0, c.handshakeErr
	}
	if writtenDuringHandshake {
		return len(b), nil
	}

	return c.Conn.Write(b)
}

func (c *EarlyConn) Close() error {
	c.handshakeOnce.Do(func() {
		close(c.handshaked) // unblock Read() method
	})
	return c.Conn.Close()
}

// NeedHandshake returns true if the handshake has not been performed yet.
func (c *EarlyConn) NeedHandshake() bool {
	select {
	case <-c.handshaked:
		return false
	default:
		return true
	}
}

// PeerResponse returns the response from the peer.
// It is used by client application.
// It returns nil if the handshake has not been performed yet.
func (c *EarlyConn) PeerResponse() *model.Response {
	if c.request.Load() == nil {
		panic("can't get peer response when request is not set")
	}
	return c.peerResponse.Load()
}

func (c *EarlyConn) doHandshakeAndWrite(b []byte) error {
	var buf bytes.Buffer
	request := c.request.Load()
	if request != nil {
		if err := request.WriteToSocks5(&buf); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("request is not set")
	}
	if len(b) > 0 {
		buf.Write(b)
	}
	if _, err := c.Conn.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("failed to write socks5 object to the connection: %w", err)
	}

	// Read the response.
	c.Conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer c.Conn.SetReadDeadline(time.Time{})

	var resp model.Response
	if err := resp.ReadFromSocks5(c.Conn); err != nil {
		return fmt.Errorf("failed to read socks5 response from the server: %w", err)
	}
	if resp.Reply != constant.Socks5ReplySuccess {
		return fmt.Errorf("server returned socks5 error code %d", resp.Reply)
	}
	c.peerResponse.Store(&resp)
	return nil
}
