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

package constant

// socks5 version number.
const (
	Socks5Version byte = 5
)

// socks5 command types.
const (
	Socks5ConnectCmd      byte = 1
	Socks5BindCmd         byte = 2
	Socks5UDPAssociateCmd byte = 3
)

// socks5 address types.
const (
	Socks5IPv4Address byte = 1
	Socks5FQDNAddress byte = 3
	Socks5IPv6Address byte = 4
)

// socks5 authentication options.
const (
	Socks5NoAuth           byte = 0
	Socks5UserPassAuth     byte = 2
	Socks5NoAcceptableAuth byte = 255

	Socks5UserPassAuthVersion byte = 1

	Socks5AuthSuccess byte = 0
	Socks5AuthFailure byte = 1
)

// socks5 reply values.
const (
	Socks5ReplySuccess              byte = 0
	Socks5ReplyServerFailure        byte = 1
	Socks5ReplyNotAllowedByRuleSet  byte = 2
	Socks5ReplyNetworkUnreachable   byte = 3
	Socks5ReplyHostUnreachable      byte = 4
	Socks5ReplyConnectionRefused    byte = 5
	Socks5ReplyTTLExpired           byte = 6
	Socks5ReplyCommandNotSupported  byte = 7
	Socks5ReplyAddrTypeNotSupported byte = 8
)
