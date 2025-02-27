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
)

// PacketOverStreamTunnel keeps the boundary of packets when transmitted
// inside a streaming tunnel.
//
// Each original packet will be wrapped like this
//
//	0x00 + 2 bytes of original length + original packet content + 0xff
//
// the length is encoded with big endian.
type PacketOverStreamTunnel struct {
	io.ReadWriteCloser
}

func (c *PacketOverStreamTunnel) Read(b []byte) (n int, err error) {
	delim := make([]byte, 1)
	if _, err = io.ReadFull(c.ReadWriteCloser, delim); err != nil {
		return 0, err
	}
	if delim[0] != 0x00 {
		return 0, fmt.Errorf("packet prefix 0x%x is not 0x00", delim[0])
	}

	lengthBytes := make([]byte, 2)
	if _, err = io.ReadFull(c.ReadWriteCloser, lengthBytes); err != nil {
		return 0, err
	}
	length := int(binary.BigEndian.Uint16(lengthBytes))
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
		return 0, fmt.Errorf("packet suffix 0x%x is not 0xff", delim[0])
	}
	return
}

func (c *PacketOverStreamTunnel) Write(b []byte) (int, error) {
	if len(b) > 65535 {
		return 0, fmt.Errorf("packet length %d is larger than maximum length 65535", len(b))
	}
	data := make([]byte, 4+len(b))
	data[0] = 0x00
	binary.BigEndian.PutUint16(data[1:], uint16(len(b)))
	copy(data[3:], b)
	data[3+len(b)] = 0xff

	if _, err := c.ReadWriteCloser.Write(data); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (c *PacketOverStreamTunnel) Close() error {
	return c.ReadWriteCloser.Close()
}

// WrapPacketOverStream wraps an existing connection with PacketOverStreamTunnel.
func WrapPacketOverStream(conn io.ReadWriteCloser) *PacketOverStreamTunnel {
	return &PacketOverStreamTunnel{ReadWriteCloser: conn}
}
