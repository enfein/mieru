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
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	pb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/stderror"
	"google.golang.org/protobuf/proto"
)

// ClientConfigToURL creates a URL to share the client configuration.
func ClientConfigToURL(config *pb.ClientConfig) (string, error) {
	if config == nil {
		return "", stderror.ErrNullPointer
	}
	b, err := proto.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("proto.Marshal() failed: %w", err)
	}
	return "mieru://" + base64.StdEncoding.EncodeToString(b), nil
}

// ClientProfileToMultiURLs creates a list of human readable URLs to share
// the client profile configuration.
func ClientProfileToMultiURLs(profile *pb.ClientProfile) (urls []string, err error) {
	if profile == nil {
		return nil, stderror.ErrNullPointer
	}
	profileName := profile.GetProfileName()
	userName := profile.GetUser().GetName()
	password := profile.GetUser().GetPassword()
	servers := profile.GetServers()
	if profileName == "" {
		return nil, fmt.Errorf("profile name is empty")
	}
	if userName == "" {
		return nil, fmt.Errorf("user name in profile %s is empty", profileName)
	}
	if password == "" {
		return nil, fmt.Errorf("password in profile %s is empty", profileName)
	}
	if len(servers) == 0 {
		return nil, fmt.Errorf("profile %s has no server", profileName)
	}
	for _, server := range servers {
		u := &url.URL{Scheme: "mierus"} // mierus => mieru simple
		u.User = url.UserPassword(userName, password)
		if server.GetDomainName() != "" {
			u.Host = server.GetDomainName()
		} else if server.GetIpAddress() != "" {
			u.Host = common.MaybeDecorateIPv6(server.GetIpAddress())
		} else {
			return nil, fmt.Errorf("profile %s has a server with no domain name or IP address", profileName)
		}
		if len(server.GetPortBindings()) == 0 {
			return nil, fmt.Errorf("profile %s has a server %s with no port bindings", profileName, u.Host)
		}
		q := url.Values{}
		q.Add("profile", profileName)
		if profile.Mtu != nil {
			q.Add("mtu", strconv.Itoa(int(profile.GetMtu())))
		}
		if profile.Multiplexing != nil && profile.Multiplexing.Level != nil {
			q.Add("multiplexing", profile.GetMultiplexing().GetLevel().String())
		}
		for _, binding := range server.GetPortBindings() {
			if binding.GetPortRange() != "" {
				q.Add("port", binding.GetPortRange())
			} else {
				q.Add("port", strconv.Itoa(int(binding.GetPort())))
			}
			q.Add("protocol", binding.GetProtocol().String())
		}
		u.RawQuery = q.Encode()
		urls = append(urls, u.String())
	}
	return
}

// URLToClientConfig returns a client configuration based on the URL.
func URLToClientConfig(s string) (*pb.ClientConfig, error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("url.Parse() failed: %w", err)
	}
	if u.Scheme != "mieru" {
		return nil, fmt.Errorf("unrecognized URL scheme %q", u.Scheme)
	}
	if u.Opaque != "" {
		return nil, fmt.Errorf("URL is opaque")
	}
	b, err := base64.StdEncoding.DecodeString(s[8:]) // Remove "mieru://"
	if err != nil {
		return nil, fmt.Errorf("base64.StdEncoding.DecodeString() failed: %w", err)
	}
	c := &pb.ClientConfig{}
	if err := proto.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("proto.Unmarshal() failed: %w", err)
	}
	return c, nil
}

// URLToClientProfile returns a client profile based on the mieru simple URL.
func URLToClientProfile(s string) (*pb.ClientProfile, error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("url.Parse() failed: %w", err)
	}
	if u.Scheme != "mierus" {
		return nil, fmt.Errorf("unrecognized URL scheme %q", u.Scheme)
	}
	if u.Opaque != "" {
		return nil, fmt.Errorf("URL is opaque")
	}

	p := &pb.ClientProfile{
		User: &pb.User{},
	}
	if u.User == nil {
		return nil, fmt.Errorf("URL has no user info")
	}
	if u.User.Username() == "" {
		return nil, fmt.Errorf("URL has no user name")
	}
	pw, _ := u.User.Password()
	if pw == "" {
		return nil, fmt.Errorf("URL has no password")
	}
	p.User.Name = proto.String(u.User.Username())
	p.User.Password = proto.String(pw)

	if u.Hostname() == "" {
		return nil, fmt.Errorf("URL has no host")
	}
	server := &pb.ServerEndpoint{}
	if net.ParseIP(u.Hostname()) != nil {
		server.IpAddress = proto.String(u.Hostname())
	} else {
		server.DomainName = proto.String(u.Hostname())
	}

	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, fmt.Errorf("url.ParseQuery() failed: %w", err)
	}
	if q.Get("profile") == "" {
		return nil, fmt.Errorf("URL has no profile name")
	}
	p.ProfileName = proto.String(q.Get("profile"))
	if q.Get("mtu") != "" {
		mtu, err := strconv.Atoi(q.Get("mtu"))
		if err != nil {
			return nil, fmt.Errorf("URL has invalid MTU %q", q.Get("mtu"))
		}
		p.Mtu = proto.Int32(int32(mtu))
	}
	if q.Get("multiplexing") != "" {
		level := pb.MultiplexingLevel(pb.MultiplexingLevel_value[q.Get("multiplexing")])
		p.Multiplexing = &pb.MultiplexingConfig{
			Level: &level,
		}
	}

	portList := q["port"]
	protocolList := q["protocol"]
	if len(portList) != len(protocolList) {
		return nil, fmt.Errorf("URL has mismatched number of port and number of protocol")
	}
	for idx, port := range portList {
		portNum, err := strconv.Atoi(port)
		if err != nil {
			portRangeParts := strings.Split(port, "-")
			if len(portRangeParts) != 2 {
				return nil, fmt.Errorf("URL has invalid port or port range %q", port)
			}
			beginPort, err := strconv.Atoi(portRangeParts[0])
			if err != nil {
				return nil, fmt.Errorf("URL has invalid begin of port range %q", portRangeParts[0])
			}
			endPort, err := strconv.Atoi(portRangeParts[1])
			if err != nil {
				return nil, fmt.Errorf("URL has invalid end of port range %q", portRangeParts[1])
			}
			if beginPort < 1 || beginPort > 65535 {
				return nil, fmt.Errorf("URL has invalid begin port number %d", beginPort)
			}
			if endPort < 1 || endPort > 65535 {
				return nil, fmt.Errorf("URL has invalid end port number %d", endPort)
			}
			if beginPort > endPort {
				return nil, fmt.Errorf("URL's begin port number %d is greater than end port number %d", beginPort, endPort)
			}
			server.PortBindings = append(server.PortBindings, &pb.PortBinding{
				PortRange: proto.String(fmt.Sprintf("%d-%d", beginPort, endPort)),
				Protocol:  pb.TransportProtocol(pb.TransportProtocol_value[protocolList[idx]]).Enum(),
			})
		} else {
			if portNum < 1 || portNum > 65535 {
				return nil, fmt.Errorf("URL has invalid port number %d", portNum)
			}
			server.PortBindings = append(server.PortBindings, &pb.PortBinding{
				Port:     proto.Int32(int32(portNum)),
				Protocol: pb.TransportProtocol(pb.TransportProtocol_value[protocolList[idx]]).Enum(),
			})
		}
	}
	p.Servers = append(p.Servers, server)
	return p, nil
}
