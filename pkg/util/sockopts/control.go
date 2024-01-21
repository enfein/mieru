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

// Control is the Control function used by net.Dialer and net.ListenConfig.
type Control = func(network, address string, c syscall.RawConn) error

// RawControl is the Control function used by syscall.RawConn.
type RawControl = func(fd uintptr)

// RawControlErr returns an error with RawControl.
type RawControlErr = func(fd uintptr) error

// Append returns a Control function that chains next after prev.
func Append(prev, next Control) Control {
	if prev == nil {
		return next
	} else if next == nil {
		return prev
	}
	return func(network, address string, c syscall.RawConn) error {
		if err := prev(network, address, c); err != nil {
			return err
		}
		return next(network, address, c)
	}
}

// ListenConfigWithControls returns a net.ListenConfig with
// all the recommended controls applied.
func ListenConfigWithControls() net.ListenConfig {
	var protectPathControl Control
	if path, found := os.LookupEnv("MIERU_PROTECT_PATH"); found {
		protectPathControl = ProtectPath(path)
	}
	return net.ListenConfig{
		Control: Append(ReuseAddrPort(), protectPathControl),
	}
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
