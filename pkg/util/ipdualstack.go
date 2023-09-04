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

package util

import (
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
)

type IPVersion int

const (
	IPVersionUnknown IPVersion = iota
	IPVersion4
	IPVersion6
)

func (v IPVersion) String() string {
	switch v {
	case IPVersionUnknown:
		return "UNKNOWN"
	case IPVersion4:
		return "IPV4"
	case IPVersion6:
		return "IPV6"
	default:
		return "UNSPECIFIED"
	}
}

// IsIPDualStack returns true if an IPv6 socket is able to send and receive
// both IPv4 and IPv6 packets.
//
// This function only supports Linux. It always returns false if running other
// operating systems.
func IsIPDualStack() bool {
	if runtime.GOOS == "linux" {
		v, err := os.ReadFile("/proc/sys/net/ipv6/bindv6only")
		if err != nil {
			return false
		}
		s := string(v)
		s = strings.Trim(s, "\n")
		i, err := strconv.Atoi(s)
		if err != nil {
			return false
		}
		if i == 0 {
			return true
		}
	}
	return false
}

// AllIPAddr returns a catch-all IP address to bind. If the machine supports
// IP dual stack, "::" is returned. Otherwise "0.0.0.0" is returned.
func AllIPAddr() string {
	if IsIPDualStack() {
		return "::"
	}
	return "0.0.0.0"
}

// LocalIPAddr returns the localhost IP address.
func LocalIPAddr() string {
	// If IP dual stack is supported, bind to "::1" will also bind to
	// "127.0.0.1". This may cause an error if the program is running
	// inside a container. Generally, we believe "127.0.0.1" is available
	// on every machine, so just use this.
	return "127.0.0.1"
}

// GetIPVersion returns the IP version of the given network address.
func GetIPVersion(addr string) IPVersion {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// Assume there is no port.
		host = addr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return IPVersionUnknown
	}
	ip4 := ip.To4()
	if ip4 != nil {
		return IPVersion4
	}
	return IPVersion6
}

// MaybeDecorateIPv6 adds [ and ] before and after an IPv6 address. If the
// input string is a IPv4 address or not a valid IP address (e.g. is a domain),
// the same string is returned.
func MaybeDecorateIPv6(addr string) string {
	if GetIPVersion(addr) == IPVersion6 {
		return "[" + addr + "]"
	}
	return addr
}
