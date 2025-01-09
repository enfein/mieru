// Copyright (C) 2022  mieru authors
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
	"net"
	"testing"
)

func TestMaybeDecorateIPv6(t *testing.T) {
	testcases := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"google.com", "google.com"},
		{"google.com:443", "google.com:443"},
		{"0.0.0.0", "0.0.0.0"},
		{"127.0.0.1:53", "127.0.0.1:53"},
		{"::", "[::]"},
		{"::1", "[::1]"},
		{"2001:db8::", "[2001:db8::]"},
	}

	for _, tc := range testcases {
		if out := MaybeDecorateIPv6(tc.input); out != tc.want {
			t.Errorf("MaybeDecorateIPv6(%q) = %q, want %q", tc.input, out, tc.want)
		}
	}
}

func TestSelectIPFromList(t *testing.T) {
	testcases := []struct {
		name     string
		ips      []net.IP
		strategy DualStackPreference
		want     net.IP
	}{
		{
			name:     "empty list, prefer ipv4",
			ips:      []net.IP{},
			strategy: PREFER_IPv4,
			want:     nil,
		},
		{
			name:     "ipv4 only, prefer ipv4",
			ips:      []net.IP{net.ParseIP("127.0.0.1")},
			strategy: PREFER_IPv4,
			want:     net.ParseIP("127.0.0.1"),
		},
		{
			name:     "ipv4 only, prefer ipv6",
			ips:      []net.IP{net.ParseIP("127.0.0.1")},
			strategy: PREFER_IPv6,
			want:     net.ParseIP("127.0.0.1"),
		},
		{
			name:     "ipv4 only, only ipv4",
			ips:      []net.IP{net.ParseIP("127.0.0.1")},
			strategy: ONLY_IPv4,
			want:     net.ParseIP("127.0.0.1"),
		},
		{
			name:     "ipv4 only, only ipv6",
			ips:      []net.IP{net.ParseIP("127.0.0.1")},
			strategy: ONLY_IPv6,
			want:     nil,
		},
		{
			name:     "ipv6 only, prefer ipv4",
			ips:      []net.IP{net.ParseIP("::1")},
			strategy: PREFER_IPv4,
			want:     net.ParseIP("::1"),
		},
		{
			name:     "ipv6 only, prefer ipv6",
			ips:      []net.IP{net.ParseIP("::1")},
			strategy: PREFER_IPv6,
			want:     net.ParseIP("::1"),
		},
		{
			name:     "ipv6 only, only ipv4",
			ips:      []net.IP{net.ParseIP("::1")},
			strategy: ONLY_IPv4,
			want:     nil,
		},
		{
			name:     "ipv6 only, only ipv6",
			ips:      []net.IP{net.ParseIP("::1")},
			strategy: ONLY_IPv6,
			want:     net.ParseIP("::1"),
		},
		{
			name:     "ipv4 and ipv6, any IP version",
			ips:      []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("127.0.0.2"), net.ParseIP("::1")},
			strategy: ANY_IP_VERSION,
			want:     net.ParseIP("127.0.0.1"),
		},
		{
			name:     "ipv4 and ipv6, prefer ipv4",
			ips:      []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("127.0.0.2"), net.ParseIP("::1")},
			strategy: PREFER_IPv4,
			want:     net.ParseIP("127.0.0.1"),
		},
		{
			name:     "ipv4 and ipv6, prefer ipv6",
			ips:      []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("127.0.0.2"), net.ParseIP("::1")},
			strategy: PREFER_IPv6,
			want:     net.ParseIP("::1"),
		},
		{
			name:     "ipv4 and ipv6, only ipv4",
			ips:      []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("127.0.0.2"), net.ParseIP("::1")},
			strategy: ONLY_IPv4,
			want:     net.ParseIP("127.0.0.1"),
		},
		{
			name:     "ipv4 and ipv6, only ipv6",
			ips:      []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("127.0.0.2"), net.ParseIP("::1")},
			strategy: ONLY_IPv6,
			want:     net.ParseIP("::1"),
		},
		{
			name:     "ipv6 and ipv4, any IP version",
			ips:      []net.IP{net.ParseIP("::1"), net.ParseIP("::2"), net.ParseIP("127.0.0.1")},
			strategy: ANY_IP_VERSION,
			want:     net.ParseIP("::1"),
		},
		{
			name:     "ipv6 and ipv4, prefer ipv4",
			ips:      []net.IP{net.ParseIP("::1"), net.ParseIP("::2"), net.ParseIP("127.0.0.1")},
			strategy: PREFER_IPv4,
			want:     net.ParseIP("127.0.0.1"),
		},
		{
			name:     "ipv6 and ipv4, prefer ipv6",
			ips:      []net.IP{net.ParseIP("::1"), net.ParseIP("::2"), net.ParseIP("127.0.0.1")},
			strategy: PREFER_IPv6,
			want:     net.ParseIP("::1"),
		},
		{
			name:     "ipv6 and ipv4, only ipv4",
			ips:      []net.IP{net.ParseIP("::1"), net.ParseIP("::2"), net.ParseIP("127.0.0.1")},
			strategy: ONLY_IPv4,
			want:     net.ParseIP("127.0.0.1"),
		},
		{
			name:     "ipv6 and ipv4, only ipv6",
			ips:      []net.IP{net.ParseIP("::1"), net.ParseIP("::2"), net.ParseIP("127.0.0.1")},
			strategy: ONLY_IPv6,
			want:     net.ParseIP("::1"),
		},
	}

	for _, tc := range testcases {
		out := SelectIPFromList(tc.ips, tc.strategy)
		if !out.Equal(tc.want) {
			t.Errorf("%s: SelectIPFromList(%v, %v) = %v, want %v", tc.name, tc.ips, tc.strategy, out, tc.want)
		}
	}
}
