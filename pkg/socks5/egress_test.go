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
	crand "crypto/rand"
	mrand "math/rand"
	"testing"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/egress"
	"google.golang.org/protobuf/proto"
)

var (
	inputIPv4 = egress.Input{
		Protocol: appctlpb.ProxyProtocol_SOCKS5_PROXY_PROTOCOL,
		Data:     []byte{5, 1, 0, 1, 1, 2, 3, 4, 5, 6},
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
			Egress: &appctlpb.Egress{},
		},
	}
	var action egress.Action
	action = controller.FindAction(inputIPv4)
	if action.Action != appctlpb.EgressAction_DIRECT || action.Proxy != nil {
		t.Errorf("got unexpected action for IPv4 input")
	}
	action = controller.FindAction(inputIPv6)
	if action.Action != appctlpb.EgressAction_DIRECT || action.Proxy != nil {
		t.Errorf("got unexpected action for IPv6 input")
	}
	action = controller.FindAction(inputDomainName)
	if action.Action != appctlpb.EgressAction_DIRECT || action.Proxy != nil {
		t.Errorf("got unexpected action for domain name input")
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
		},
	}
	var action egress.Action
	action = controller.FindAction(inputIPv4)
	if action.Action != appctlpb.EgressAction_PROXY || action.Proxy == nil {
		t.Errorf("got unexpected action for IPv4 input")
	}
	action = controller.FindAction(inputIPv6)
	if action.Action != appctlpb.EgressAction_PROXY || action.Proxy == nil {
		t.Errorf("got unexpected action for IPv6 input")
	}
	action = controller.FindAction(inputDomainName)
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
		if len(b) >= 2 && b[0] == 0x05 && (b[1] == 0x01 || b[1] == 0x03) {
			continue
		}
		input.Data = b
		action = controller.FindAction(input)
		if action.Action != appctlpb.EgressAction_DIRECT || action.Proxy != nil {
			t.Errorf("got unexpected action for random input %v", b)
		}
	}
}
