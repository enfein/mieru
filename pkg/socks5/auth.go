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

import (
	"fmt"
	"io"
	"net"

	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/util"
)

const (
	noAuth           byte = 0
	userPassAuth     byte = 2
	noAcceptableAuth byte = 255

	userPassAuthVersion byte = 1

	authSuccess byte = 0
	authFailure byte = 1
)

// Auth provide authentication settings to socks5 server.
type Auth struct {
	// Do socks5 authentication at proxy client side.
	ClientSideAuthentication bool

	// Credentials to authenticate incoming requests.
	// If empty, username password authentication is not supported.
	IngressCredentials []Credential

	// Credential to dial an outgoing socks5 connection.
	// If nil, username password authentication is not used.
	EgressCredential *Credential
}

// Credential stores socks5 credential for user password authentication.
type Credential struct {
	// User to dial an outgoing socks5 connection.
	User string

	// Password to dial an outgoing socks5 connection.
	Password string
}

func (s *Server) handleAuthentication(conn net.Conn) error {
	// Read the version byte and ensure we are compatible.
	util.SetReadTimeout(conn, s.config.HandshakeTimeout)
	defer util.SetReadTimeout(conn, 0)
	version := []byte{0}
	if _, err := io.ReadFull(conn, version); err != nil {
		HandshakeErrors.Add(1)
		return fmt.Errorf("get socks version failed: %w", err)
	}
	if version[0] != constant.Socks5Version {
		HandshakeErrors.Add(1)
		return fmt.Errorf("unsupported socks version: %v", version)
	}

	// Authenticate the connection.
	nAuthMethods := []byte{0}
	if _, err := io.ReadFull(conn, nAuthMethods); err != nil {
		HandshakeErrors.Add(1)
		return fmt.Errorf("get number of authentication method failed: %w", err)
	}
	if nAuthMethods[0] == 0 {
		HandshakeErrors.Add(1)
		return fmt.Errorf("number of authentication method is 0")
	}

	// Collect authentication methods.
	requestNoAuth := false
	requestUserPassAuth := false
	authMethods := make([]byte, nAuthMethods[0])
	if _, err := io.ReadFull(conn, authMethods); err != nil {
		HandshakeErrors.Add(1)
		return fmt.Errorf("get authentication method failed: %w", err)
	}
	for _, method := range authMethods {
		if method == noAuth {
			requestNoAuth = true
		}
		if method == userPassAuth {
			requestUserPassAuth = true
		}
	}

	if !requestNoAuth && !requestUserPassAuth {
		HandshakeErrors.Add(1)
		if _, err := conn.Write([]byte{constant.Socks5Version, noAcceptableAuth}); err != nil {
			return fmt.Errorf("write authentication response (no acceptable methods) failed: %w", err)
		}
		return fmt.Errorf("socks5 client provided authentication is not supported by socks5 server")
	}
	if requestNoAuth {
		// Handle no authentication. This has higher priority than user password authentication.
		if !requestUserPassAuth && len(s.config.AuthOpts.IngressCredentials) > 0 {
			HandshakeErrors.Add(1)
			return fmt.Errorf("socks5 client requested no authentication, but user and password are required by socks5 server")
		}
		if _, err := conn.Write([]byte{constant.Socks5Version, noAuth}); err != nil {
			HandshakeErrors.Add(1)
			return fmt.Errorf("write authentication response (no authentication required) failed: %w", err)
		}
	} else if requestUserPassAuth {
		// Handle user password authentication.
		if len(s.config.AuthOpts.IngressCredentials) == 0 {
			HandshakeErrors.Add(1)
			return fmt.Errorf("there is no registered socks5 server user")
		}

		// Tell the client to use user password authentication.
		if _, err := conn.Write([]byte{constant.Socks5Version, userPassAuth}); err != nil {
			HandshakeErrors.Add(1)
			return fmt.Errorf("write user password authentication request failed: %w", err)
		}

		// Get the authentication version.
		header := []byte{0}
		if _, err := io.ReadFull(conn, header); err != nil {
			return fmt.Errorf("get user password authentication version failed: %w", err)
		}
		if header[0] != userPassAuthVersion {
			return fmt.Errorf("user password authentication version %d is not supported by socks5 server", header[0])
		}

		// Get user.
		if _, err := io.ReadFull(conn, header); err != nil {
			return fmt.Errorf("get user length failed: %w", err)
		}
		user := make([]byte, header[0])
		if _, err := io.ReadFull(conn, user); err != nil {
			return fmt.Errorf("read user failed: %w", err)
		}

		// Get password.
		if _, err := io.ReadFull(conn, header); err != nil {
			return fmt.Errorf("get password length failed: %w", err)
		}
		password := make([]byte, header[0])
		if _, err := io.ReadFull(conn, password); err != nil {
			return fmt.Errorf("read password failed: %w", err)
		}

		// Verify user and password.
		userStr := string(user)
		passwordStr := string(password)
		for _, c := range s.config.AuthOpts.IngressCredentials {
			if c.User == userStr && c.Password == passwordStr {
				if _, err := conn.Write([]byte{userPassAuthVersion, authSuccess}); err != nil {
					HandshakeErrors.Add(1)
					return fmt.Errorf("write user password authentication success response failed: %w", err)
				}
				return nil
			}
		}
		HandshakeErrors.Add(1)
		if _, err := conn.Write([]byte{userPassAuthVersion, authFailure}); err != nil {
			return fmt.Errorf("write user password authentication failure response failed: %w", err)
		}
		return fmt.Errorf("user password authentication failed: invalid user or password")
	}
	return nil
}

