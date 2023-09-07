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
	"net"
	"testing"
)

func TestIsNilNetAddr(t *testing.T) {
	testcases := []struct {
		addr net.Addr
		want bool
	}{
		{&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8964}, false},
		{&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8964}, false},
		{NilNetAddr(), true},
	}

	for _, tc := range testcases {
		got := IsNilNetAddr(tc.addr)
		if got != tc.want {
			t.Errorf("IsNilNetAddr(%v) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}
