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
// along with this program.  If not, see <https://www.gnu.org/licenses/>

package protocolv2

import (
	"context"
	"fmt"
	"net"

	"github.com/enfein/mieru/pkg/cipher"
	"github.com/enfein/mieru/pkg/netutil"
)

const (
	udpOverhead = cipher.DefaultNonceSize + metadataLength + cipher.DefaultOverhead*2
)

type UDPUnderlay struct {
	baseUnderlay
	conn  *net.UDPConn
	block cipher.BlockCipher
}

var _ Underlay = &UDPUnderlay{}

func (u *UDPUnderlay) String() string {
	if u.conn == nil {
		return "UDPUnderlay{}"
	}
	return fmt.Sprintf("UDPUnderlay{%v - %v}", u.conn.LocalAddr(), u.conn.RemoteAddr())
}

func (u *UDPUnderlay) MTU() int {
	return 1500
}

func (u *UDPUnderlay) IPVersion() netutil.IPVersion {
	if u.conn == nil {
		return netutil.IPVersionUnknown
	}
	return netutil.GetIPVersion(u.conn.LocalAddr().String())
}

func (u *UDPUnderlay) TransportProtocol() netutil.TransportProtocol {
	return netutil.UDPTransport
}

func (u *UDPUnderlay) LocalAddr() net.Addr {
	return u.conn.LocalAddr()
}

func (u *UDPUnderlay) RemoteAddr() net.Addr {
	return u.conn.RemoteAddr()
}

func (u *UDPUnderlay) RunEventLoop(ctx context.Context) error {
	return nil
}

func (u *UDPUnderlay) Close() error {
	u.baseUnderlay.Close()
	return u.conn.Close()
}
