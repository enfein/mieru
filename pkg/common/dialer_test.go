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
	"testing"
)

func TestUDPDialerListenPacket(t *testing.T) {
	d := UDPDialer{}
	ctx := context.Background()

	t.Run("Successful listen on udp4", func(t *testing.T) {
		conn, err := d.ListenPacket(ctx, "udp4", "127.0.0.1:0", "")
		if err != nil {
			t.Fatalf("expected no error for udp4, got %v", err)
		}
		if conn == nil {
			t.Fatal("expected non-nil connection for udp4")
		}
		if err := conn.Close(); err != nil {
			t.Fatalf("expected no error on close for udp4, got %v", err)
		}
	})

	t.Run("Successful listen on udp6", func(t *testing.T) {
		conn, err := d.ListenPacket(ctx, "udp6", "[::1]:0", "")
		if err != nil {
			t.Fatalf("expected no error for udp6, got %v", err)
		}
		if conn == nil {
			t.Fatal("expected non-nil connection for udp6")
		}
		if err := conn.Close(); err != nil {
			t.Fatalf("expected no error on close for udp6, got %v", err)
		}
	})

	t.Run("Invalid network type", func(t *testing.T) {
		conn, err := d.ListenPacket(ctx, "tcp", "127.0.0.1:0", "")
		if err == nil {
			t.Fatal("expected error for invalid network type, got nil")
		}
		if conn != nil {
			t.Fatalf("expected nil connection for invalid network type, got %v", conn)
		}
		if _, ok := err.(net.UnknownNetworkError); !ok {
			t.Fatalf("expected UnknownNetworkError, got %T", err)
		}
	})

	t.Run("Invalid local address", func(t *testing.T) {
		conn, err := d.ListenPacket(ctx, "udp", "invalid-address", "")
		if err == nil {
			t.Fatal("expected error for invalid local address, got nil")
		}
		if conn != nil {
			t.Fatalf("expected nil connection for invalid local address, got %v", conn)
		}
	})

	t.Run("Empty laddr, listen on a blank UDPAddr", func(t *testing.T) {
		conn, err := d.ListenPacket(ctx, "udp", "", "")
		if err != nil {
			t.Fatalf("expected no error for empty laddr, got %v", err)
		}
		if conn == nil {
			t.Fatal("expected non-nil connection for empty laddr")
		}
		if err := conn.Close(); err != nil {
			t.Fatalf("expected no error on close for empty laddr, got %v", err)
		}
	})
}
