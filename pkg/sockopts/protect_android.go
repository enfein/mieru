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

import (
	"fmt"
	"syscall"
)

// ProtectPath is a UNIX domain socket that Android VPN service is listening to.
// By sending the file descriptor of the connection to the socket via out-of-band
// channel, the connection will be protected by the Android VPN service.
func ProtectPath(protectPath string) Control {
	return func(_, _ string, conn syscall.RawConn) error {
		var err error
		conn.Control(func(fd uintptr) { err = ProtectPathRawErr(protectPath)(fd) })
		return err
	}
}

func ProtectPathRaw(protectPath string) RawControl {
	return func(fd uintptr) {
		ProtectPathRawErr(protectPath)(fd)
	}
}

func ProtectPathRawErr(protectPath string) RawControlErr {
	return func(fd uintptr) error {
		socket, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
		if err != nil {
			return fmt.Errorf("open protect socket failed: %w", err)
		}
		defer syscall.Close(socket)
		err = syscall.Connect(socket, &syscall.SockaddrUnix{Name: protectPath})
		if err != nil {
			return fmt.Errorf("connect to protect path %q failed: %w", protectPath, err)
		}
		oob := syscall.UnixRights(int(fd))
		dummy := []byte{1}
		err = syscall.Sendmsg(socket, dummy, oob, nil, 0)
		if err != nil {
			return fmt.Errorf("Sendmsg() failed: %w", err)
		}
		n, err := syscall.Read(socket, dummy)
		if err != nil {
			return fmt.Errorf("Read() failed: %w", err)
		}
		if n != 1 {
			return fmt.Errorf("failed to protect file descriptor %d", fd)
		}
		return nil
	}
}
