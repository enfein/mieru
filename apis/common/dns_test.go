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
	"errors"
	"net"
	"testing"
)

type testDNSResolver struct {
	lookupIP func(ctx context.Context, network, host string) ([]net.IP, error)
}

func (r testDNSResolver) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	if r.lookupIP == nil {
		return nil, nil
	}
	return r.lookupIP(ctx, network, host)
}

func TestHostMapResolverLookupIP(t *testing.T) {
	ipv4 := net.ParseIP("192.0.2.10")
	ipv6 := net.ParseIP("2001:db8::10")

	testcases := []struct {
		name     string
		resolver HostMapResolver
		network  string
		host     string
		expected []net.IP
		wantErr  error
	}{
		{
			name: "mapped_ipv4_with_normalized_host",
			resolver: HostMapResolver{
				Hosts: map[string]net.IP{
					"example.com": ipv4,
				},
			},
			network:  "",
			host:     "Example.COM.",
			expected: []net.IP{net.ParseIP("192.0.2.10")},
		},
		{
			name: "mapped_ipv4_filtered_by_ip4",
			resolver: HostMapResolver{
				Hosts: map[string]net.IP{
					"example.com": ipv4,
				},
			},
			network:  "ip4",
			host:     "example.com",
			expected: []net.IP{net.ParseIP("192.0.2.10")},
		},
		{
			name: "mapped_ipv4_filtered_out_by_ip6",
			resolver: HostMapResolver{
				Hosts: map[string]net.IP{
					"example.com": ipv4,
				},
			},
			network:  "ip6",
			host:     "example.com",
			expected: nil,
		},
		{
			name: "mapped_ipv6_filtered_by_ip6",
			resolver: HostMapResolver{
				Hosts: map[string]net.IP{
					"example.com": ipv6,
				},
			},
			network:  "ip6",
			host:     "example.com",
			expected: []net.IP{net.ParseIP("2001:db8::10")},
		},
		{
			name: "mapped_ipv6_filtered_out_by_ip4",
			resolver: HostMapResolver{
				Hosts: map[string]net.IP{
					"example.com": ipv6,
				},
			},
			network:  "ip4",
			host:     "example.com",
			expected: nil,
		},
		{
			name: "unknown_network_on_mapped_host",
			resolver: HostMapResolver{
				Hosts: map[string]net.IP{
					"example.com": ipv4,
				},
			},
			network: "tcp",
			host:    "example.com",
			wantErr: net.UnknownNetworkError("tcp"),
		},
		{
			name: "fallback_to_delegate_resolver_on_miss",
			resolver: HostMapResolver{
				Hosts: map[string]net.IP{
					"mapped.example": ipv4,
				},
				Resolver: testDNSResolver{
					lookupIP: func(ctx context.Context, network, host string) ([]net.IP, error) {
						if network != "ip" {
							t.Fatalf("LookupIP() network = %q, want %q", network, "ip")
						}
						if host != "unmapped.example." {
							t.Fatalf("LookupIP() host = %q, want %q", host, "unmapped.example.")
						}
						return []net.IP{net.ParseIP("198.51.100.20")}, nil
					},
				},
			},
			network:  "ip",
			host:     "unmapped.example.",
			expected: []net.IP{net.ParseIP("198.51.100.20")},
		},
		{
			name: "missing_delegate_returns_error",
			resolver: HostMapResolver{
				Hosts: map[string]net.IP{
					"example.com": ipv4,
				},
			},
			network: "ip",
			host:    "missing.example",
			wantErr: errors.New("look up IP address of missing.example is not supported"),
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.resolver.LookupIP(context.Background(), tc.network, tc.host)
			if tc.wantErr != nil {
				if err == nil {
					t.Fatalf("LookupIP() error = nil, want %v", tc.wantErr)
				}
				if err.Error() != tc.wantErr.Error() {
					t.Fatalf("LookupIP() error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LookupIP() error = %v", err)
			}
			if len(got) != len(tc.expected) {
				t.Fatalf("LookupIP() len = %d, want %d", len(got), len(tc.expected))
			}
			for i := range tc.expected {
				if !got[i].Equal(tc.expected[i]) {
					t.Fatalf("LookupIP()[%d] = %v, want %v", i, got[i], tc.expected[i])
				}
			}
		})
	}
}

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