// dialWithAuthentication dials to another socks5 server with given credential.
// The proxy connection is closed if there is any error.
func (s *Server) dialWithAuthentication(proxyConn net.Conn, auth *appctlpb.Auth) error {
	util.SetReadTimeout(proxyConn, s.config.HandshakeTimeout)
	defer util.SetReadTimeout(proxyConn, 0)

	if auth == nil || auth.GetUser() == "" || auth.GetPassword() == "" {
		// No authentication required.
		if _, err := proxyConn.Write([]byte{constant.Socks5Version, 1, noAuth}); err != nil {
			HandshakeErrors.Add(1)
			proxyConn.Close()
			return fmt.Errorf("failed to write socks5 authentication header to egress proxy: %w", err)
		}

		resp := []byte{0, 0}
		if _, err := io.ReadFull(proxyConn, resp); err != nil {
			HandshakeErrors.Add(1)
			proxyConn.Close()
			return fmt.Errorf("failed to read socks5 authentication response from egress proxy: %w", err)
		}
		if resp[0] != constant.Socks5Version || resp[1] != noAuth {
			HandshakeErrors.Add(1)
			proxyConn.Close()
			return fmt.Errorf("got unexpected socks5 authentication response from egress proxy: %v", resp)
		}
	} else {
		// User password authentication.
		if _, err := proxyConn.Write([]byte{constant.Socks5Version, 1, userPassAuth}); err != nil {
			HandshakeErrors.Add(1)
			proxyConn.Close()
			return fmt.Errorf("failed to write socks5 authentication header to egress proxy: %w", err)
		}

		resp := []byte{0, 0}
		if _, err := io.ReadFull(proxyConn, resp); err != nil {
			HandshakeErrors.Add(1)
			proxyConn.Close()
			return fmt.Errorf("failed to read socks5 authentication response from egress proxy: %w", err)
		}
		if resp[0] != constant.Socks5Version || resp[1] != userPassAuth {
			HandshakeErrors.Add(1)
			proxyConn.Close()
			return fmt.Errorf("got unexpected socks5 authentication response from egress proxy: %v", resp)
		}

		// Send socks5 credential.
		credential := []byte{userPassAuthVersion}
		credential = append(credential, byte(len(auth.GetUser())))
		credential = append(credential, []byte(auth.GetUser())...)
		credential = append(credential, byte(len(auth.GetPassword())))
		credential = append(credential, []byte(auth.GetPassword())...)
		if _, err := proxyConn.Write(credential); err != nil {
			HandshakeErrors.Add(1)
			proxyConn.Close()
			return fmt.Errorf("failed to write socks5 authentication credential to egress proxy: %w", err)
		}

		if _, err := io.ReadFull(proxyConn, resp); err != nil {
			HandshakeErrors.Add(1)
			proxyConn.Close()
			return fmt.Errorf("failed to read socks5 authentication response from egress proxy: %w", err)
		}
		if resp[0] != userPassAuthVersion {
			HandshakeErrors.Add(1)
			proxyConn.Close()
			return fmt.Errorf("got unexpected socks5 user password authentication version from egress proxy: %v", resp[0])
		}
		if resp[1] != authSuccess {
			HandshakeErrors.Add(1)
			proxyConn.Close()
			return fmt.Errorf("socks5 authentication with user password failed from egress proxy")
		}
	}
	return nil
}
