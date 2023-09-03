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

package protocolv2

import (
	"bytes"
	"context"
	"io"
	mrand "math/rand"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/enfein/mieru/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/pkg/cipher"
	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/netutil"
	"github.com/enfein/mieru/pkg/rng"
	"github.com/enfein/mieru/pkg/testtool"
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
		SetClientPassword(cipher.HashPassword(password, username)).
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
				payloadSize := mrand.Intn(MaxPDU) + 1
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
		}()
	}
	wg.Wait()

	if err := clientMux.Close(); err != nil {
		t.Errorf("Close client mux failed: %v", err)
	}
}

func TestIPv4TCPUnderlay(t *testing.T) {
	rng.InitSeed()
	log.SetOutputToTest(t)
	log.SetLevel("DEBUG")
	port, err := netutil.UnusedTCPPort()
	if err != nil {
		t.Fatalf("netutil.UnusedTCPPort() failed: %v", err)
	}
	serverDescriptor := underlayDescriptor{
		mtu:               1500,
		ipVersion:         netutil.IPVersion4,
		transportProtocol: netutil.TCPTransport,
		localAddr:         &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: port},
	}
	serverMux := NewMux(false).
		SetServerUsers(users).
		SetServerHandler(testtool.TestHelperConnHandler{}).
		SetEndpoints([]UnderlayProperties{serverDescriptor})

	go func() {
		if err := serverMux.ListenAndServeAll(); err != nil {
			t.Errorf("[%s] ListenAndServeAll() failed: %v", time.Now().Format(testtool.TimeLayout), err)
		}
	}()
	time.Sleep(1 * time.Second)

	clientDescriptor := underlayDescriptor{
		mtu:               1500,
		ipVersion:         netutil.IPVersion4,
		transportProtocol: netutil.TCPTransport,
		remoteAddr:        &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: port},
	}
	runClient(t, clientDescriptor, []byte("xiaochitang"), []byte("kuiranbudong"), 4)
	if err := serverMux.Close(); err != nil {
		t.Errorf("Server mux close failed: %v", err)
	}
}

func TestIPv6TCPUnderlay(t *testing.T) {
	rng.InitSeed()
	log.SetOutputToTest(t)
	log.SetLevel("DEBUG")
	port, err := netutil.UnusedTCPPort()
	if err != nil {
		t.Fatalf("netutil.UnusedTCPPort() failed: %v", err)
	}
	serverDescriptor := underlayDescriptor{
		mtu:               1500,
		ipVersion:         netutil.IPVersion6,
		transportProtocol: netutil.TCPTransport,
		localAddr:         &net.TCPAddr{IP: net.ParseIP("::1"), Port: port},
	}
	serverMux := NewMux(false).
		SetServerUsers(users).
		SetServerHandler(testtool.TestHelperConnHandler{}).
		SetEndpoints([]UnderlayProperties{serverDescriptor})

	go func() {
		if err := serverMux.ListenAndServeAll(); err != nil {
			t.Errorf("[%s] ListenAndServeAll() failed: %v", time.Now().Format(testtool.TimeLayout), err)
		}
	}()
	time.Sleep(1 * time.Second)

	clientDescriptor := underlayDescriptor{
		mtu:               1500,
		ipVersion:         netutil.IPVersion6,
		transportProtocol: netutil.TCPTransport,
		remoteAddr:        &net.TCPAddr{IP: net.ParseIP("::1"), Port: port},
	}
	runClient(t, clientDescriptor, []byte("xiaochitang"), []byte("kuiranbudong"), 4)
	if err := serverMux.Close(); err != nil {
		t.Errorf("Server mux close failed: %v", err)
	}
}

func TestIPv4UDPUnderlay(t *testing.T) {
	rng.InitSeed()
	log.SetOutputToTest(t)
	log.SetLevel("TRACE")
	port, err := netutil.UnusedUDPPort()
	if err != nil {
		t.Fatalf("netutil.UnusedUDPPort() failed: %v", err)
	}
	serverDescriptor := underlayDescriptor{
		mtu:               1500,
		ipVersion:         netutil.IPVersion4,
		transportProtocol: netutil.UDPTransport,
		localAddr:         &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port},
	}
	serverMux := NewMux(false).
		SetServerUsers(users).
		SetServerHandler(testtool.TestHelperConnHandler{}).
		SetEndpoints([]UnderlayProperties{serverDescriptor})

	go func() {
		if err := serverMux.ListenAndServeAll(); err != nil {
			t.Errorf("[%s] ListenAndServeAll() failed: %v", time.Now().Format(testtool.TimeLayout), err)
		}
	}()
	time.Sleep(1 * time.Second)

	clientDescriptor := underlayDescriptor{
		mtu:               1500,
		ipVersion:         netutil.IPVersion4,
		transportProtocol: netutil.UDPTransport,
		remoteAddr:        &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port},
	}
	runClient(t, clientDescriptor, []byte("xiaochitang"), []byte("kuiranbudong"), 1)
	if err := serverMux.Close(); err != nil {
		t.Errorf("Server mux close failed: %v", err)
	}
}

func TestIPv6UDPUnderlay(t *testing.T) {
	rng.InitSeed()
	log.SetOutputToTest(t)
	log.SetLevel("TRACE")
	port, err := netutil.UnusedUDPPort()
	if err != nil {
		t.Fatalf("netutil.UnusedUDPPort() failed: %v", err)
	}
	serverDescriptor := underlayDescriptor{
		mtu:               1500,
		ipVersion:         netutil.IPVersion6,
		transportProtocol: netutil.UDPTransport,
		localAddr:         &net.UDPAddr{IP: net.ParseIP("::1"), Port: port},
	}
	serverMux := NewMux(false).
		SetServerUsers(users).
		SetServerHandler(testtool.TestHelperConnHandler{}).
		SetEndpoints([]UnderlayProperties{serverDescriptor})

	go func() {
		if err := serverMux.ListenAndServeAll(); err != nil {
			t.Errorf("[%s] ListenAndServeAll() failed: %v", time.Now().Format(testtool.TimeLayout), err)
		}
	}()
	time.Sleep(1 * time.Second)

	clientDescriptor := underlayDescriptor{
		mtu:               1500,
		ipVersion:         netutil.IPVersion6,
		transportProtocol: netutil.UDPTransport,
		remoteAddr:        &net.UDPAddr{IP: net.ParseIP("::1"), Port: port},
	}
	runClient(t, clientDescriptor, []byte("xiaochitang"), []byte("kuiranbudong"), 1)
	if err := serverMux.Close(); err != nil {
		t.Errorf("Server mux close failed: %v", err)
	}
}
