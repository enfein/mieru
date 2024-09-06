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

package socks5

const (
	noAuth              byte = 0
	userPassAuth        byte = 2
	userPassAuthVersion byte = 1
	authSuccess         byte = 0
	authFailure         byte = 1
)

// Auth provide authentication settings to socks5 server.
type Auth struct {
	// Do socks5 authentication at proxy client side.
	ClientSideAuthentication bool

	// Credentials to authenticate incoming requests.
	// If empty, username password authentication is not supported.
	IngressCredentials map[string]string

	// Credential to dial an outgoing socks5 connection.
	// If nil, username password authentication is not used.
	EgressCredential *DialCredential
}

// DialCredential stores socks5 credential for user password authentication.
type DialCredential struct {
	// User to dial an outgoing socks5 connection.
	User string

	// Password to dial an outgoing socks5 connection.
	Password string
}
