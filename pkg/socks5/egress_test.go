// Copyright (C) 2025  mieru authors
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
	"context"
	crand "crypto/rand"
	mrand "math/rand"
	"testing"

	apicommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/egress"
	"google.golang.org/protobuf/proto"
)

var (
	inputIPv4 = egress.Input{
		Protocol: appctlpb.ProxyProtocol_SOCKS5_PROXY_PROTOCOL,
		Data:     []byte{5, 1, 0, 1, 1, 2, 3, 4, 5, 6},
	}
	inputPrivateIPv4 = egress.Input{
		Protocol: appctlpb.ProxyProtocol_SOCKS5_PROXY_PROTOCOL,
		Data:     []byte{5, 1, 0, 1, 192, 168, 0, 1, 1, 187},
		Env:      map[string]string{"user": "xijinping"},
	}
	inputLoopbackIPv4 = egress.Input{
		Protocol: appctlpb.ProxyProtocol_SOCKS5_PROXY_PROTOCOL,
		Data:     []byte{5, 1, 0, 1, 127, 0, 0, 1, 1, 187},
		Env:      map[string]string{"user": "xijinping"},
	}
	inputIPv6 = egress.Input{
		Protocol: appctlpb.ProxyProtocol_SOCKS5_PROXY_PROTOCOL,
		Data:     []byte{5, 1, 0, 4, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18},
	}
	inputDomainName = egress.Input{
		Protocol: appctlpb.ProxyProtocol_SOCKS5_PROXY_PROTOCOL,
		Data:     []byte{5, 1, 0, 3, 10, 'g', 'o', 'o', 'g', 'l', 'e', '.', 'c', 'o', 'm', 1, 187},
	}
)

func TestNoEgressRule(t *testing.T) {
	controller := &Server{
		config: &Config{
			Egress:   &appctlpb.Egress{},
			Resolver: apicommon.NilDNSResolver{},
		},
	}
	var action egress.Action
	action = controller.FindAction(context.Background(), inputIPv4)
	if action.Action != appctlpb.EgressAction_DIRECT || action.Proxy != nil {
		t.Errorf("got unexpected action for IPv4 input")
	}
	action = controller.FindAction(context.Background(), inputIPv6)
	if action.Action != appctlpb.EgressAction_DIRECT || action.Proxy != nil {
		t.Errorf("got unexpected action for IPv6 input")
	}
	action = controller.FindAction(context.Background(), inputDomainName)
	if action.Action != appctlpb.EgressAction_DIRECT || action.Proxy != nil {
		t.Errorf("got unexpected action for domain name input")
	}
	action = controller.FindAction(context.Background(), inputPrivateIPv4)
	if action.Action != appctlpb.EgressAction_REJECT || action.Proxy != nil {
		t.Errorf("got unexpected action for private IPv4 input")
	}
	action = controller.FindAction(context.Background(), inputLoopbackIPv4)
	if action.Action != appctlpb.EgressAction_REJECT || action.Proxy != nil {
		t.Errorf("got unexpected action for loopback IPv4 input")
	}

	controller.config.Users = map[string]*appctlpb.User{
		"xijinping": {
			Name:            proto.String("xijinping"),
			AllowPrivateIP:  proto.Bool(true),
			AllowLoopbackIP: proto.Bool(true),
		},
	}
	action = controller.FindAction(context.Background(), inputPrivateIPv4)
	if action.Action != appctlpb.EgressAction_DIRECT || action.Proxy != nil {
		t.Errorf("got unexpected action for private IPv4 input")
	}
	action = controller.FindAction(context.Background(), inputLoopbackIPv4)
	if action.Action != appctlpb.EgressAction_DIRECT || action.Proxy != nil {
		t.Errorf("got unexpected action for loopback IPv4 input")
	}
}

func TestEgressRule(t *testing.T) {
	controller := &Server{
		config: &Config{
			Egress: &appctlpb.Egress{
				Proxies: []*appctlpb.EgressProxy{
					{
						Name:     proto.String("wrap"),
						Protocol: appctlpb.ProxyProtocol_SOCKS5_PROXY_PROTOCOL.Enum(),
						Host:     proto.String("127.0.0.1"),
						Port:     proto.Int32(6789),
					},
				},
				Rules: []*appctlpb.EgressRule{
					{
						IpRanges:    []string{"*"},
						DomainNames: []string{"*"},
						Action:      appctlpb.EgressAction_PROXY.Enum(),
						ProxyName:   proto.String("wrap"),
					},
				},
			},
			Resolver: apicommon.NilDNSResolver{},
		},
	}
	var action egress.Action
	action = controller.FindAction(context.Background(), inputIPv4)
	if action.Action != appctlpb.EgressAction_PROXY || action.Proxy == nil {
		t.Errorf("got unexpected action for IPv4 input")
	}
	action = controller.FindAction(context.Background(), inputIPv6)
	if action.Action != appctlpb.EgressAction_PROXY || action.Proxy == nil {
		t.Errorf("got unexpected action for IPv6 input")
	}
	action = controller.FindAction(context.Background(), inputDomainName)
	if action.Action != appctlpb.EgressAction_PROXY || action.Proxy == nil {
		t.Errorf("got unexpected action for domain name input")
	}
	for i := 0; i < 1000; i++ {
		input := egress.Input{}
		input.Protocol = appctlpb.ProxyProtocol(mrand.Int31n(2))
		inputLen := mrand.Intn(11)
		b := make([]byte, inputLen)
		if _, err := crand.Read(b); err != nil {
			t.Fatalf("Read() failed: %v", err)
		}
		if len(b) >= 2 && b[0] == constant.Socks5Version && (b[1] == constant.Socks5ConnectCmd || b[1] == constant.Socks5UDPAssociateCmd) {
			continue
		}
		input.Data = b
		action = controller.FindAction(context.Background(), input)
		if action.Action != appctlpb.EgressAction_DIRECT || action.Proxy != nil {
			t.Errorf("got unexpected action for random input %v", b)
		}
	}
}

