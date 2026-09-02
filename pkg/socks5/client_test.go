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
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/testtool"
)

func TestParseProxyURI(t *testing.T) {
	t.Parallel()
	testcases := []struct {
		name string
		uri  string
		c    Client
	}{
		{
			name: "full config",
			uri:  "socks5://u1:p1@127.0.0.1:8080?timeout=1s",
			c: Client{
				Credential: &Credential{
					User:     "u1",
					Password: "p1",
				},
				Host:    "127.0.0.1:8080",
				Timeout: 1 * time.Second,
			},
		},
		{
			name: "simple socks5",
			uri:  "socks5://127.0.0.1:8080",
			c: Client{
				Host: "127.0.0.1:8080",
			},
		},
	}
	for _, tc := range testcases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, err := parseProxyURI(tc.uri)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(c, &tc.c) {
				t.Fatalf("expect %v got %v", tc.c, c)
			}
		})
	}
}

func TestNewClientDialerFromURI(t *testing.T) {
	dialer, err := NewClientDialerFromURI("socks5://u1:p1@127.0.0.1:8080?timeout=2s", true)
	if err != nil {
		t.Fatalf("NewClientDialerFromURI() failed: %v", err)
	}
	if dialer.ProxyAddress != "127.0.0.1:8080" {
		t.Errorf("ProxyAddress = %q, want %q", dialer.ProxyAddress, "127.0.0.1:8080")
	}
	if dialer.Credential == nil || dialer.Credential.User != "u1" || dialer.Credential.Password != "p1" {
		t.Errorf("Credential = %v, want user u1 and password p1", dialer.Credential)
	}
	if dialer.Timeout != 2*time.Second {
		t.Errorf("Timeout = %v, want %v", dialer.Timeout, 2*time.Second)
	}
	if !dialer.Socks5UDPAssociate {
		t.Error("Socks5UDPAssociate = false, want true")
	}
}

func TestDialRejectsUnsupportedCommands(t *testing.T) {
	for _, cmd := range []byte{constant.Socks5BindCmd, constant.Socks5UDPAssociateCmd} {
		dial := Dial("socks5://127.0.0.1:1080", cmd)
		if _, err := dial("tcp", "example.com:80"); err == nil {
			t.Errorf("Dial() command %d error = nil, want unsupported command error", cmd)
		}
	}
}

func TestDialSocks5ProxyRejectsBind(t *testing.T) {
	dial := DialSocks5Proxy(&Client{
		Host:    "127.0.0.1:1080",
		CmdType: constant.Socks5BindCmd,
	})
	if _, _, _, err := dial("tcp", "example.com:80"); err == nil {
		t.Error("DialSocks5Proxy() BIND error = nil, want unsupported command error")
	}
}

func TestDialPreservesURIHandshakeTimeout(t *testing.T) {
	proxyAddr := startUnresponsiveSocks5Proxy(t)
	dial := Dial(fmt.Sprintf("socks5://%s?timeout=100ms", proxyAddr), constant.Socks5ConnectCmd)

	done := make(chan error, 1)
	go func() {
		_, err := dial("tcp", "example.com:80")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Dial() error = nil, want timeout error")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Dial() did not honor the URI handshake timeout")
	}
}

func TestDialSocks5ProxyPreservesHandshakeTimeout(t *testing.T) {
	proxyAddr := startUnresponsiveSocks5Proxy(t)
	dial := DialSocks5Proxy(&Client{
		Host:    proxyAddr,
		Timeout: 100 * time.Millisecond,
		CmdType: constant.Socks5ConnectCmd,
	})

	done := make(chan error, 1)
	go func() {
		_, _, _, err := dial("tcp", "example.com:80")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("DialSocks5Proxy() error = nil, want timeout error")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("DialSocks5Proxy() did not honor the handshake timeout")
	}
}

func TestClientDialerHTTPTimeoutCancelsHandshake(t *testing.T) {
	proxyAddr := startUnresponsiveSocks5Proxy(t)
	dialer := NewClientDialer(proxyAddr, nil, false)
	client := &http.Client{
		Transport: &http.Transport{DialContext: dialer.DialContext},
		Timeout:   100 * time.Millisecond,
	}
	defer client.CloseIdleConnections()

	done := make(chan error, 1)
	go func() {
		_, err := client.Get("http://example.com")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("http.Client.Get() error = nil, want timeout error")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("http.Client timeout did not cancel the socks5 handshake")
	}
}

func startUnresponsiveSocks5Proxy(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		<-stop
	}()
	t.Cleanup(func() {
		close(stop)
		l.Close()
		<-done
	})
	return l.Addr().String()
}

func newTestSocksServer(port int) {
	conf := &Config{
		AllowLoopbackDestination: true,
	}
	srv, err := New(conf)
	if err != nil {
		panic(err)
	}

	started := make(chan struct{})
	go func() {
		l, err := net.Listen("tcp", "0.0.0.0:"+strconv.Itoa(port))
		if err != nil {
			panic(err)
		}
		close(started)
		if err := srv.Serve(l); err != nil {
			panic(err)
		}
	}()

	runtime.Gosched()
	<-started
	runtime.Gosched()
	testtool.WaitForTCPReady(port, 5*time.Second)
}

func TestSocks5Anonymous(t *testing.T) {
	httpTestServer := testtool.NewTestHTTPServer([]byte("hello"))

	port, err := common.UnusedTCPPort()
	if err != nil {
		t.Fatalf("common.UnusedTCPPort() failed: %v", err)
	}
	newTestSocksServer(port)

	dialSocksProxy := Dial(fmt.Sprintf("socks5://127.0.0.1:%d?timeout=5s", port), constant.Socks5ConnectCmd)
	tr := &http.Transport{Dial: dialSocksProxy}
	httpClient := &http.Client{Transport: tr}
	resp, err := httpClient.Get(fmt.Sprintf("http://localhost" + httpTestServer.Addr))
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	if string(respBody) != "hello" {
		t.Fatalf("expect response hello but got %s", respBody)
	}
}
