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
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/egress"
	"github.com/enfein/mieru/v3/pkg/log"
)

var (
	// socks5.Server implements egress.Controller interface.
	_ egress.Controller = (*Server)(nil)
)

func (s *Server) FindAction(in egress.Input) egress.Action {
	// DIRECT is used for all invalid inputs, such that they are handled by
	// the subsequent logic.
	if in.Protocol != appctlpb.ProxyProtocol_SOCKS5_PROXY_PROTOCOL {
		log.Debugf("socks5 egress controller: %s is not supported", in.Protocol.String())
		return egress.Action{
			Action: appctlpb.EgressAction_DIRECT,
		}
	}
	// The request's VER must be 0x05 and CMD must be 0x01 (CONNECT).
	// CMD 0x03 (UDP ASSOCIATE) is not supported at the moment.
	if len(in.Data) < 4 {
		log.Debugf("socks5 egress controller: input %v is too short", in.Data)
		return egress.Action{
			Action: appctlpb.EgressAction_DIRECT,
		}
	}
	if in.Data[0] != 0x05 {
		log.Debugf("socks5 egress controller: input %v is not socks5 protocol", in.Data)
		return egress.Action{
			Action: appctlpb.EgressAction_DIRECT,
		}
	} else if in.Data[1] == 0x01 {
		isIP := in.Data[3] == 0x01 || in.Data[3] == 0x04
		isDomainName := in.Data[3] == 0x03
		for _, rule := range s.config.Egress.GetRules() {
			if (isIP && len(rule.GetIpRanges()) > 0 && rule.GetIpRanges()[0] == "*") || (isDomainName && len(rule.GetDomainNames()) > 0 && rule.GetDomainNames()[0] == "*") {
				for _, proxy := range s.config.Egress.GetProxies() {
					if proxy.GetName() == rule.GetProxyName() {
						return egress.Action{
							Action: rule.GetAction(),
							Proxy:  proxy,
						}
					}
				}
			}
		}
		return egress.Action{
			Action: appctlpb.EgressAction_DIRECT,
		}
	} else {
		return egress.Action{
			Action: appctlpb.EgressAction_DIRECT,
		}
	}
}
