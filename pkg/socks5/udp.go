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

package socks5

import (
	"encoding/binary"
	"net"
)

// udpAddrToHeader returns a UDP associate header with the given
// destination address.
func udpAddrToHeader(addr *net.UDPAddr) []byte {
	if addr == nil {
		panic("When translating UDP address to UDP associate header, the UDP address is nil")
	}
	res := []byte{0, 0, 0}
	ip := addr.IP
	if ip.To4() != nil {
		res = append(res, 1)
		res = append(res, ip.To4()...)
	} else {
		res = append(res, 4)
		res = append(res, ip.To16()...)
	}
	return binary.BigEndian.AppendUint16(res, uint16(addr.Port))
}
