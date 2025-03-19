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
	"bytes"
	"fmt"
	"net"

	"github.com/enfein/mieru/v3/apis/model"
)

// UDPAssociateWrapper wraps a raw UDP packet with socks5 UDP association header.
type UDPAssociateWrapper struct {
	net.PacketConn
}

// ReadFrom receives a packet and strips the socks5 UDP associate header.
func (w *UDPAssociateWrapper) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	b := make([]byte, len(p)+256) // add some space to parse the header
	n, addr, err = w.PacketConn.ReadFrom(b)
	if err != nil {
		return
	}
	b = b[:n]

	// Validate the header.
	if n <= 6 {
		err = fmt.Errorf("packet size %d is too short to hold UDP associate header", n)
		return
	}
	if b[0] != 0x00 || b[1] != 0x00 {
		err = fmt.Errorf("invalid UDP header")
		return
	}
	if b[2] != 0x00 {
		err = fmt.Errorf("UDP fragment is not supported")
		return
	}

	var destination model.NetAddrSpec
	r := bytes.NewReader(b[3:])
	if err = destination.ReadFromSocks5(r); err != nil {
		return
	}
	if destination.FQDN != "" {
		err = fmt.Errorf("peer used FQDN in UDP associate header, which is unsupported")
		return
	}

	n, err = r.Read(p)
	// Caller may expect the returned address to be *net.UDPAddr.
	addr = &net.UDPAddr{
		IP:   destination.IP,
		Port: destination.Port,
	}
	return
}

// WriteTo adds a socks5 UDP associate header and sends the packet.
func (w *UDPAssociateWrapper) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	var destination model.NetAddrSpec
	if err = destination.From(addr); err != nil {
		return
	}

	b := bytes.NewBuffer([]byte{0, 0, 0})
	destination.WriteToSocks5(b)
	b.Write(p)
	_, err = w.PacketConn.WriteTo(b.Bytes(), destination)
	if err != nil {
		return
	}
	return len(p), nil
}

// NewUDPAssociateWrapper creates a UDPAssociateWrapper.
func NewUDPAssociateWrapper(conn net.PacketConn) *UDPAssociateWrapper {
	return &UDPAssociateWrapper{PacketConn: conn}
}
