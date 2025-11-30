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
	"testing"
)

func TestResolveTCPAddr(t *testing.T) {
	resolver := &net.Resolver{PreferGo: true}
	testcases := []struct {
		name     string
		network  string
		address  string
		expected *net.TCPAddr
		wantErr  bool
	}{
		{
			name:     "localhost",
			network:  "tcp4",
			address:  "localhost:80",
			expected: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 80},
			wantErr:  false,
		},
		{
			name:     "ip4_address",
			network:  "tcp",
			address:  "127.0.0.1:80",
			expected: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 80},
			wantErr:  false,
		},
		{
			name:     "ip6_address",
			network:  "tcp",
			address:  "[::1]:80",
			expected: &net.TCPAddr{IP: net.ParseIP("::1"), Port: 80},
			wantErr:  false,
		},
		{
			name:    "invalid_host",
			network: "tcp",
			address: "invalid_host:80",
			wantErr: true,
		},
		{
			name:    "invalid_port",
			network: "tcp",
			address: "127.0.0.1:invalid_port",
			wantErr: true,
		},
		{
			name:    "no_port",
			network: "tcp",
			address: "no_port",
			wantErr: true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			addr, err := ResolveTCPAddr(context.Background(), resolver, tc.network, tc.address)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ResolveTCPAddr() error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.expected != nil {
				if !addr.IP.Equal(tc.expected.IP) {
					t.Errorf("IP mismatch: got %v, want %v", addr.IP, tc.expected.IP)
				}
				if addr.Port != tc.expected.Port {
					t.Errorf("Port mismatch: got %v, want %v", addr.Port, tc.expected.Port)
				}
			}
		})
	}
}

func TestResolveUDPAddr(t *testing.T) {
	resolver := &net.Resolver{PreferGo: true}
	testcases := []struct {
		name     string
		network  string
		address  string
		expected *net.UDPAddr
		wantErr  bool
	}{
		{
			name:     "localhost",
			network:  "udp4",
			address:  "localhost:80",
			expected: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 80},
			wantErr:  false,
		},
		{
			name:     "ip4_address",
			network:  "udp",
			address:  "127.0.0.1:80",
			expected: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 80},
			wantErr:  false,
		},
		{
			name:     "ip6_address",
			network:  "udp",
			address:  "[::1]:80",
			expected: &net.UDPAddr{IP: net.ParseIP("::1"), Port: 80},
			wantErr:  false,
		},
		{
			name:    "invalid_host",
			network: "udp",
			address: "invalid_host:80",
			wantErr: true,
		},
		{
			name:    "invalid_port",
			network: "udp",
			address: "127.0.0.1:invalid_port",
			wantErr: true,
		},
		{
			name:    "no_port",
			network: "udp",
			address: "no_port",
			wantErr: true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			addr, err := ResolveUDPAddr(context.Background(), resolver, tc.network, tc.address)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ResolveUDPAddr() error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.expected != nil {
				if !addr.IP.Equal(tc.expected.IP) {
					t.Errorf("IP mismatch: got %v, want %v", addr.IP, tc.expected.IP)
				}
				if addr.Port != tc.expected.Port {
					t.Errorf("Port mismatch: got %v, want %v", addr.Port, tc.expected.Port)
				}
			}
		})
	}
}
