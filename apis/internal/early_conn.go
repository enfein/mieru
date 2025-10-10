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
	"strings"
	"sync"
	"time"

	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/apis/model"
)

// EarlyConn implements net.Conn interface.
// When the Write() method on the net.Conn is called for the first time,
// it performs the initial handshake and writes the request to the server.
type EarlyConn struct {
	net.Conn
	handshakeOnce sync.Once
	handshakeErr  error
	handshaked    chan struct{}
	netAddrSpec   model.NetAddrSpec
}

// NewEarlyConn creates a new EarlyConn.
func NewEarlyConn(conn net.Conn, netAddrSpec model.NetAddrSpec) *EarlyConn {
	return &EarlyConn{
		Conn:        conn,
		handshaked:  make(chan struct{}),
		netAddrSpec: netAddrSpec,
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
	var req bytes.Buffer
	isTCP := strings.HasPrefix(c.netAddrSpec.Network(), "tcp")
	isUDP := strings.HasPrefix(c.netAddrSpec.Network(), "udp")

	if isTCP {
		req.Write([]byte{constant.Socks5Version, constant.Socks5ConnectCmd, 0})
	} else if isUDP {
		req.Write([]byte{constant.Socks5Version, constant.Socks5UDPAssociateCmd, 0})
	} else {
		return fmt.Errorf("unsupported network type %s", c.netAddrSpec.Network())
	}

	if err := c.netAddrSpec.WriteToSocks5(&req); err != nil {
		return err
	}
	if len(b) > 0 {
		req.Write(b)
	}

	if _, err := c.Conn.Write(req.Bytes()); err != nil {
		return fmt.Errorf("failed to write socks5 connection request to the server: %w", err)
	}

	c.Conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer func() {
		c.Conn.SetReadDeadline(time.Time{})
	}()

	resp := make([]byte, 3)
	if _, err := io.ReadFull(c.Conn, resp); err != nil {
		return fmt.Errorf("failed to read socks5 connection response from the server: %w", err)
	}
	var respAddr model.NetAddrSpec
	if err := respAddr.ReadFromSocks5(c.Conn); err != nil {
		return fmt.Errorf("failed to read socks5 connection address response from the server: %w", err)
	}
	if resp[1] != 0 {
		return fmt.Errorf("server returned socks5 error code %d", resp[1])
	}

	return nil
}
