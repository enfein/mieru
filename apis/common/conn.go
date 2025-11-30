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

package common

import (
	"net"
	"sync"
)

// HierarchyConn closes all child connections when this connection is closed.
type HierarchyConn interface {
	net.Conn

	// Add attach a child connection to this connection.
	// The child connection is closed when this connection close.
	Add(conn net.Conn)
}

type hierarchyConn struct {
	net.Conn
	subConnetions []HierarchyConn
	mu            sync.Mutex
}

var (
	_ HierarchyConn = (*hierarchyConn)(nil)
	_ UserContext   = (*hierarchyConn)(nil)
)

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

func (h *hierarchyConn) Add(conn net.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subConnetions = append(h.subConnetions, WrapHierarchyConn(conn))
}

func (h *hierarchyConn) UserName() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if userCtx, ok := h.Conn.(UserContext); ok {
		return userCtx.UserName()
	}
	return ""
}

// WrapHierarchyConn wraps an existing connection with HierarchyConn.
func WrapHierarchyConn(conn net.Conn) HierarchyConn {
	return &hierarchyConn{Conn: conn}
}
