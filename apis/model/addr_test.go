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

package model

import (
	"bytes"
	"net"
	"reflect"
	"testing"

	"github.com/enfein/mieru/v3/apis/constant"
)

// netAddr implements net.Addr interface.
type netAddr struct {
	Net string
	Str string
}

func (a netAddr) Network() string {
	return a.Net
}

func (a netAddr) String() string {
	return a.Str
}

func TestAddrSpecAddress(t *testing.T) {
	testCases := []struct {
		input    *AddrSpec
		wantAddr string
	}{
		{
			input:    &AddrSpec{IP: net.IP{127, 0, 0, 1}, Port: 8080},
			wantAddr: "127.0.0.1:8080",
		},
		{
			input:    &AddrSpec{IP: net.ParseIP("::1"), Port: 8080},
			wantAddr: "[::1]:8080",
		},
		{
			input:    &AddrSpec{FQDN: "localhost", Port: 8080},
			wantAddr: "localhost:8080",
		},
	}

	for _, tc := range testCases {
		addr := tc.input.String()
		if addr != tc.wantAddr {
			t.Errorf("got %v, want %v", addr, tc.wantAddr)
		}
	}
}

func TestAddrSpecReadWrite(t *testing.T) {
	testCases := []struct {
		input []byte
		addr  *AddrSpec
	}{
		{
			input: []byte{constant.Socks5IPv4Address, 127, 0, 0, 1, 0, 80},
			addr: &AddrSpec{
				IP:   net.IP{127, 0, 0, 1},
				Port: 80,
			},
		},
		{
			input: []byte{constant.Socks5IPv6Address, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 80},
			addr: &AddrSpec{
				IP:   net.ParseIP("::1"),
				Port: 80,
			},
		},
		{
			input: []byte{constant.Socks5FQDNAddress, 9, 'l', 'o', 'c', 'a', 'l', 'h', 'o', 's', 't', 0, 80},
			addr: &AddrSpec{
				FQDN: "localhost",
				Port: 80,
			},
		},
	}

	for _, tc := range testCases {
		addr := &AddrSpec{}
		err := addr.ReadFromSocks5(bytes.NewBuffer(tc.input))
		if err != nil {
			t.Fatalf("ReadFromSocks5() failed: %v", err)
		}
		if addr.FQDN != tc.addr.FQDN {
			t.Errorf("got %v, want %v", addr.FQDN, tc.addr.FQDN)
		}
		if !addr.IP.Equal(tc.addr.IP) {
			t.Errorf("got %v, want %v", addr.IP, tc.addr.IP)
		}
		if addr.Port != tc.addr.Port {
			t.Errorf("got %v, want %v", addr.Port, tc.addr.Port)
		}

		var output bytes.Buffer
		err = addr.WriteToSocks5(&output)
		if err != nil {
			t.Fatalf("WriteToSocks5() failed: %v", err)
		}
		outputBytes := output.Bytes()
		if !bytes.Equal(outputBytes, tc.input) {
			t.Errorf("got %v, want %v", outputBytes, tc.input)
		}
	}
}

func TestNetAddrSpecFrom(t *testing.T) {
	testCases := []struct {
		netAddrSpec NetAddrSpec
		addr        net.Addr
		wantErr     bool
	}{
		{
			netAddrSpec: NetAddrSpec{
				AddrSpec: AddrSpec{
					IP:   net.ParseIP("192.0.2.1"),
					Port: 8080,
				},
				Net: "tcp",
			},
			addr: &net.TCPAddr{
				IP:   net.ParseIP("192.0.2.1"),
				Port: 8080,
			},
		},
		{
			netAddrSpec: NetAddrSpec{
				AddrSpec: AddrSpec{
					IP:   net.ParseIP("2001:db8::1"),
					Port: 8080,
				},
				Net: "udp",
			},
			addr: &net.UDPAddr{
				IP:   net.ParseIP("2001:db8::1"),
				Port: 8080,
			},
		},
		{
			netAddrSpec: NetAddrSpec{
				AddrSpec: AddrSpec{
					FQDN: "example.com",
					Port: 8080,
				},
				Net: "tcp",
			},
			addr: netAddr{
				Net: "tcp",
				Str: "example.com:8080",
			},
		},
		{
			netAddrSpec: NetAddrSpec{},
			addr: &net.TCPAddr{
				Port: 8080,
			},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		n := &NetAddrSpec{}
		err := n.From(tc.addr)
		if (err != nil) != tc.wantErr {
			t.Fatalf("From() error = %v, wantErr %v", err, tc.wantErr)
		}
		if tc.wantErr {
			continue
		}
		if !reflect.DeepEqual(*n, tc.netAddrSpec) {
			t.Errorf("got %v, want %v", n, tc.netAddrSpec)
		}
	}
}
