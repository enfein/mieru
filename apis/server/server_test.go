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

package server

import (
	"context"
	"errors"
	"net"
	"testing"

	pb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"google.golang.org/protobuf/proto"
)

func TestStoreListenIPAddress(t *testing.T) {
	for _, c := range []struct {
		address string
		wantErr bool
	}{
		{"", false}, {"127.0.0.1", false}, {"0.0.0.0", false}, {"::1", false}, {"::", false},
		{"localhost", true}, {"256.0.0.1", true}, {"[::1]", true}, {"127.0.0.1:80", true},
	} {
		t.Run(c.address, func(t *testing.T) {
			config := listenConfig(c.address)
			err := NewServer().Store(&ServerConfig{Config: config})
			if (err != nil) != c.wantErr {
				t.Fatalf("Store() error = %v, want error %v", err, c.wantErr)
			}
			if c.wantErr && !errors.Is(err, ErrInvalidServerConfig) {
				t.Fatalf("error = %v, want ErrInvalidServerConfig", err)
			}
		})
	}
}

func TestStartListenIPAddress(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "::1"} {
		t.Run(address, func(t *testing.T) {
			if address == "::1" {
				listener, err := net.Listen("tcp6", "[::1]:0")
				if err != nil {
					t.Skipf("IPv6 loopback unavailable: %v", err)
				}
				listener.Close()
			}
			factory := &listenFactory{addresses: make(chan net.Addr, 2)}
			server := NewServer()
			t.Cleanup(func() { server.Stop() })
			if err := server.Store(&ServerConfig{
				Config: listenConfig(address), StreamListenerFactory: factory, PacketListenerFactory: factory,
			}); err != nil {
				t.Fatal(err)
			}
			if err := server.Start(); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				bound := <-factory.addresses
				host, _, err := net.SplitHostPort(bound.String())
				if err != nil {
					t.Fatal(err)
				}
				if host != address {
					t.Errorf("%s bound to %q, want %q", bound.Network(), host, address)
				}
			}
		})
	}
}

func listenConfig(address string) *pb.ServerConfig {
	return &pb.ServerConfig{
		ListenIPAddress: proto.String(address),
		PortBindings: []*pb.PortBinding{
			{Port: proto.Int32(8000), Protocol: pb.TransportProtocol_TCP.Enum()},
			{Port: proto.Int32(8000), Protocol: pb.TransportProtocol_UDP.Enum()},
		},
		Users: []*pb.User{{Name: proto.String("user"), Password: proto.String("password")}},
	}
}

// listenFactory uses ephemeral ports while preserving the configured listen IP.
type listenFactory struct{ addresses chan net.Addr }

func (f *listenFactory) Listen(ctx context.Context, network, address string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	lc := &net.ListenConfig{}
	listener, err := lc.Listen(ctx, network, net.JoinHostPort(host, "0"))
	if err == nil {
		f.addresses <- listener.Addr()
	}
	return listener, err
}

func (f *listenFactory) ListenPacket(ctx context.Context, network, address string) (net.PacketConn, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	lc := &net.ListenConfig{}
	conn, err := lc.ListenPacket(ctx, network, net.JoinHostPort(host, "0"))
	if err == nil {
		f.addresses <- conn.LocalAddr()
	}
	return conn, err
}
