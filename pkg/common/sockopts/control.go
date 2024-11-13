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

package sockopts

import (
	"fmt"
	"net"
	"os"
	"syscall"
)

// Control is the Control function used by net.Dialer.
type Control = func(network, address string, c syscall.RawConn) error

// RawControl is the Control function used by syscall.RawConn.
type RawControl = func(fd uintptr)

// RawControlErr returns an error with RawControl.
type RawControlErr = func(fd uintptr) error

// ApplyTCPControls applies all the recommended controls to the TCP listener.
func ApplyTCPControls(listener *net.TCPListener) error {
	rawConn, err := listener.SyscallConn()
	if err != nil {
		return fmt.Errorf("SyscallConn() failed: %w", err)
	}
	if err := rawConn.Control(ReuseAddrPortRaw()); err != nil {
		return err
	}
	if path, found := os.LookupEnv("MIERU_PROTECT_PATH"); found {
		return rawConn.Control(ProtectPathRaw(path))
	}
	return nil
}

// ApplyUDPControls applies all the recommended controls to the UDP connection.
func ApplyUDPControls(conn *net.UDPConn) error {
	rawConn, err := conn.SyscallConn()
	if err != nil {
		return fmt.Errorf("SyscallConn() failed: %w", err)
	}
	if err := rawConn.Control(ReuseAddrPortRaw()); err != nil {
		return err
	}
	if path, found := os.LookupEnv("MIERU_PROTECT_PATH"); found {
		return rawConn.Control(ProtectPathRaw(path))
	}
	return nil
}
