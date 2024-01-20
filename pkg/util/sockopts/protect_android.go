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

//go:build android

package sockopts

import "syscall"

// ProtectPath is a UNIX domain socket that Android VPN service is listening to.
// By sending the file descriptor of the connection to the socket via out-of-band
// channel, the connection will be protected by the Android VPN service.
func ProtectPath(protectPath string) Control {
	return func(network, address string, conn syscall.RawConn) error {
		return nil
	}
}

func ProtectPathRaw(protectPath string) RawControl {
	return func(fd uintptr) {}
}
