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

package appctl

import (
	"testing"

	pb "github.com/enfein/mieru/pkg/appctl/appctlpb"
	"google.golang.org/protobuf/proto"
)

func TestURL(t *testing.T) {
	c := &pb.ClientConfig{
		Profiles: []*pb.ClientProfile{
			{
				ProfileName: proto.String("default"),
				User: &pb.User{
					Name:     proto.String("qingguanyidao"),
					Password: proto.String("tongshangkuanyi"),
				},
				Servers: []*pb.ServerEndpoint{
					{
						IpAddress: proto.String("1.2.3.4"),
						PortBindings: []*pb.PortBinding{
							{
								Port:     proto.Int32(6666),
								Protocol: pb.TransportProtocol_TCP.Enum(),
							},
						},
					},
				},
			},
		},
		ActiveProfile: proto.String("default"),
		RpcPort:       proto.Int32(8989),
		Socks5Port:    proto.Int32(1080),
		LoggingLevel:  pb.LoggingLevel_INFO.Enum(),
	}

	link, err := ClientConfigToURL(c)
	if err != nil {
		t.Fatalf("ClientConfigToURL() failed: %v", err)
	}
	c2, err := URLToClientConfig(link)
	if err != nil {
		t.Fatalf("URLToClientConfig() failed: %v", err)
	}
	if !proto.Equal(c, c2) {
		t.Fatalf("client config is not equal after generating and loading URL:\n%s\n%s", c.String(), c2.String())
	}
}
