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

package common_test

import (
	"bytes"
	"net"
	"testing"

	"github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/apis/model"
	"github.com/enfein/mieru/v3/internal/testtool"
)

func TestUDPAssociateWrapper(t *testing.T) {
	in, out := testtool.BufPipe()
	inConn := common.NewUDPAssociateWrapper(common.NewPacketOverStreamTunnel(in))
	outConn := common.NewUDPAssociateWrapper(common.NewPacketOverStreamTunnel(out))

	addr := model.NetAddrSpec{Net: "udp", AddrSpec: model.AddrSpec{IP: net.IPv4(1, 1, 1, 1), Port: 8964}}
	data := []byte{8, 9, 6, 4}
	n, err := inConn.WriteTo(data, addr)
	if err != nil {
		t.Fatalf("Write() failed: %v", err)
	}
	if n != 4 {
		t.Fatalf("Write() returns %d, want %d", n, 4)
	}

	buf := make([]byte, 16)
	var addr2 net.Addr
	n, addr2, err = outConn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("Read() failed: %v", err)
	}
	if n != 4 {
		t.Fatalf("Read() returns %d, want %d", n, 4)
	}
	destination := addr2.(*net.UDPAddr)
	if destination.Network() != "udp" {
		t.Errorf("got destination network %s, want %s", destination.Network(), "udp")
	}
	if destination.String() != "1.1.1.1:8964" {
		t.Errorf("got destination %s, want %s", destination.String(), "1.1.1.1:8964")
	}

	if err = inConn.Close(); err != nil {
		t.Errorf("Close() failed: %v", err)
	}
	if err = outConn.Close(); err != nil {
		t.Errorf("Close() failed: %v", err)
	}
}

func TestUDPAssociateWrapperEmptyPayload(t *testing.T) {
	for _, host := range []string{"1.1.1.1", "::1"} {
		t.Run(host, func(t *testing.T) {
			in, out := testtool.BufPipe()
			inConn := common.NewUDPAssociateWrapper(common.NewPacketOverStreamTunnel(in))
			outConn := common.NewUDPAssociateWrapper(common.NewPacketOverStreamTunnel(out))
			defer inConn.Close()
			defer outConn.Close()

			addr := &net.UDPAddr{IP: net.ParseIP(host), Port: 8964}

			// A nonempty packet must still be readable after empty packets.
			for _, payload := range [][]byte{nil, {}, []byte("ping")} {
				n, err := inConn.WriteTo(payload, addr)
				if err != nil {
					t.Fatalf("WriteTo() failed: %v", err)
				}
				if n != len(payload) {
					t.Fatalf("WriteTo() returned %d, want %d", n, len(payload))
				}

				buf := make([]byte, 16)
				n, gotAddr, err := outConn.ReadFrom(buf)
				if err != nil {
					t.Fatalf("ReadFrom() failed for payload %v: %v", payload, err)
				}
				if n != len(payload) || !bytes.Equal(buf[:n], payload) {
					t.Fatalf("ReadFrom() payload = %v, want %v", buf[:n], payload)
				}
				gotUDPAddr, ok := gotAddr.(*net.UDPAddr)
				if !ok || !gotUDPAddr.IP.Equal(addr.IP) || gotUDPAddr.Port != addr.Port {
					t.Fatalf("ReadFrom() address = %v, want %v", gotAddr, addr)
				}
			}
		})
	}
}

func TestUDPAssociateWrapperOptions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		host    string
		options []common.UDPAssociateWrapperOptions
		wantErr bool
	}{
		{name: "FQDN rejected by default", host: "example.com", wantErr: true},
		{name: "FQDN enabled", host: "example.com", options: []common.UDPAssociateWrapperOptions{common.UDPAssociateReadFromFQDN{}}},
		{name: "repeated options", host: "example.com", options: []common.UDPAssociateWrapperOptions{common.UDPAssociateReadFromFQDN{}, common.UDPAssociateReadFromFQDN{}}},
		{name: "IPv4 default", host: "1.1.1.1"},
		{name: "IPv4 with option", host: "1.1.1.1", options: []common.UDPAssociateWrapperOptions{common.UDPAssociateReadFromFQDN{}}},
		{name: "IPv6 default", host: "::1"},
		{name: "IPv6 with option", host: "::1", options: []common.UDPAssociateWrapperOptions{common.UDPAssociateReadFromFQDN{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, out := testtool.BufPipe()
			inConn := common.NewUDPAssociateWrapper(common.NewPacketOverStreamTunnel(in))
			outConn := common.NewUDPAssociateWrapper(common.NewPacketOverStreamTunnel(out), tc.options...)
			defer inConn.Close()
			defer outConn.Close()

			destination := model.NetAddrSpec{Net: "udp"}
			if err := destination.AddrSpec.From(net.JoinHostPort(tc.host, "8964")); err != nil {
				t.Fatal(err)
			}
			for _, payload := range [][]byte{nil, []byte("ping")} {
				if _, err := inConn.WriteTo(payload, destination); err != nil {
					t.Fatalf("WriteTo() failed: %v", err)
				}
				buf := make([]byte, 16)
				n, addr, err := outConn.ReadFrom(buf)
				if (err != nil) != tc.wantErr {
					t.Fatalf("ReadFrom() error = %v, wantErr %v", err, tc.wantErr)
				}
				if tc.wantErr {
					continue
				}
				if n != len(payload) || !bytes.Equal(buf[:n], payload) {
					t.Fatalf("ReadFrom() payload = %v, want %v", buf[:n], payload)
				}
				if destination.FQDN != "" {
					got, ok := addr.(model.NetAddrSpec)
					if !ok || got.FQDN != destination.FQDN || got.IP != nil || got.Port != destination.Port || got.Net != "udp" {
						t.Errorf("ReadFrom() address = %#v, want %#v", addr, destination)
					}
				} else {
					got, ok := addr.(*net.UDPAddr)
					if !ok || !got.IP.Equal(destination.IP) || got.Port != destination.Port {
						t.Errorf("ReadFrom() address = %#v, want %v", addr, destination)
					}
				}
			}
		})
	}
}
