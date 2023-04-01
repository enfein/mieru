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
	"io"
	"net"

	"github.com/enfein/mieru/pkg/stderror"
)

// UDPAssociateTunnelConn keeps the boundary of UDP packets when transmitted
// inside the proxy tunnel, which is typically a streaming pipe.
//
// Each original UDP packet will be wrapped like this
//
//	0x00 + 2 bytes of original length + original content + 0xff
//
// the length is encoded with little endian.
type UDPAssociateTunnelConn struct {
	io.ReadWriteCloser
}

func (c *UDPAssociateTunnelConn) Read(b []byte) (n int, err error) {
	delim := make([]byte, 1)
	if _, err = io.ReadFull(c.ReadWriteCloser, delim); err != nil {
		return 0, err
	}
	if delim[0] != 0x00 {
		return 0, stderror.ErrInvalidArgument
	}

	lengthBytes := make([]byte, 2)
	if _, err = io.ReadFull(c.ReadWriteCloser, lengthBytes); err != nil {
		return 0, err
	}
	length := int(binary.LittleEndian.Uint16(lengthBytes))
	if length > len(b) {
		return 0, io.ErrShortBuffer
	}

	if n, err = io.ReadFull(c.ReadWriteCloser, b[:length]); err != nil {
		return 0, err
	}

	if _, err = io.ReadFull(c.ReadWriteCloser, delim); err != nil {
		return 0, err
	}
	if delim[0] != 0xff {
		return 0, stderror.ErrInvalidArgument
	}
	return
}

func (c *UDPAssociateTunnelConn) Write(b []byte) (int, error) {
	if len(b) > 65535 {
		return 0, stderror.ErrOutOfRange
	}
	data := make([]byte, 4+len(b))
	data[0] = 0x00
	binary.LittleEndian.PutUint16(data[1:], uint16(len(b)))
	copy(data[3:], b)
	data[3+len(b)] = 0xff

	if _, err := c.ReadWriteCloser.Write(data); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (c *UDPAssociateTunnelConn) Close() error {
	return c.ReadWriteCloser.Close()
}

// WrapUDPAssociateTunnel wraps an existing connection with UDPAssociateTunnelConn.
func WrapUDPAssociateTunnel(conn io.ReadWriteCloser) *UDPAssociateTunnelConn {
	return &UDPAssociateTunnelConn{ReadWriteCloser: conn}
}

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
