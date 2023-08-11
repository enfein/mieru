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

package netutil

import "net"

// NetAddr implements net.Addr interface.
type NetAddr struct {
	Net string
	Str string
}

func (a NetAddr) Network() string {
	return a.Net
}

func (a NetAddr) String() string {
	return a.Str
}

// NilNetAddr returns an empty network address.
func NilNetAddr() net.Addr {
	return NetAddr{}
}

// IsNilNetAddr returns true if the net.Addr is nil / empty.
func IsNilNetAddr(addr net.Addr) bool {
	return addr == nil || (addr.Network() == "" && addr.String() == "")
}
