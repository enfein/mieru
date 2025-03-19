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
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

// PacketOverStreamTunnel keeps the boundary of packets when transmitted
// inside a streaming tunnel.
//
// Each original packet will be wrapped like this
//
//	0x00 + 2 bytes of original length + original packet content + 0xff
//
// the length is encoded with big endian.
//
// The destination of packets are negotiated separately.
type PacketOverStreamTunnel struct {
	net.Conn
}

var _ net.PacketConn = (*PacketOverStreamTunnel)(nil)

func (c *PacketOverStreamTunnel) Read(p []byte) (n int, err error) {
	delim := make([]byte, 1)
	if _, err = io.ReadFull(c.Conn, delim); err != nil {
		return 0, err
	}
	if delim[0] != 0x00 {
		return 0, fmt.Errorf("packet prefix 0x%x is not 0x00", delim[0])
	}

	lengthBytes := make([]byte, 2)
	if _, err = io.ReadFull(c.Conn, lengthBytes); err != nil {
		return 0, err
	}
	length := int(binary.BigEndian.Uint16(lengthBytes))
	if length > len(p) {
		return 0, io.ErrShortBuffer
	}

	if n, err = io.ReadFull(c.Conn, p[:length]); err != nil {
		return 0, err
	}

	if _, err = io.ReadFull(c.Conn, delim); err != nil {
		return 0, err
	}
	if delim[0] != 0xff {
		return 0, fmt.Errorf("packet suffix 0x%x is not 0xff", delim[0])
	}
	return
}

func (c *PacketOverStreamTunnel) Write(p []byte) (int, error) {
	if len(p) > 65535 {
		return 0, fmt.Errorf("packet length %d is larger than maximum length 65535", len(p))
	}
	data := make([]byte, 4+len(p))
	data[0] = 0x00
	binary.BigEndian.PutUint16(data[1:], uint16(len(p)))
	copy(data[3:], p)
	data[3+len(p)] = 0xff

	if _, err := c.Conn.Write(data); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *PacketOverStreamTunnel) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	addr = c.RemoteAddr()
	n, err = c.Read(p)
	return
}

func (c *PacketOverStreamTunnel) WriteTo(p []byte, _ net.Addr) (n int, err error) {
	return c.Write(p)
}

func (c *PacketOverStreamTunnel) Close() error {
	return c.Conn.Close()
}

// NewPacketOverStreamTunnel creates a PacketOverStreamTunnel on top of
// an existing stream oriented network connection.
func NewPacketOverStreamTunnel(conn net.Conn) *PacketOverStreamTunnel {
	return &PacketOverStreamTunnel{Conn: conn}
}
