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
	"context"
	"fmt"
	"net"

	"github.com/enfein/mieru/v3/pkg/sockopts"
)

// UDPDialer is one implementation of apicommon.PacketDialer interface.
type UDPDialer struct {
	Control sockopts.Control
}

func (d UDPDialer) ListenPacket(ctx context.Context, network, laddr, raddr string) (net.PacketConn, error) {
	switch network {
	case "udp", "udp4", "udp6":
	default:
		return nil, net.UnknownNetworkError(network)
	}

	var localUDPAddr *net.UDPAddr
	if laddr != "" {
		var err error
		localUDPAddr, err = net.ResolveUDPAddr(network, laddr)
		if err != nil {
			return nil, fmt.Errorf("net.ResolveUDPAddr() failed: %w", err)
		}
	}

	conn, err := net.ListenUDP(network, localUDPAddr)
	if err != nil {
		return nil, fmt.Errorf("net.ListenUDP() failed: %w", err)
	}
	if d.Control != nil {
		if err := sockopts.ApplyUDPControl(conn, d.Control); err != nil {
			return nil, fmt.Errorf("ApplyUDPControl() failed: %w", err)
		}
	}
	return conn, nil
}
