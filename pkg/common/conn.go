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
	"context"
	"io"
	"net"
	"sync"
	"time"
)

// NewWriteTimeoutConn wraps conn to limit each write to timeout without imposing
// an idle or lifetime limit on the connection. Each write resets the underlying
// connection's write deadline.
func NewWriteTimeoutConn(conn net.Conn, timeout time.Duration) net.Conn {
	return &writeTimeoutConn{Conn: conn, timeout: timeout}
}

type writeTimeoutConn struct {
	net.Conn
	timeout time.Duration
}

func (c *writeTimeoutConn) Write(p []byte) (int, error) {
	if err := c.SetWriteDeadline(time.Now().Add(c.timeout)); err != nil {
		return 0, err
	}
	return c.Conn.Write(p)
}

func NewIdleTrackingConn(conn net.Conn) *IdleTrackingConn {
	return &IdleTrackingConn{
		Conn:         conn,
		lastActivity: time.Now(),
	}
}

// IdleTrackingConn records transport activity and can close the connection
// when it is idle.
type IdleTrackingConn struct {
	net.Conn
	mu           sync.Mutex
	lastActivity time.Time
}

func (c *IdleTrackingConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	c.recordActivity(n)
	return n, err
}

func (c *IdleTrackingConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	c.recordActivity(n)
	return n, err
}

// CloseIfIdle closes the connection if there is no read or write within
// the timeout period. Returns true if the connection is closed by this call.
func (c *IdleTrackingConn) CloseIfIdle(timeout time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if timeout <= 0 || time.Since(c.lastActivity) < timeout {
		return false
	}
	c.Conn.Close()
	return true
}

func (c *IdleTrackingConn) recordActivity(n int) {
	if n > 0 {
		c.mu.Lock()
		c.lastActivity = time.Now()
		c.mu.Unlock()
	}
}

type SetReadDeadlineInterface interface {
	SetReadDeadline(t time.Time) error
}

// net.Conn implements SetReadDeadlineInterface.
var _ SetReadDeadlineInterface = net.Conn(nil)

// SetReadTimeout set read deadline.
// It cancels the deadline if the timeout is 0 or negative.
func SetReadTimeout(conn SetReadDeadlineInterface, timeout time.Duration) {
	if timeout > 0 {
		conn.SetReadDeadline(time.Now().Add(timeout))
	} else {
		conn.SetReadDeadline(time.Time{})
	}
}

// ReadAllAndDiscard reads from r until an error or EOF.
// All the data are discarded.
func ReadAllAndDiscard(r io.Reader) {
	b := make([]byte, 1024)
	for {
		_, err := r.Read(b)
		if err != nil {
			return
		}
	}
}

// RoundTrip sends a request to the connection and returns the response.
func RoundTrip(ctx context.Context, rw io.ReadWriter, req []byte, maxRespSize int) (resp []byte, err error) {
	_, err = rw.Write(req)
	if err != nil {
		return
	}

	resp = make([]byte, maxRespSize)
	var n int
	n, err = rw.Read(resp)
	resp = resp[:n]
	return
}
