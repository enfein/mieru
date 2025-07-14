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
	"net"
	"strings"

	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/egress"
	"github.com/enfein/mieru/v3/pkg/log"
)

var (
	// socks5.Server implements egress.Controller interface.
	_ egress.Controller = (*Server)(nil)
)

var wellKnownIPv4LocalDomainNames = []string{
	"localhost", // can be resolved to IPv6 address
	"localhost4",
	"localhost.localdomain", // can be resolved to IPv6 address
	"localhost4.localdomain4",
}

var wellKnownIPv6LocalDomainNames = []string{
	"localhost6",
	"ip6-localhost",
	"ip6-loopback",
	"localhost6.localdomain6",
}

func (s *Server) FindAction(ctx context.Context, in egress.Input) egress.Action {
	// DIRECT is used for all invalid inputs, such that they are handled by
	// the subsequent logic.
	if in.Protocol != appctlpb.ProxyProtocol_SOCKS5_PROXY_PROTOCOL {
		log.Debugf("socks5 egress controller: %s is not supported", in.Protocol.String())
		return egress.Action{
			Action: appctlpb.EgressAction_DIRECT,
		}
	}
	if len(in.Data) < 4 {
		log.Debugf("socks5 egress controller: input %v is too short", in.Data)
		return egress.Action{
			Action: appctlpb.EgressAction_DIRECT,
		}
	}
	if in.Data[0] != constant.Socks5Version {
		log.Debugf("socks5 egress controller: input %v is not socks5 protocol", in.Data)
		return egress.Action{
			Action: appctlpb.EgressAction_DIRECT,
		}
	} else if in.Data[1] == constant.Socks5ConnectCmd || in.Data[1] == constant.Socks5UDPAssociateCmd {
		// 1. Check if the request should be rejected because the destination is
		// private or loopback IP.
		action := s.rejectPrivateAndLoopbackIPAction(ctx, in)
		if action.Action == appctlpb.EgressAction_REJECT {
			return action
		}
		// 2. Check if the request should be forwarded to another proxy.
		return s.forwardToProxyAction(ctx, in)
	}
	return egress.Action{
		Action: appctlpb.EgressAction_DIRECT,
	}
}

func (s *Server) rejectPrivateAndLoopbackIPAction(_ context.Context, in egress.Input) egress.Action {
	var ip net.IP
	if in.Data[3] == constant.Socks5IPv4Address {
		ip = net.IP(in.Data[4:8])
	} else if in.Data[3] == constant.Socks5IPv6Address {
		ip = net.IP(in.Data[4:20])
	} else if in.Data[3] == constant.Socks5FQDNAddress {
		domainNameLength := int(in.Data[4])
		domainName := string(in.Data[5 : 5+domainNameLength])
		// If we do a DNS lookup, we leak the destination domain name to the DNS server.
		// For user privacy, we only check some well-known local domain names.
		isWellKnownIPv4LocalDomainName := false
		isWellKnownIPv6LocalDomainName := false
		for _, d := range wellKnownIPv4LocalDomainNames {
			if domainName == d {
				isWellKnownIPv4LocalDomainName = true
				break
			}
		}
		for _, d := range wellKnownIPv6LocalDomainNames {
			if domainName == d {
				isWellKnownIPv6LocalDomainName = true
				break
			}
		}
		if isWellKnownIPv4LocalDomainName {
			ip = net.ParseIP("127.0.0.1")
		} else if isWellKnownIPv6LocalDomainName {
			ip = net.ParseIP("::1")
		} else {
			return egress.Action{
				Action: appctlpb.EgressAction_DIRECT,
			}
		}
	}

	if !ip.IsPrivate() && !ip.IsLoopback() {
		return egress.Action{
			Action: appctlpb.EgressAction_DIRECT,
		}
	}

	// For testing propose, allow bypassing the user check below.
	if ip.IsLoopback() && s.config.AllowLoopbackDestination {
		return egress.Action{
			Action: appctlpb.EgressAction_DIRECT,
		}
	}

	// Load user information.
	userName, ok := in.Env["user"]
	if !ok || userName == "" {
		// User name is unknown.
		// By default, we reject the request.
		return egress.Action{
			Action: appctlpb.EgressAction_REJECT,
		}
	}
	user, ok := s.config.Users[userName]
	if !ok {
		// User is not registered.
		// By default, we reject the request.
		return egress.Action{
			Action: appctlpb.EgressAction_REJECT,
		}
	}
	if ip.IsPrivate() && user.GetAllowPrivateIP() {
		return egress.Action{
			Action: appctlpb.EgressAction_DIRECT,
		}
	} else if ip.IsLoopback() && user.GetAllowLoopbackIP() {
		return egress.Action{
			Action: appctlpb.EgressAction_DIRECT,
		}
	}
	return egress.Action{
		Action: appctlpb.EgressAction_REJECT,
	}
}

func (s *Server) forwardToProxyAction(_ context.Context, in egress.Input) egress.Action {
	addrType := in.Data[3]
	var addr net.IP
	var domain string

	switch addrType {
	case constant.Socks5IPv4Address:
		addr = net.IP(in.Data[4:8])
	case constant.Socks5IPv6Address:
		addr = net.IP(in.Data[4:20])
	case constant.Socks5FQDNAddress:
		domainLen := int(in.Data[4])
		domain = string(in.Data[5 : 5+domainLen])
	default:
		return egress.Action{Action: appctlpb.EgressAction_DIRECT}
	}

	for _, rule := range s.config.Egress.GetRules() {
		if s.matchEgressRule(addr, domain, rule) {
			if rule.GetAction() == appctlpb.EgressAction_PROXY {
				for _, proxy := range s.config.Egress.GetProxies() {
					if proxy.GetName() == rule.GetProxyName() {
						return egress.Action{
							Action: rule.GetAction(),
							Proxy:  proxy,
						}
					}
				}
			}
			return egress.Action{Action: rule.GetAction()}
		}
	}

	return egress.Action{Action: appctlpb.EgressAction_DIRECT}
}

func (s *Server) matchEgressRule(addr net.IP, domain string, rule *appctlpb.EgressRule) bool {
	if addr != nil {
		// IP based rule
		for _, ipRange := range rule.GetIpRanges() {
			if ipRange == "*" {
				return true
			}
			_, cidr, err := net.ParseCIDR(ipRange)
			if err != nil {
				continue
			}
			if cidr.Contains(addr) {
				return true
			}
		}
	} else if domain != "" {
		// Domain name based rule.
		for _, d := range rule.GetDomainNames() {
			if d == "*" {
				return true
			}
			if domain == d || strings.HasSuffix(domain, "."+d) {
				return true
			}
		}
	}
	return false
}
