// Copyright (C) 2026  mieru authors
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
	"bytes"
	"io"
	"net"
	"testing"

	"github.com/enfein/mieru/v3/apis/constant"
)

// runAuthClient drives the client side of the socks5 authentication handshake
// on conn. It returns the authentication method selected by the server, or 0
// if the server did not send a method selection.
func runAuthClient(conn net.Conn, methods []byte, user, password string) byte {
	req := append([]byte{constant.Socks5Version, byte(len(methods))}, methods...)
	if _, err := conn.Write(req); err != nil {
		return 0
	}

	selection := make([]byte, 2)
	if _, err := io.ReadFull(conn, selection); err != nil {
		return 0
	}
	if selection[1] != constant.Socks5UserPassAuth {
		return selection[1]
	}

	var auth bytes.Buffer
	auth.WriteByte(constant.Socks5UserPassAuthVersion)
	auth.WriteByte(byte(len(user)))
	auth.WriteString(user)
	auth.WriteByte(byte(len(password)))
	auth.WriteString(password)
	if _, err := conn.Write(auth.Bytes()); err != nil {
		return selection[1]
	}
	result := make([]byte, 2)
	if _, err := io.ReadFull(conn, result); err != nil {
		return selection[1]
	}
	return selection[1]
}

// TestHandleAuthenticationCredentialPolicy verifies that the socks5 server
// requires user and password authentication whenever inbound credentials are
// configured, even if the client also advertises the no authentication method.
func TestHandleAuthenticationCredentialPolicy(t *testing.T) {
	cred := Credential{User: "user", Password: "pass"}
	tests := []struct {
		name         string
		ingress      []Credential
		methods      []byte
		user         string
		password     string
		wantErr      bool
		wantSelected byte
	}{
		{
			name:         "credentials required, client offers no auth and user pass",
			ingress:      []Credential{cred},
			methods:      []byte{constant.Socks5NoAuth, constant.Socks5UserPassAuth},
			user:         "user",
			password:     "pass",
			wantErr:      false,
			wantSelected: constant.Socks5UserPassAuth,
		},
		{
			name:         "credentials required, client offers no auth and wrong password",
			ingress:      []Credential{cred},
			methods:      []byte{constant.Socks5NoAuth, constant.Socks5UserPassAuth},
			user:         "user",
			password:     "wrong",
			wantErr:      true,
			wantSelected: constant.Socks5UserPassAuth,
		},
		{
			name:         "credentials required, client only offers no auth",
			ingress:      []Credential{cred},
			methods:      []byte{constant.Socks5NoAuth},
			wantErr:      true,
			wantSelected: 0,
		},
		{
			name:         "credentials not required, client offers both",
			methods:      []byte{constant.Socks5NoAuth, constant.Socks5UserPassAuth},
			wantErr:      false,
			wantSelected: constant.Socks5NoAuth,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := &Server{config: &Config{AuthOpts: Auth{IngressCredentials: tc.ingress}}}
			clientConn, serverConn := net.Pipe()
			selectedCh := make(chan byte, 1)
			go func() {
				selectedCh <- runAuthClient(clientConn, tc.methods, tc.user, tc.password)
			}()

			err := server.handleAuthentication(serverConn)
			clientConn.Close()
			serverConn.Close()
			selected := <-selectedCh

			if (err != nil) != tc.wantErr {
				t.Errorf("handleAuthentication() error = %v, wantErr %v", err, tc.wantErr)
			}
			if selected != tc.wantSelected {
				t.Errorf("selected authentication method = %d, want %d", selected, tc.wantSelected)
			}
		})
	}
}
