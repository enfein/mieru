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

package appctlcommon

import (
	"net"
	"testing"

	pb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"google.golang.org/protobuf/proto"
)

func TestPortBindingsListenIPAddress(t *testing.T) {
	bindings := []*pb.PortBinding{
		{PortRange: proto.String("8000-8001"), Protocol: pb.TransportProtocol_TCP.Enum()},
		{PortRange: proto.String("8000-8001"), Protocol: pb.TransportProtocol_UDP.Enum()},
	}
	for _, address := range []string{"", "127.0.0.1", "0.0.0.0", "::1", "::", "2001:db8::1"} {
		t.Run(address, func(t *testing.T) {
			endpoints, err := AddrPortToUnderlayProperties(address, bindings, 1400)
			if err != nil {
				t.Fatal(err)
			}
			if len(endpoints) != 4 {
				t.Fatalf("got %d endpoints, want 4", len(endpoints))
			}
			for i, endpoint := range endpoints {
				network := "tcp"
				if i >= 2 {
					network = "udp"
				}
				port := "8000"
				if i%2 == 1 {
					port = "8001"
				}
				if got, want := endpoint.LocalAddr().String(), net.JoinHostPort(address, port); got != want {
					t.Errorf("endpoint %d address = %q, want %q", i, got, want)
				}
				if endpoint.LocalAddr().Network() != network {
					t.Errorf("endpoint %d network = %q, want %q", i, endpoint.LocalAddr().Network(), network)
				}
				if endpoint.MTU() != 1400 {
					t.Errorf("endpoint %d MTU = %d, want 1400", i, endpoint.MTU())
				}
			}
		})
	}
	for _, address := range []string{"localhost", "256.0.0.1", "127.0.0.1:8000", "[::1]", "127.0.0.1/8", " ::1", "fe80::1%eth0"} {
		t.Run(address, func(t *testing.T) {
			if _, err := AddrPortToUnderlayProperties(address, bindings, 1400); err == nil {
				t.Fatal("invalid listen IP address accepted")
			}
		})
	}
}
