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
	"fmt"
	"net"
	"strconv"
)

// DNSResolver provides a way to look up IP addresses from host name.
type DNSResolver interface {
	LookupIP(ctx context.Context, network, host string) ([]net.IP, error)
}

// Standard library net.Resolver implements the DNSResolver interface.
var _ DNSResolver = (*net.Resolver)(nil)

// NilDNSResolver implements the DNSResolver interface but
// it is not able to look up IP addresses.
type NilDNSResolver struct{}

func (r NilDNSResolver) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	return nil, fmt.Errorf("look up IP address of %s is not supported", host)
}

var _ DNSResolver = NilDNSResolver{}

// ClientDNSConfig provides DNS configurations used by proxy client.
type ClientDNSConfig struct {
	// If enabled, when creating a stream oriented network connection,
	// Resolver is not used. The proxy server endpoint is passed to Dialer as is.
	//
	// You may enable this when Dialer has an internal mechanism to resolve DNS.
	BypassDialerDNS bool
}

// ResolveTCPAddr returns a TCP address using the DNSResolver.
func ResolveTCPAddr(ctx context.Context, r DNSResolver, network, address string) (*net.TCPAddr, error) {
	dnsQueryNetwork := "ip"
	switch network {
	case "tcp":
	case "tcp4":
		dnsQueryNetwork = "ip4"
	case "tcp6":
		dnsQueryNetwork = "ip6"
	case "":
		network = "tcp"
	default:
		return nil, net.UnknownNetworkError(network)
	}

	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); ip != nil {
		return &net.TCPAddr{IP: ip, Port: port}, nil
	}

	ips, err := r.LookupIP(ctx, dnsQueryNetwork, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, &net.AddrError{Err: "IP address not found", Addr: address}
	}

	return &net.TCPAddr{IP: ips[0], Port: port}, nil
}

// ResolveUDPAddr returns a UDP address using the DNSResolver.
func ResolveUDPAddr(ctx context.Context, r DNSResolver, network, address string) (*net.UDPAddr, error) {
	dnsQueryNetwork := "ip"
	switch network {
	case "udp":
	case "udp4":
		dnsQueryNetwork = "ip4"
	case "udp6":
		dnsQueryNetwork = "ip6"
	case "":
		network = "udp"
	default:
		return nil, net.UnknownNetworkError(network)
	}

	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); ip != nil {
		return &net.UDPAddr{IP: ip, Port: port}, nil
	}

	ips, err := r.LookupIP(ctx, dnsQueryNetwork, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, &net.AddrError{Err: "IP address not found", Addr: address}
	}

	return &net.UDPAddr{IP: ips[0], Port: port}, nil
}

// ForbidDefaultResolver causes the process to panic if
// net.DefaultResolver object is used.
func ForbidDefaultResolver() {
	net.DefaultResolver.PreferGo = true
	net.DefaultResolver.Dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		panic("Using net.DefaultResolver is forbidden")
	}
}
