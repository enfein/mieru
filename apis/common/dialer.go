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

package common

import (
	"context"
	"net"
)

// Dial provides methods to create stream oriented connections.
type Dialer interface {
	// It is recommended to use IP and port in address string.
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

var _ Dialer = (*net.Dialer)(nil)

// PacketDialer provides methods to create packet oriented connections.
type PacketDialer interface {
	// It is recommended to use IP and port in laddr and raddr string.
	// If laddr is an empty string, it will listen to a random port.
	ListenPacket(ctx context.Context, network, laddr, raddr string) (net.PacketConn, error)
}
