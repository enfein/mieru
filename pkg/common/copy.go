// Copyright (C) 2022  mieru authors
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

package common

import (
	"io"
	"net"
	"time"
)

const DefaultIdleTimeout = 15 * time.Minute

// TimeoutConn wraps a net.Conn and sets a combined read+write deadline
// before every Read or Write call. Any I/O activity on the connection
// resets the idle timer for both directions. This prevents the quiet
// side of an asymmetric transfer from expiring while the other side
// is still actively transferring data.
type TimeoutConn struct {
	net.Conn
	timeout time.Duration
}

// NewTimeoutConn creates a TimeoutConn that resets both read and write
// deadlines on any I/O activity.
func NewTimeoutConn(conn net.Conn, timeout time.Duration) *TimeoutConn {
	return &TimeoutConn{Conn: conn, timeout: timeout}
}

func (c *TimeoutConn) Read(p []byte) (int, error) {
	c.Conn.SetDeadline(time.Now().Add(c.timeout))
	return c.Conn.Read(p)
}

func (c *TimeoutConn) Write(p []byte) (int, error) {
	c.Conn.SetDeadline(time.Now().Add(c.timeout))
	return c.Conn.Write(p)
}

// Close is provided to satisfy io.ReadWriteCloser. It delegates to the
// underlying net.Conn.Close().
func (c *TimeoutConn) Close() error {
	return c.Conn.Close()
}

// BidiCopy does bi-directional data copy.
// An idle timeout of DefaultIdleTimeout is applied to prevent hanging on
// connections that accept TCP but never send data.
// Any I/O activity (read or write) on either connection resets the deadline
// for both directions, so long-lived asymmetric transfers are not killed
// by the quiet side timing out.
func BidiCopy(conn1, conn2 io.ReadWriteCloser) error {
	return bidiCopyWithTimeout(conn1, conn2, DefaultIdleTimeout)
}

func bidiCopyWithTimeout(conn1, conn2 io.ReadWriteCloser, idleTimeout time.Duration) error {
	errCh := make(chan error, 2)

	// Wrap connections with idle timeout if they support it.
	if idleTimeout > 0 {
		if c, ok := conn1.(net.Conn); ok {
			conn1 = NewTimeoutConn(c, idleTimeout)
		}
		if c, ok := conn2.(net.Conn); ok {
			conn2 = NewTimeoutConn(c, idleTimeout)
		}
	}

	go func() {
		_, err := io.Copy(conn1, conn2)
		conn1.Close()
		errCh <- err
	}()
	go func() {
		_, err := io.Copy(conn2, conn1)
		conn2.Close()
		errCh <- err
	}()

	err := <-errCh
	<-errCh
	return err
}
