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
	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/netutil"
)

const (
	udpOverhead = cipher.DefaultNonceSize + metadataLength + cipher.DefaultOverhead*2
)

type UDPUnderlay struct {
	baseUnderlay
	conn       *net.UDPConn
	remoteAddr *net.UDPAddr
	block      cipher.BlockCipher
}

var _ Underlay = &UDPUnderlay{}

// NewUDPUnderlay connects to the remote address "raddr" on the network "udp"
// with packet encryption. If "laddr" is empty, an automatic address is used.
// "block" is the block encryption algorithm to encrypt packets.
func NewUDPUnderlay(ctx context.Context, network, laddr, raddr string, mtu int, block cipher.BlockCipher) (*UDPUnderlay, error) {
	switch network {
	case "udp", "udp4", "udp6":
	default:
		return nil, fmt.Errorf("network %s is not supported for UDP underlay", network)
	}
	if !block.IsStateless() {
		return nil, fmt.Errorf("UDP block cipher must be stateless")
	}
	remoteAddr, err := net.ResolveUDPAddr("udp", raddr)
	if err != nil {
		return nil, fmt.Errorf("net.ResolveUDPAddr() failed: %w", err)
	}
	var localAddr *net.UDPAddr
	if laddr != "" {
		localAddr, err = net.ResolveUDPAddr("udp", laddr)
		if err != nil {
			return nil, fmt.Errorf("net.ResolveUDPAddr() failed: %w", err)
		}
	}

	conn, err := net.ListenUDP(network, localAddr)
	if err != nil {
		return nil, fmt.Errorf("net.ListenUDP() failed: %w", err)
	}
	log.Debugf("Created new client UDP underlay [%v - %v]", conn.LocalAddr(), remoteAddr)
	return &UDPUnderlay{
		baseUnderlay: *newBaseUnderlay(true, mtu),
		conn:         conn,
		remoteAddr:   remoteAddr,
		block:        block,
	}, nil
}

func (u *UDPUnderlay) String() string {
	if u.conn == nil {
		return "UDPUnderlay{}"
	}
	return fmt.Sprintf("UDPUnderlay{%v - %v}", u.conn.LocalAddr(), u.conn.RemoteAddr())
}

func (u *UDPUnderlay) Close() error {
	select {
	case <-u.done:
		return nil
	default:
	}

	log.Debugf("Closing %v", u)
	u.baseUnderlay.Close()
	return u.conn.Close()
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

func (u *UDPUnderlay) AddSession(s *Session) error {
	if err := u.baseUnderlay.AddSession(s); err != nil {
		return err
	}
	s.conn = u // override base underlay
	close(s.ready)
	log.Debugf("Adding session %d to %v", s.id, u)

	s.wg.Add(2)
	go func() {
		if err := s.runInputLoop(context.Background()); err != nil {
			log.Debugf("%v runInputLoop(): %v", s, err)
		}
		s.wg.Done()
	}()
	go func() {
		if err := s.runOutputLoop(context.Background()); err != nil {
			log.Debugf("%v runOutputLoop(): %v", s, err)
		}
		s.wg.Done()
	}()
	return nil
}

func (u *UDPUnderlay) RemoveSession(s *Session) error {
	err := u.baseUnderlay.RemoveSession(s)
	if len(u.baseUnderlay.sessionMap) == 0 {
		u.Close()
	}
	return err
}

func (u *UDPUnderlay) RunEventLoop(ctx context.Context) error {
	return nil
}
