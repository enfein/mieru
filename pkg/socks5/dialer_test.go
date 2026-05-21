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
	"context"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	apicommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/apis/model"
)

func TestClientDialerConnect(t *testing.T) {
	target := startTCPEchoServer(t)
	proxyAddr := startDatagramModeServer(t, nil)
	dialer := NewClientDialer(proxyAddr, nil, false)

	conn, err := dialer.DialContext(context.Background(), "tcp", target.String())
	if err != nil {
		t.Fatalf("DialContext() failed: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("Write() failed: %v", err)
	}
	out := make([]byte, 4)
	if _, err := io.ReadFull(conn, out); err != nil {
		t.Fatalf("io.ReadFull() failed: %v", err)
	}
	if !bytes.Equal(out, []byte("pong")) {
		t.Fatalf("got %v, want %v", out, []byte("pong"))
	}
}

func TestClientDialerConnectWithAuthentication(t *testing.T) {
	target := startTCPEchoServer(t)
	proxyAddr := startClientDialerTestServer(t, []Credential{{User: "u", Password: "p"}}, nil)
	dialer := NewClientDialer(proxyAddr, &Credential{User: "u", Password: "p"}, false)

	conn, err := dialer.DialContext(context.Background(), "tcp", target.String())
	if err != nil {
		t.Fatalf("DialContext() failed: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("Write() failed: %v", err)
	}
	out := make([]byte, 4)
	if _, err := io.ReadFull(conn, out); err != nil {
		t.Fatalf("io.ReadFull() failed: %v", err)
	}
	if !bytes.Equal(out, []byte("pong")) {
		t.Fatalf("got %v, want %v", out, []byte("pong"))
	}
}

func TestClientDialerUDPAssociate(t *testing.T) {
	target := startUDPEchoServer(t)
	proxyAddr := startDatagramModeServer(t, nil)
	dialer := NewClientDialer(proxyAddr, nil, true)

	conn, err := dialer.ListenPacket(context.Background(), "udp", "", target.String())
	if err != nil {
		t.Fatalf("ListenPacket() failed: %v", err)
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline() failed: %v", err)
	}

	if _, err := conn.WriteTo([]byte("ping"), target); err != nil {
		t.Fatalf("WriteTo() failed: %v", err)
	}
	out := make([]byte, 1024)
	n, addr, err := conn.ReadFrom(out)
	if err != nil {
		t.Fatalf("ReadFrom() failed: %v", err)
	}
	if addr.String() != target.String() {
		t.Fatalf("ReadFrom() addr = %v, want %v", addr, target)
	}
	if !bytes.Equal(out[:n], []byte("pong")) {
		t.Fatalf("got %v, want %v", out[:n], []byte("pong"))
	}
}

func TestClientDialerUDPAssociateDisabled(t *testing.T) {
	target := startUDPEchoServer(t)
	proxyAddr := startDatagramModeServer(t, nil)
	dialer := NewClientDialer(proxyAddr, nil, false)

	if _, err := dialer.ListenPacket(context.Background(), "udp", "", target.String()); err == nil {
		t.Fatal("ListenPacket() error = nil, want error")
	}
}

func TestClientDialerUDPAssociateFQDN(t *testing.T) {
	target := startUDPEchoServer(t)
	const host = "udp-dialer.example"
	resolver := apicommon.HostMapResolver{
		Resolver: datagramFailResolver{},
		Hosts: map[string]net.IP{
			apicommon.NormalizeDomainName(host): net.IPv4(127, 0, 0, 1),
		},
	}
	proxyAddr := startDatagramModeServer(t, resolver)
	dialer := NewClientDialer(proxyAddr, nil, true)
	remoteAddr := &model.NetAddrSpec{
		Net: "udp",
		AddrSpec: model.AddrSpec{
			FQDN: host,
			Port: target.Port,
		},
	}

	conn, err := dialer.ListenPacket(context.Background(), "udp", "", remoteAddr.String())
	if err != nil {
		t.Fatalf("ListenPacket() failed: %v", err)
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline() failed: %v", err)
	}

	if _, err := conn.WriteTo([]byte("ping"), remoteAddr); err != nil {
		t.Fatalf("WriteTo() failed: %v", err)
	}
	out := make([]byte, 1024)
	n, addr, err := conn.ReadFrom(out)
	if err != nil {
		t.Fatalf("ReadFrom() failed: %v", err)
	}
	if addr.String() != remoteAddr.String() {
		t.Fatalf("ReadFrom() addr = %v, want %v", addr, remoteAddr)
	}
	if !bytes.Equal(out[:n], []byte("pong")) {
		t.Fatalf("got %v, want %v", out[:n], []byte("pong"))
	}
}

func startClientDialerTestServer(t *testing.T, credentials []Credential, resolver apicommon.DNSResolver) string {
	t.Helper()
	conf := &Config{
		AllowLoopbackDestination: true,
		AuthOpts: Auth{
			IngressCredentials: credentials,
		},
		Resolver:         resolver,
		UDPAssociateMode: UDPAssociateModeDatagram,
	}
	serv, err := New(conf)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", "0"))
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- serv.Serve(l)
	}()
	t.Cleanup(func() {
		serv.Close()
		l.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Errorf("socks5 server did not stop")
		}
	})
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(l.Addr().(*net.TCPAddr).Port))
}
