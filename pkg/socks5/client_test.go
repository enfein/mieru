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
			uri:  "socks5://u1:p1@127.0.0.1:8080?timeout=2s",
			c: Client{
				Credential: &Credential{
					User:     "u1",
					Password: "p1",
				},
				Host:    "127.0.0.1:8080",
				Timeout: 2 * time.Second,
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
