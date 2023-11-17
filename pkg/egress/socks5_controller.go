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

package egress

import (
	"github.com/enfein/mieru/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/pkg/log"
)

type Socks5Controller struct {
	config *appctlpb.Egress
}

var (
	_ Controller = &Socks5Controller{}
)

func NewSocks5Controller(config *appctlpb.Egress) *Socks5Controller {
	if config == nil {
		config = &appctlpb.Egress{}
	}
	return &Socks5Controller{
		config: config,
	}
}

func (c *Socks5Controller) FindAction(in Input) Action {
	if in.Protocol != appctlpb.ProxyProtocol_SOCKS5_PROXY_PROTOCOL {
		log.Debugf("egress Socks5Controller: %s is not supported", in.Protocol.String())
		return Action{
			Action: appctlpb.EgressAction_DIRECT,
		}
	}
	// The request's VER must be 0x05 and CMD must be 0x01 (CONNECT).
	// CMD 0x03 (UDP ASSOCIATE) is not supported at the moment.
	if len(in.Data) < 4 {
		log.Debugf("egress Socks5Controller: input %v is too short", in.Data)
		return Action{
			Action: appctlpb.EgressAction_DIRECT,
		}
	}
	if in.Data[0] != 0x05 {
		log.Debugf("egress Socks5Controller: input %v is not socks5 protocol", in.Data)
		return Action{
			Action: appctlpb.EgressAction_DIRECT,
		}
	} else if in.Data[1] == 0x01 {
		isIP := in.Data[3] == 0x01 || in.Data[3] == 0x04
		isDomainName := in.Data[3] == 0x03
		for _, rule := range c.config.GetRules() {
			if (isIP && len(rule.GetIpRanges()) > 0 && rule.GetIpRanges()[0] == "*") || (isDomainName && len(rule.GetDomainNames()) > 0 && rule.GetDomainNames()[0] == "*") {
				for _, proxy := range c.config.GetProxies() {
					if proxy.GetName() == rule.GetProxyName() {
						return Action{
							Action: rule.GetAction(),
							Proxy:  proxy,
						}
					}
				}
			}
		}
		return Action{
			Action: appctlpb.EgressAction_DIRECT,
		}
	} else {
		return Action{
			Action: appctlpb.EgressAction_DIRECT,
		}
	}
}
