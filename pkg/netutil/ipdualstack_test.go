// Copyright (C) 2022  mieru authors
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

package netutil

import (
	"testing"
)

func TestGetIPVersion(t *testing.T) {
	testcases := []struct {
		input string
		want  IPVersion
	}{
		{"google.com", IPVersionUnknown},
		{"127.0.0.1", IPVersion4},
		{"1234::0", IPVersion6},
	}

	for _, tc := range testcases {
		if out := GetIPVersion(tc.input); out != tc.want {
			t.Errorf("GetIPVersion(%v) = %v, want %v", tc.input, out, tc.want)
		}
	}
}

func TestMaybeDecorateIPv6(t *testing.T) {
	testcases := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"google.com", "google.com"},
		{"google.com:443", "google.com:443"},
		{"0.0.0.0", "0.0.0.0"},
		{"127.0.0.1:53", "127.0.0.1:53"},
		{"::", "[::]"},
		{"::1", "[::1]"},
		{"2001:db8::", "[2001:db8::]"},
	}

	for _, tc := range testcases {
		if out := MaybeDecorateIPv6(tc.input); out != tc.want {
			t.Errorf("MaybeDecorateIPv6(%q) = %q, want %q", tc.input, out, tc.want)
		}
	}
}
