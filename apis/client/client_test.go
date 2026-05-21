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

package client

import (
	"errors"
	"strings"
	"testing"

	pb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"google.golang.org/protobuf/proto"
)

func TestStoreRejectProfileDialer(t *testing.T) {
	c := NewClient()
	profile := generateClientProfile(t)
	profile.Dialer = &pb.ClientDialer{
		Protocol:           pb.ProxyProtocol_SOCKS5_PROXY_PROTOCOL.Enum(),
		Host:               proto.String("127.0.0.1"),
		Port:               proto.Int32(1081),
		Socks5UDPAssociate: proto.Bool(true),
	}

	err := c.Store(&ClientConfig{Profile: profile})
	if err == nil {
		t.Fatal("Store() error = nil, want error")
	}
	if !errors.Is(err, ErrInvalidClientConfig) {
		t.Fatalf("Store() error = %v, want ErrInvalidClientConfig", err)
	}
	if !strings.Contains(err.Error(), "client profile dialer is not supported by client API") {
		t.Fatalf("Store() error = %q, want profile dialer rejection", err.Error())
	}
}

func generateClientProfile(t *testing.T) *pb.ClientProfile {
	t.Helper()
	return &pb.ClientProfile{
		ProfileName: proto.String("default"),
		User: &pb.User{
			Name:     proto.String("user"),
			Password: proto.String("password"),
		},
		Servers: []*pb.ServerEndpoint{
			{
				IpAddress: proto.String("127.0.0.1"),
				PortBindings: []*pb.PortBinding{
					{
						Port:     proto.Int32(10001),
						Protocol: pb.TransportProtocol_TCP.Enum(),
					},
				},
			},
		},
	}
}
