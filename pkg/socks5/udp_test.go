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

package socks5

import (
	"bytes"
	"net"
	"testing"

	"github.com/enfein/mieru/pkg/testtool"
)

func TestUDPAssociateTunnelConn(t *testing.T) {
	in, out := testtool.BufPipe()
	inConn := WrapUDPAssociateTunnel(in)
	outConn := WrapUDPAssociateTunnel(out)

	data := []byte{8, 9, 6, 4}
	n, err := inConn.Write(data)
	if err != nil {
		t.Fatalf("Write() failed: %v", err)
	}
	if n != 4 {
		t.Fatalf("Write() returns %d, want %d", n, 4)
	}

	buf := make([]byte, 16)
	n, err = outConn.Read(buf)
	if err != nil {
		t.Fatalf("Read() failed: %v", err)
	}
	if n != 4 {
		t.Fatalf("Read() returns %d, want %d", n, 4)
	}

	if err = inConn.Close(); err != nil {
		t.Errorf("Close() failed: %v", err)
	}
	if err = outConn.Close(); err != nil {
		t.Errorf("Close() failed: %v", err)
	}
}

func TestUDPAddrToHeader(t *testing.T) {
	testcases := []struct {
		addr   *net.UDPAddr
		header []byte
	}{
		{
			&net.UDPAddr{IP: net.ParseIP("192.168.1.2"), Port: 258},
			[]byte{0, 0, 0, 1, 192, 168, 1, 2, 1, 2},
		},
		{
			&net.UDPAddr{IP: net.ParseIP("::1"), Port: 515},
			[]byte{0, 0, 0, 4, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 2, 3},
		},
	}

	for _, tc := range testcases {
		got := udpAddrToHeader(tc.addr)
		if !bytes.Equal(got, tc.header) {
			t.Errorf("udpAddrToHeader(%v) = %v, want %v", tc.addr, got, tc.header)
		}
	}
}
