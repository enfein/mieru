// Copyright (C) 2023  mieru authors
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

package protocolv2

import (
	"fmt"
	"net"

	"github.com/enfein/mieru/pkg/stderror"
)

// Mux manages the sessions and underlays.
type Mux struct {
	isClient          bool
	underlays         []Underlay
	serverListenAddrs []net.Addr
}

// ListenAndServeAll listens on all the server addresses and serves
// incoming requests. Call this method in client results in an error.
func (m *Mux) ListenAndServeAll() error {
	if m.isClient {
		return stderror.ErrInvalidOperation
	}
	if len(m.serverListenAddrs) == 0 {
		return fmt.Errorf("no server listen address found")
	}
	return nil
}
