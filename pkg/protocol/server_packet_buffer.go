// Copyright (C) 2026  mieru authors
// SPDX-License-Identifier: GPL-3.0-or-later

package protocol

import (
	"errors"
	"fmt"
	"net"
)

// Shared server listeners receive uploads from many sessions. Leave room for
// short scheduling pauses without changing client sockets or global sysctls.
// These are requests, not reservations: the OS may clamp or reject them.
func configureServerPacketBuffers(conn net.PacketConn) error {
	var result error
	if socket, ok := conn.(interface{ SetReadBuffer(int) error }); ok {
		if err := socket.SetReadBuffer(4 << 20); err != nil {
			result = fmt.Errorf("set UDP receive buffer: %w", err)
		}
	}
	if socket, ok := conn.(interface{ SetWriteBuffer(int) error }); ok {
		if err := socket.SetWriteBuffer(1 << 20); err != nil {
			result = errors.Join(result, fmt.Errorf("set UDP send buffer: %w", err))
		}
	}
	return result
}
