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
	"net"

	"github.com/enfein/mieru/pkg/cipher"
	"github.com/enfein/mieru/pkg/netutil"
)

const (
	udpOverhead = cipher.DefaultNonceSize + metadataLength + cipher.DefaultOverhead*2
)

type UDPUnderlay struct {
	conn  *net.UDPConn
	block cipher.BlockCipher
}

var _ Underlay = &UDPUnderlay{}

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

func (u *UDPUnderlay) AddSession(s *Session) error {
	return nil
}

func (u *UDPUnderlay) RemoveSession(sessionID uint32) error {
	return nil
}

func (u *UDPUnderlay) RunEventLoop() error {
	return nil
}

func (u *UDPUnderlay) Close() error {
	return u.conn.Close()
}
