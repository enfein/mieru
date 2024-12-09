// Copyright (C) 2024  mieru authors
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
	"fmt"
	"net"

	pb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
)

// ValidateClientConfigSingleProfile validates
// a single client config profile.
//
// It validates
// 1. profile name is not empty
// 2. user name is not empty
// 3. user has either a password or a hashed password
// 4. user has no quota
// 5. it has at least 1 server, and for each server
// 5.1. the server has either IP address or domain name
// 5.2. if set, server's IP address is parsable
// 5.3. the server has at least 1 port binding, and all port bindings are valid
// 6. if set, MTU is valid
func ValidateClientConfigSingleProfile(profile *pb.ClientProfile) error {
	name := profile.GetProfileName()
	if name == "" {
		return fmt.Errorf("profile name is not set")
	}
	user := profile.GetUser()
	if user.GetName() == "" {
		return fmt.Errorf("user name is not set")
	}
	if user.GetPassword() == "" && user.GetHashedPassword() == "" {
		return fmt.Errorf("user password is not set")
	}
	if len(user.GetQuotas()) != 0 {
		return fmt.Errorf("user quota is not supported by proxy client")
	}
	servers := profile.GetServers()
	if len(servers) == 0 {
		return fmt.Errorf("servers are not set")
	}
	for _, server := range servers {
		if server.GetIpAddress() == "" && server.GetDomainName() == "" {
			return fmt.Errorf("neither server IP address nor domain name is set")
		}
		if server.GetIpAddress() != "" && net.ParseIP(server.GetIpAddress()) == nil {
			return fmt.Errorf("failed to parse IP address %q", server.GetIpAddress())
		}
		portBindings := server.GetPortBindings()
		if len(portBindings) == 0 {
			return fmt.Errorf("server port binding is not set")
		}
		if _, err := FlatPortBindings(portBindings); err != nil {
			return err
		}
	}
	if profile.GetMtu() != 0 && (profile.GetMtu() < 1280 || profile.GetMtu() > 1500) {
		return fmt.Errorf("MTU value %d is out of range, valid range is [1280, 1500]", profile.GetMtu())
	}
	return nil
}
