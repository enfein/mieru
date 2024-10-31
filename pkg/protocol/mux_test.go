// Copyright (C) 2023  mieru authors
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

package protocol

import (
	"bytes"
	"context"
	"io"
	mrand "math/rand"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/testtool"
	"google.golang.org/protobuf/proto"
)

var users = map[string]*appctlpb.User{
	"xiaochitang": {
		Name:     proto.String("xiaochitang"),
		Password: proto.String("kuiranbudong"),
	},
}

func runClient(t *testing.T, properties UnderlayProperties, username, password []byte, concurrent int) {
	clientMux := NewMux(true).
		SetClientUserNamePassword(string(username), cipher.HashPassword(password, username)).
		SetClientMultiplexFactor(2).
		SetEndpoints([]UnderlayProperties{properties})

	dialCtx, cancelFunc := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFunc()

	var wg sync.WaitGroup
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := clientMux.DialContext(dialCtx)
			if err != nil {
				t.Errorf("DialContext() failed: %v", err)
				return
			}
			defer conn.Close()
			for i := 0; i < 100; i++ {
				payloadSize := mrand.Intn(maxPDU) + 1
				payload := testtool.TestHelperGenRot13Input(payloadSize)
				if _, err := conn.Write(payload); err != nil {
					t.Errorf("Write() failed: %v", err)
				}
				resp := make([]byte, payloadSize)
				if _, err := io.ReadFull(conn, resp); err != nil {
					t.Errorf("io.ReadFull() failed: %v", err)
				}
				rot13, err := testtool.TestHelperRot13(resp)
				if err != nil {
					t.Errorf("TestHelperRot13() failed: %v", err)
				}
				if !bytes.Equal(payload, rot13) {
					t.Errorf("Received unexpected response")
				}
			}
			sessionInfoTable := clientMux.ExportSessionInfoTable()
			if len(sessionInfoTable) < 2 {
				t.Errorf("connection is not shown in the session info table: %v", sessionInfoTable)
			}
		}()
	}
	wg.Wait()

	if err := clientMux.Close(); err != nil {
		t.Errorf("Close client mux failed: %v", err)
	}
}

func TestIPv4TCPUnderlay(t *testing.T) {
	log.SetOutputToTest(t)
	log.SetLevel("DEBUG")
	port, err := common.UnusedTCPPort()
	if err != nil {
		t.Fatalf("common.UnusedTCPPort() failed: %v", err)
	}
	serverProperties := NewUnderlayProperties(1400, common.StreamTransport, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: port}, nil)
	serverMux := NewMux(false).
		SetServerUsers(users).
		SetEndpoints([]UnderlayProperties{serverProperties})
	testServer := testtool.NewTestHelperServer()

	if err := serverMux.Start(); err != nil {
		t.Fatalf("[%s] Start() failed: %v", time.Now().Format(testtool.TimeLayout), err)
	}
	time.Sleep(100 * time.Millisecond)
	go func() {
		if err := testServer.Serve(serverMux); err != nil {
			t.Errorf("[%s] Serve() failed: %v", time.Now().Format(testtool.TimeLayout), err)
		}
	}()
	defer testServer.Close()
	time.Sleep(100 * time.Millisecond)

	clientProperties := NewUnderlayProperties(1400, common.StreamTransport, nil, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: port})
	runClient(t, clientProperties, []byte("xiaochitang"), []byte("kuiranbudong"), 4)
	if err := serverMux.Close(); err != nil {
		t.Errorf("Server mux close failed: %v", err)
	}
}

func TestIPv6TCPUnderlay(t *testing.T) {
	log.SetOutputToTest(t)
	log.SetLevel("DEBUG")
	port, err := common.UnusedTCPPort()
	if err != nil {
		t.Fatalf("common.UnusedTCPPort() failed: %v", err)
	}
	serverProperties := NewUnderlayProperties(1400, common.StreamTransport, &net.TCPAddr{IP: net.ParseIP("::1"), Port: port}, nil)
	serverMux := NewMux(false).
		SetServerUsers(users).
		SetEndpoints([]UnderlayProperties{serverProperties})
	testServer := testtool.NewTestHelperServer()

	if err := serverMux.Start(); err != nil {
		t.Fatalf("[%s] Start() failed: %v", time.Now().Format(testtool.TimeLayout), err)
	}
	time.Sleep(100 * time.Millisecond)
	go func() {
		if err := testServer.Serve(serverMux); err != nil {
			t.Errorf("[%s] Serve() failed: %v", time.Now().Format(testtool.TimeLayout), err)
		}
	}()
	defer testServer.Close()
	time.Sleep(100 * time.Millisecond)

	clientProperties := NewUnderlayProperties(1400, common.StreamTransport, nil, &net.TCPAddr{IP: net.ParseIP("::1"), Port: port})
	runClient(t, clientProperties, []byte("xiaochitang"), []byte("kuiranbudong"), 4)
	if err := serverMux.Close(); err != nil {
		t.Errorf("Server mux close failed: %v", err)
	}
}

