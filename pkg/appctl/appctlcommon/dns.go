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
	"fmt"
	"net"
	"strings"

	apicommon "github.com/enfein/mieru/v3/apis/common"
	pb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
)

// TransformDNSHosts validates and normalizes static DNS host entries from protobuf.
func TransformDNSHosts(dns *pb.DNS) (map[string]net.IP, error) {
	if dns == nil || len(dns.GetHosts()) == 0 {
		return nil, nil
	}

	hosts := make(map[string]net.IP, len(dns.GetHosts()))
	for domainName, ipString := range dns.GetHosts() {
		if strings.HasPrefix(domainName, ".") {
			return nil, fmt.Errorf("domain name %q must not begin with a dot", domainName)
		}
		if strings.HasSuffix(domainName, ".") {
			return nil, fmt.Errorf("domain name %q must not end with a dot", domainName)
		}
		normalizedDomainName := apicommon.NormalizeDomainName(domainName)
		if normalizedDomainName == "" {
			return nil, fmt.Errorf("domain name is empty")
		}
		if _, found := hosts[normalizedDomainName]; found {
			return nil, fmt.Errorf("found duplicate domain name %q after normalization", domainName)
		}
		ip := net.ParseIP(ipString)
		if ip == nil {
			return nil, fmt.Errorf("domain name %q has invalid IP address %q", domainName, ipString)
		}
		hosts[normalizedDomainName] = ip
	}
	return hosts, nil
}
