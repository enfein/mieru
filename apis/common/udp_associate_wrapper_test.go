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
	"strings"
	"testing"

	"github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/apis/model"
	"github.com/enfein/mieru/v3/internal/testtool"
)

func TestUDPAssociateWrapper(t *testing.T) {
	tests := []struct {
		name string
		addr model.AddrSpec
	}{
		{"IPv4", model.AddrSpec{IP: net.IPv4(1, 1, 1, 1), Port: 8964}},
		{"IPv6", model.AddrSpec{IP: net.ParseIP("2001:db8::1"), Port: 8964}},
		{"FQDN", model.AddrSpec{FQDN: "example.com", Port: 8964}},
		{"FQDN255", model.AddrSpec{FQDN: strings.Repeat("a", 255), Port: 8964}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			in, out := testtool.BufPipe()
			inConn := common.NewUDPAssociateWrapper(common.NewPacketOverStreamTunnel(in))
			outConn := common.NewUDPAssociateWrapper(common.NewPacketOverStreamTunnel(out))

			addr := model.NetAddrSpec{Net: "udp", AddrSpec: test.addr}
			data := []byte{8, 9, 6, 4}
			n, err := inConn.WriteTo(data, addr)
			if err != nil {
				t.Fatalf("WriteTo() failed: %v", err)
			}
			if n != len(data) {
				t.Fatalf("WriteTo() returns %d, want %d", n, len(data))
			}

			buf := make([]byte, len(data))
			n, destination, err := outConn.ReadFrom(buf)
			if err != nil {
				t.Fatalf("ReadFrom() failed: %v", err)
			}
			if n != len(data) || !bytes.Equal(buf[:n], data) {
				t.Fatalf("ReadFrom() returns %v, want %v", buf[:n], data)
			}
			if test.addr.FQDN == "" {
				if _, ok := destination.(*net.UDPAddr); !ok {
					t.Errorf("got destination type %T, want *net.UDPAddr", destination)
				}
			} else if _, ok := destination.(model.NetAddrSpec); !ok {
				t.Errorf("got destination type %T, want model.NetAddrSpec", destination)
			}
			if destination.Network() != "udp" {
				t.Errorf("got destination network %s, want %s", destination.Network(), "udp")
			}
			if destination.String() != addr.String() {
				t.Errorf("got destination %s, want %s", destination.String(), addr.String())
			}

			if err = inConn.Close(); err != nil {
				t.Errorf("Close() failed: %v", err)
			}
			if err = outConn.Close(); err != nil {
				t.Errorf("Close() failed: %v", err)
			}
		})
	}
}
