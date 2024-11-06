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
	"strconv"
)

// DNSResolver provides a way to look up IP addresses from host name.
type DNSResolver interface {
	LookupIP(ctx context.Context, network, host string) ([]net.IP, error)
}

// Standard library Resolver implements the DNSResolver interface.
var _ DNSResolver = &net.Resolver{}

// ResolveTCPAddr returns a TCP address using the DNSResolver.
func ResolveTCPAddr(r DNSResolver, network, address string) (*net.TCPAddr, error) {
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

	ips, err := r.LookupIP(context.Background(), dnsQueryNetwork, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, &net.AddrError{Err: "IP address not found", Addr: address}
	}

	return &net.TCPAddr{IP: ips[0], Port: port}, nil
}

// ResolveUDPAddr returns a UDP address using the DNSResolver.
func ResolveUDPAddr(r DNSResolver, network, address string) (*net.UDPAddr, error) {
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

	ips, err := r.LookupIP(context.Background(), dnsQueryNetwork, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, &net.AddrError{Err: "IP address not found", Addr: address}
	}

	return &net.UDPAddr{IP: ips[0], Port: port}, nil
}