func TestMatchEgressRule(t *testing.T) {
	controller := &Server{
		config: &Config{
			Egress: &appctlpb.Egress{
				Proxies: []*appctlpb.EgressProxy{
					{
						Name:     proto.String("cloudflare"),
						Protocol: appctlpb.ProxyProtocol_SOCKS5_PROXY_PROTOCOL.Enum(),
						Host:     proto.String("127.0.0.1"),
						Port:     proto.Int32(6789),
					},
				},
				Rules: []*appctlpb.EgressRule{
					{
						IpRanges: []string{"192.168.0.0/16"},
						Action:   appctlpb.EgressAction_DIRECT.Enum(),
					},
					{
						DomainNames: []string{"google.com"},
						Action:      appctlpb.EgressAction_DIRECT.Enum(),
					},
					{
						IpRanges:    []string{"*"},
						DomainNames: []string{"*"},
						Action:      appctlpb.EgressAction_PROXY.Enum(),
						ProxyName:   proto.String("cloudflare"),
					},
				},
			},
			Resolver: apicommon.NilDNSResolver{},
			Users: map[string]*appctlpb.User{
				"test": {
					Name:           proto.String("test"),
					AllowPrivateIP: proto.Bool(true),
				},
			},
		},
	}

	tests := []struct {
		name       string
		input      egress.Input
		wantAction appctlpb.EgressAction
	}{
		{
			name: "IP matching explicit rule",
			input: egress.Input{
				Protocol: appctlpb.ProxyProtocol_SOCKS5_PROXY_PROTOCOL,
				Data:     []byte{5, 1, 0, 1, 192, 168, 0, 1, 1, 187},
				Env:      map[string]string{"user": "test"},
			},
			wantAction: appctlpb.EgressAction_DIRECT,
		},
		{
			name: "Domain matching explicit rule",
			input: egress.Input{
				Protocol: appctlpb.ProxyProtocol_SOCKS5_PROXY_PROTOCOL,
				Data:     []byte{5, 1, 0, 3, 12, 'a', '.', 'g', 'o', 'o', 'g', 'l', 'e', '.', 'c', 'o', 'm', 1, 187},
			},
			wantAction: appctlpb.EgressAction_DIRECT,
		},
		{
			name: "IP matching default rule",
			input: egress.Input{
				Protocol: appctlpb.ProxyProtocol_SOCKS5_PROXY_PROTOCOL,
				Data:     []byte{5, 1, 0, 1, 8, 8, 8, 8, 0, 53},
			},
			wantAction: appctlpb.EgressAction_PROXY,
		},
		{
			name: "Domain matching default rule",
			input: egress.Input{
				Protocol: appctlpb.ProxyProtocol_SOCKS5_PROXY_PROTOCOL,
				Data:     []byte{5, 1, 0, 3, 11, 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm', 1, 187},
			},
			wantAction: appctlpb.EgressAction_PROXY,
		},
		{
			name: "Domain matching explicit rule exactly",
			input: egress.Input{
				Protocol: appctlpb.ProxyProtocol_SOCKS5_PROXY_PROTOCOL,
				Data:     []byte{5, 1, 0, 3, 10, 'g', 'o', 'o', 'g', 'l', 'e', '.', 'c', 'o', 'm', 1, 187},
			},
			wantAction: appctlpb.EgressAction_DIRECT,
		},
		{
			name: "Partial domain suffix does not match",
			input: egress.Input{
				Protocol: appctlpb.ProxyProtocol_SOCKS5_PROXY_PROTOCOL,
				Data:     []byte{5, 1, 0, 3, 14, 'e', 'v', 'i', 'l', 'g', 'o', 'o', 'g', 'l', 'e', '.', 'c', 'o', 'm', 1, 187},
			},
			wantAction: appctlpb.EgressAction_PROXY,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := controller.FindAction(context.Background(), tt.input)
			if action.Action != tt.wantAction {
				t.Errorf("expected %v, but got %v", tt.wantAction, action.Action)
			}
			if action.Action == appctlpb.EgressAction_PROXY && action.Proxy == nil {
				t.Errorf("expected Proxy to be non-nil, but got nil")
			}
		})
	}
}
