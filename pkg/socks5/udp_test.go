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
)

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