func TestIPv4UDPUnderlay(t *testing.T) {
	log.SetOutputToTest(t)
	log.SetLevel("DEBUG")
	port, err := common.UnusedUDPPort()
	if err != nil {
		t.Fatalf("common.UnusedUDPPort() failed: %v", err)
	}
	serverProperties := NewUnderlayProperties(1400, common.PacketTransport, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port}, nil)
	serverMux := NewMux(false).
		SetServerUsers(users).
		SetEndpoints([]UnderlayProperties{serverProperties})
	testServer := testtool.NewTestHelperServer()

	if err := serverMux.Start(); err != nil {
		t.Fatalf("[%s] Start() failed: %v", time.Now().Format(testtool.TimeLayout), err)
	}
	time.Sleep(100 * time.Millisecond)
	go func() {
		if err := testServer.Serve(serverMux); err != nil {
			t.Errorf("[%s] Serve() failed: %v", time.Now().Format(testtool.TimeLayout), err)
		}
	}()
	defer testServer.Close()
	time.Sleep(100 * time.Millisecond)

	clientProperties := NewUnderlayProperties(1400, common.PacketTransport, nil, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port})
	runClient(t, clientProperties, []byte("xiaochitang"), []byte("kuiranbudong"), 4)
	if err := serverMux.Close(); err != nil {
		t.Errorf("Server mux close failed: %v", err)
	}
}

func TestIPv6UDPUnderlay(t *testing.T) {
	log.SetOutputToTest(t)
	log.SetLevel("DEBUG")
	port, err := common.UnusedUDPPort()
	if err != nil {
		t.Fatalf("common.UnusedUDPPort() failed: %v", err)
	}
	serverProperties := NewUnderlayProperties(1400, common.PacketTransport, &net.UDPAddr{IP: net.ParseIP("::1"), Port: port}, nil)
	serverMux := NewMux(false).
		SetServerUsers(users).
		SetEndpoints([]UnderlayProperties{serverProperties})
	testServer := testtool.NewTestHelperServer()

	if err := serverMux.Start(); err != nil {
		t.Fatalf("[%s] Start() failed: %v", time.Now().Format(testtool.TimeLayout), err)
	}
	time.Sleep(100 * time.Millisecond)
	go func() {
		if err := testServer.Serve(serverMux); err != nil {
			t.Errorf("[%s] Serve() failed: %v", time.Now().Format(testtool.TimeLayout), err)
		}
	}()
	defer testServer.Close()
	time.Sleep(100 * time.Millisecond)

	clientProperties := NewUnderlayProperties(1400, common.PacketTransport, nil, &net.UDPAddr{IP: net.ParseIP("::1"), Port: port})
	runClient(t, clientProperties, []byte("xiaochitang"), []byte("kuiranbudong"), 4)
	if err := serverMux.Close(); err != nil {
		t.Errorf("Server mux close failed: %v", err)
	}
}

func TestNewEndpoints(t *testing.T) {
	cases := []struct {
		old []UnderlayProperties
		new []UnderlayProperties
		res []UnderlayProperties
	}{
		{
			nil,
			nil,
			[]UnderlayProperties{},
		},
		{
			nil,
			[]UnderlayProperties{
				NewUnderlayProperties(1400, common.StreamTransport, common.NilNetAddr(), common.NilNetAddr()),
			},
			[]UnderlayProperties{
				NewUnderlayProperties(1400, common.StreamTransport, common.NilNetAddr(), common.NilNetAddr()),
			},
		},
		{
			[]UnderlayProperties{
				NewUnderlayProperties(1400, common.StreamTransport, common.NilNetAddr(), common.NilNetAddr()),
			},
			nil,
			[]UnderlayProperties{},
		},
		{
			[]UnderlayProperties{
				NewUnderlayProperties(1400, common.StreamTransport, common.NilNetAddr(), common.NilNetAddr()),
				NewUnderlayProperties(1400, common.StreamTransport, common.NilNetAddr(), common.NilNetAddr()),
			},
			[]UnderlayProperties{
				NewUnderlayProperties(1400, common.StreamTransport, common.NilNetAddr(), common.NilNetAddr()),
				NewUnderlayProperties(1400, common.PacketTransport, common.NilNetAddr(), common.NilNetAddr()),
			},
			[]UnderlayProperties{
				NewUnderlayProperties(1400, common.PacketTransport, common.NilNetAddr(), common.NilNetAddr()),
			},
		},
	}

	mux := NewMux(false)
	for _, tc := range cases {
		ep := mux.newEndpoints(tc.old, tc.new)
		if !reflect.DeepEqual(ep, tc.res) {
			t.Errorf("newEndpoints(): got %v, want %v", ep, tc.res)
		}
	}
}
