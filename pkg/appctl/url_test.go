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

	pb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"google.golang.org/protobuf/proto"
)

func TestClientConfigWithURL(t *testing.T) {
	c := &pb.ClientConfig{
		Profiles: []*pb.ClientProfile{
			{
				ProfileName: proto.String("default"),
				User: &pb.User{
					Name:     proto.String("abcABC123_!$&'()*+,;=.~-"),
					Password: proto.String("defDEF456_!$&'()*+,;=.~-"),
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
				Mtu: proto.Int32(1280),
				Multiplexing: &pb.MultiplexingConfig{
					Level: pb.MultiplexingLevel_MULTIPLEXING_MIDDLE.Enum(),
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

func TestClientProfileWithMultiURLs(t *testing.T) {
	p := &pb.ClientProfile{
		ProfileName: proto.String("default"),
		User: &pb.User{
			Name:     proto.String("abcABC123_!$&'()*+,;=.~-"),
			Password: proto.String("defDEF456_!$&'()*+,;=.~-"),
		},
		Servers: []*pb.ServerEndpoint{
			{
				IpAddress: proto.String("2001:db8::1"),
				PortBindings: []*pb.PortBinding{
					{
						Port:     proto.Int32(6666),
						Protocol: pb.TransportProtocol_TCP.Enum(),
					},
					{
						PortRange: proto.String("8964-8965"),
						Protocol:  pb.TransportProtocol_UDP.Enum(),
					},
				},
			},
			{
				DomainName: proto.String("example.com"),
				PortBindings: []*pb.PortBinding{
					{
						Port:     proto.Int32(9999),
						Protocol: pb.TransportProtocol_TCP.Enum(),
					},
				},
			},
		},
		Mtu: proto.Int32(1280),
		Multiplexing: &pb.MultiplexingConfig{
			Level: pb.MultiplexingLevel_MULTIPLEXING_MIDDLE.Enum(),
		},
	}

	p0 := &pb.ClientProfile{
		ProfileName: proto.String("default"),
		User: &pb.User{
			Name:     proto.String("abcABC123_!$&'()*+,;=.~-"),
			Password: proto.String("defDEF456_!$&'()*+,;=.~-"),
		},
		Servers: []*pb.ServerEndpoint{
			{
				IpAddress: proto.String("2001:db8::1"),
				PortBindings: []*pb.PortBinding{
					{
						Port:     proto.Int32(6666),
						Protocol: pb.TransportProtocol_TCP.Enum(),
					},
					{
						PortRange: proto.String("8964-8965"),
						Protocol:  pb.TransportProtocol_UDP.Enum(),
					},
				},
			},
		},
		Mtu: proto.Int32(1280),
		Multiplexing: &pb.MultiplexingConfig{
			Level: pb.MultiplexingLevel_MULTIPLEXING_MIDDLE.Enum(),
		},
	}

	p1 := &pb.ClientProfile{
		ProfileName: proto.String("default"),
		User: &pb.User{
			Name:     proto.String("abcABC123_!$&'()*+,;=.~-"),
			Password: proto.String("defDEF456_!$&'()*+,;=.~-"),
		},
		Servers: []*pb.ServerEndpoint{
			{
				DomainName: proto.String("example.com"),
				PortBindings: []*pb.PortBinding{
					{
						Port:     proto.Int32(9999),
						Protocol: pb.TransportProtocol_TCP.Enum(),
					},
				},
			},
		},
		Mtu: proto.Int32(1280),
		Multiplexing: &pb.MultiplexingConfig{
			Level: pb.MultiplexingLevel_MULTIPLEXING_MIDDLE.Enum(),
		},
	}

	urls, err := ClientProfileToMultiURLs(p)
	if err != nil {
		t.Fatalf("ClientConfigToMultiURLs() failed: %v", err)
	}
	if len(urls) != 2 {
		t.Fatalf("got %d URLs, want 2", len(urls))
	}

	profile0, err := URLToClientProfile(urls[0])
	if err != nil {
		t.Fatalf("URLToClientProfile() failed: %v", err)
	}
	if !proto.Equal(profile0, p0) {
		t.Errorf("profile is not equal after generating and loading URL %q", urls[0])
	}
	profile1, err := URLToClientProfile(urls[1])
	if err != nil {
		t.Fatalf("URLToClientProfile() failed: %v", err)
	}
	if !proto.Equal(profile1, p1) {
		t.Errorf("profile is not equal after generating and loading URL %q", urls[1])
	}
}

func TestIsSafeURLString(t *testing.T) {
	testCases := []struct {
		input  string
		isSafe bool
	}{
		{input: "abc", isSafe: true},
		{input: "ABC", isSafe: true},
		{input: "123", isSafe: true},
		{input: "_", isSafe: true},
		{input: "!", isSafe: true},
		{input: "$", isSafe: true},
		{input: "&", isSafe: true},
		{input: "'", isSafe: true},
		{input: "(", isSafe: true},
		{input: ")", isSafe: true},
		{input: "*", isSafe: true},
		{input: "+", isSafe: true},
		{input: ",", isSafe: true},
		{input: ";", isSafe: true},
		{input: "=", isSafe: true},
		{input: ".", isSafe: true},
		{input: "~", isSafe: true},
		{input: "-", isSafe: true},
		{input: "abcABC123_!$&'()*+,;=.~-", isSafe: true},
		{input: " ", isSafe: false},
		{input: "\"", isSafe: false},
		{input: "#", isSafe: false},
		{input: "%", isSafe: false},
		{input: "/", isSafe: false},
		{input: "\\", isSafe: false},
		{input: ":", isSafe: false},
		{input: "<", isSafe: false},
		{input: ">", isSafe: false},
		{input: "?", isSafe: false},
		{input: "@", isSafe: false},
		{input: "[", isSafe: false},
		{input: "]", isSafe: false},
		{input: "^", isSafe: false},
		{input: "`", isSafe: false},
		{input: "{", isSafe: false},
		{input: "|", isSafe: false},
		{input: "}", isSafe: false},
		{input: "abc 123", isSafe: false},
	}

	for _, tc := range testCases {
		actual := isSafeURLString(tc.input)
		if actual != tc.isSafe {
			t.Errorf("isSafeURLString(%q) = %v, want %v", tc.input, actual, tc.isSafe)
		}
	}
}
