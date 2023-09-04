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

package util

import (
	"net"
	"sync"
)

// HierarchyConn closes sub-connections when this connection is closed.
type HierarchyConn interface {
	net.Conn

	AddSubConnection(conn net.Conn)
}

type hierarchyConnImpl struct {
	net.Conn
	subConnetions []HierarchyConn
	mu            sync.Mutex
}

func (h *hierarchyConnImpl) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sub := range h.subConnetions {
		if sub != nil {
			sub.Close()
		}
	}
	return h.Conn.Close()
}

func (h *hierarchyConnImpl) AddSubConnection(conn net.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subConnetions == nil {
		h.subConnetions = make([]HierarchyConn, 0)
	}
	h.subConnetions = append(h.subConnetions, WrapHierarchyConn(conn))
}

// WrapHierarchyConn wraps an existing connection with HierarchyConn.
func WrapHierarchyConn(conn net.Conn) HierarchyConn {
	return &hierarchyConnImpl{Conn: conn}
}
