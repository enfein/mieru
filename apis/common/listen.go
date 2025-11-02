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
	"net"
)

// StreamListenerFactory provides a way to create stream-oriented network listeners.
type StreamListenerFactory interface {
	Listen(ctx context.Context, network, address string) (net.Listener, error)
}

// PacketListenerFactory provides a way to create packet-oriented network listeners.
type PacketListenerFactory interface {
	ListenPacket(ctx context.Context, network, address string) (net.PacketConn, error)
}

var _ StreamListenerFactory = (*net.ListenConfig)(nil)
var _ PacketListenerFactory = (*net.ListenConfig)(nil)
