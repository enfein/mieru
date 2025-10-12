// Copyright (C) 2024  mieru authors
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

package model

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"

	"github.com/enfein/mieru/v3/apis/constant"
)

var (
	ErrUnrecognizedAddrType = errors.New("unrecognized address type")
)

// AddrSpec is used to specify an IPv4, IPv6, or a FQDN address
// with a port number.
type AddrSpec struct {
	FQDN string
	IP   net.IP
	Port int
}

// String returns a string that can be used by Dial function.
// It prefers IP address over FQDN.
func (a AddrSpec) String() string {
	if len(a.IP) != 0 {
		return net.JoinHostPort(a.IP.String(), strconv.Itoa(a.Port))
	}
	return net.JoinHostPort(a.FQDN, strconv.Itoa(a.Port))
}

// AddrType returns the socks5 address type.
func (a AddrSpec) AddrType() uint8 {
	if a.IP.To4() != nil {
		return constant.Socks5IPv4Address
	}
	if a.IP.To16() != nil {
		return constant.Socks5IPv6Address
	}
	if a.FQDN != "" {
		return constant.Socks5FQDNAddress
	}
	return 0 // invalid address
}

// ReadFromSocks5 reads the AddrSpec from a socks5 request.
func (a *AddrSpec) ReadFromSocks5(r io.Reader) error {
	// Get the address type.
	addrType := []byte{0}
	if _, err := io.ReadFull(r, addrType); err != nil {
		return err
	}

	// Handle on a per type basis.
	switch addrType[0] {
	case constant.Socks5IPv4Address:
		addr := make([]byte, 4)
		if _, err := io.ReadFull(r, addr); err != nil {
			return err
		}
		a.IP = net.IP(addr)
	case constant.Socks5IPv6Address:
		addr := make([]byte, 16)
		if _, err := io.ReadFull(r, addr); err != nil {
			return err
		}
		a.IP = net.IP(addr)
	case constant.Socks5FQDNAddress:
		addrLen := []byte{0}
		if _, err := io.ReadFull(r, addrLen); err != nil {
			return err
		}
		fqdn := make([]byte, int(addrLen[0]))
		if _, err := io.ReadFull(r, fqdn); err != nil {
			return err
		}
		a.FQDN = string(fqdn)
	default:
		return ErrUnrecognizedAddrType
	}

	// Read the port number.
	port := []byte{0, 0}
	if _, err := io.ReadFull(r, port); err != nil {
		return err
	}
	a.Port = (int(port[0]) << 8) | int(port[1])

	return nil
}

// WriteToSocks5 writes a socks5 request from the AddrSpec.
func (a AddrSpec) WriteToSocks5(w io.Writer) error {
	var addrPort uint16
	var b bytes.Buffer

	switch {
	case a.IP.To4() != nil:
		b.WriteByte(constant.Socks5IPv4Address)
		b.Write(a.IP.To4())
	case a.IP.To16() != nil:
		b.WriteByte(constant.Socks5IPv6Address)
		b.Write(a.IP.To16())
	case a.FQDN != "":
		b.WriteByte(constant.Socks5FQDNAddress)
		b.WriteByte(byte(len(a.FQDN)))
		b.Write([]byte(a.FQDN))
	default:
		return ErrUnrecognizedAddrType
	}
	addrPort = uint16(a.Port)
	b.WriteByte(byte(addrPort >> 8))
	b.WriteByte(byte(addrPort & 0xff))

	_, err := w.Write(b.Bytes())
	return err
}

// NetAddrSpec is a AddrSpec with a network type.
// It implements the net.Addr interface.
type NetAddrSpec struct {
	AddrSpec
	Net string
}

var _ net.Addr = NetAddrSpec{}
var _ net.Addr = &NetAddrSpec{}

// Network returns a network type that can be used by Dial function.
func (n NetAddrSpec) Network() string {
	return n.Net
}

// From sets the NetAddrSpec object with the given network address.
func (n *NetAddrSpec) From(addr net.Addr) error {
	if nas, ok := addr.(NetAddrSpec); ok {
		n.AddrSpec = nas.AddrSpec
		n.Net = nas.Net
		return nil
	}
	if nas, ok := addr.(*NetAddrSpec); ok {
		n.AddrSpec = nas.AddrSpec
		n.Net = nas.Net
		return nil
	}

	n.Net = addr.Network()

	host, portStr, err := net.SplitHostPort(addr.String())
	if err != nil {
		return fmt.Errorf("split host port failed: %w", err)
	}
	if host == "" {
		return fmt.Errorf("host is empty")
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("parse port failed: %w", err)
	}
	n.Port = port

	if ip := net.ParseIP(host); ip != nil {
		n.IP = ip
		n.FQDN = ""
		return nil
	}

	n.IP = nil
	n.FQDN = host
	return nil
}
