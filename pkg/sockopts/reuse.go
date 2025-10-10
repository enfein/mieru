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

//go:build !(android || darwin || linux)

package sockopts

import (
	"syscall"
)

// ReuseAddrPort does nothing in unsupported platforms.
func ReuseAddrPort() Control {
	return func(_, _ string, _ syscall.RawConn) error {
		return nil
	}
}

func ReuseAddrPortRaw() RawControl {
	return func(fd uintptr) {}
}

func ReuseAddrPortRawErr() RawControlErr {
	return func(fd uintptr) error { return nil }
}
