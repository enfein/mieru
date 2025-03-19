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
	"net"
	"testing"

	"github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/apis/model"
	"github.com/enfein/mieru/v3/pkg/testtool"
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
