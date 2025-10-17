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
	"io"
	"net"
	"sync"
	"time"

	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/apis/model"
)

// Socks5Writer is an interface for socks5 objects that can be written to a writer.
type Socks5Writer interface {
	WriteToSocks5(writer io.Writer) error
}

var _ Socks5Writer = (*model.Request)(nil)
var _ Socks5Writer = (*model.Response)(nil)

// EarlyConn implements net.Conn interface.
// When the Write() method on the net.Conn is called for the first time,
// it performs the initial handshake and writes the request to the server.
type EarlyConn struct {
	net.Conn
	object        Socks5Writer
	handshakeOnce sync.Once
	handshakeErr  error
	handshaked    chan struct{}
}

// NewEarlyConn creates a new EarlyConn.
func NewEarlyConn(conn net.Conn, object Socks5Writer) *EarlyConn {
	return &EarlyConn{
		Conn:       conn,
		object:     object,
		handshaked: make(chan struct{}),
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

func (c *EarlyConn) doHandshakeAndWrite(b []byte) error {
	var buf bytes.Buffer
	if err := c.object.WriteToSocks5(&buf); err != nil {
		return err
	}
	if len(b) > 0 {
		buf.Write(b)
	}
	if _, err := c.Conn.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("failed to write socks5 object to the connection: %w", err)
	}

	// If this is a request, read the response.
	switch c.object.(type) {
	case *model.Request:
		c.Conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		defer c.Conn.SetReadDeadline(time.Time{})

		var resp model.Response
		if err := resp.ReadFromSocks5(c.Conn); err != nil {
			return fmt.Errorf("failed to read socks5 response from the server: %w", err)
		}
		if resp.Reply != constant.Socks5ReplySuccess {
			return fmt.Errorf("server returned socks5 error code %d", resp.Reply)
		}
	case *model.Response:
		// No need to read anything.
	default:
		return fmt.Errorf("unsupported object type for EarlyConn: %T", c.object)
	}

	return nil
}
