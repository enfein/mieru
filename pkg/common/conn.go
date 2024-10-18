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

type SetReadDeadlineInterface interface {
	SetReadDeadline(t time.Time) error
}

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

// HierarchyConn closes sub-connections when this connection is closed.
type HierarchyConn interface {
	net.Conn

	// AddSubConnection attach a child connection to this connection.
	// The child connection is closed when this connection close.
	AddSubConnection(conn net.Conn)
}

type hierarchyConn struct {
	net.Conn
	subConnetions []HierarchyConn
	mu            sync.Mutex
}

func (h *hierarchyConn) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sub := range h.subConnetions {
		if sub != nil {
			sub.Close()
		}
	}
	return h.Conn.Close()
}

func (h *hierarchyConn) AddSubConnection(conn net.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subConnetions == nil {
		h.subConnetions = make([]HierarchyConn, 0)
	}
	h.subConnetions = append(h.subConnetions, WrapHierarchyConn(conn))
}

// WrapHierarchyConn wraps an existing connection with HierarchyConn.
func WrapHierarchyConn(conn net.Conn) HierarchyConn {
	return &hierarchyConn{Conn: conn}
}
