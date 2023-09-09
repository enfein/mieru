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
	"context"
	"fmt"
	"net"
)

type DNSPolicy uint8

const (
	DNSPolicyDefault DNSPolicy = iota
	DNSPolicyIPv4Only
	DNSPolicyIPv6Only
)

func (p DNSPolicy) String() string {
	switch p {
	case DNSPolicyDefault:
		return "DEFAULT"
	case DNSPolicyIPv4Only:
		return "IPv4_ONLY"
	case DNSPolicyIPv6Only:
		return "IPv6_ONLY"
	default:
		return "UNSPECIFIED"
	}
}

// DNSResolver uses Golang's default DNS implementation to resolve host names.
type DNSResolver struct {
	DNSPolicy DNSPolicy
}

// LookupIP looks up host for the given network using the DNS resolver.
func (d *DNSResolver) LookupIP(ctx context.Context, host string) (net.IP, error) {
	network := "ip"
	switch d.DNSPolicy {
	case DNSPolicyIPv4Only:
		network = "ip4"
	case DNSPolicyIPv6Only:
		network = "ip6"
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, network, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("lookup IP from %s returned no result", host)
	}
	return ips[0], nil
}
