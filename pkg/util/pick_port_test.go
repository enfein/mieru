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
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package util

import (
	"fmt"
	"net"
	"testing"
)

func TestUnusedTCPPort(t *testing.T) {
	port, err := UnusedTCPPort()
	if err != nil {
		t.Fatalf("UnusedTCPPort() failed: %v", err)
	}

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		t.Fatalf("net.Listen() on port %d failed: %v", port, err)
	}
	l.Close()
}

func TestUnusedUDPPort(t *testing.T) {
	port, err := UnusedUDPPort()
	if err != nil {
		t.Fatalf("UnusedUDPPort() failed: %v", err)
	}

	l, err := net.ListenPacket("udp", fmt.Sprintf(":%d", port))
	if err != nil {
		t.Fatalf("net.ListenPacket() on port %d failed: %v", port, err)
	}
	l.Close()
}
