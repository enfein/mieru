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
	"encoding/hex"
	"fmt"
	"net"
	"strconv"

	apicommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/apis/model"
	"github.com/enfein/mieru/v3/apis/trafficpattern"
	pb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/protocol"
	"github.com/enfein/mieru/v3/pkg/socks5"
	"github.com/enfein/mieru/v3/pkg/stderror"
)

// ValidateClientConfigSingleProfile validates a single client config profile.
//
// It validates:
//   - profile name is not empty
//   - user name is not empty
//   - user has either a password or a hashed password
//   - user name length is not greater than 64 bytes
//   - user password (if set) length is not greater than 64 bytes
//   - user has no quota
//   - it has at least 1 server, and for each server:
//     1. the server has either IP address or domain name
//     2. if set, server's IP address is parsable
//     3. the server has at least 1 port binding, and all port bindings are valid
//   - if set, MTU is valid
//   - if set, traffic pattern is valid
//   - if set, dialer is valid
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
	if len(user.GetName()) > 64 {
		return fmt.Errorf("user name exceeds 64 bytes")
	}
	if user.GetPassword() != "" && len(user.GetPassword()) > 64 {
		return fmt.Errorf("user password exceeds 64 bytes")
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
	if err := trafficpattern.Validate(profile.TrafficPattern); err != nil {
		return fmt.Errorf("invalid traffic pattern: %w", err)
	}
	if err := validateClientProfileDialer(profile); err != nil {
		return err
	}
	return nil
}

func NewClientMuxFromProfile(activeProfile *pb.ClientProfile, dialer apicommon.Dialer, packetDialer apicommon.PacketDialer, resolver apicommon.DNSResolver, dnsConfig *apicommon.ClientDNSConfig) (*protocol.Mux, error) {
	var err error
	mux := protocol.NewMux(true)

	// Construct dialer and packet dialer, which are used to connect to proxy server.
	if profileDialer := activeProfile.GetDialer(); profileDialer != nil {
		if dialer == nil && packetDialer == nil {
			profileSocks5Dialer := newProfileSocks5Dialer(profileDialer)
			dialer = profileSocks5Dialer
			if profileDialer.GetSocks5UDPAssociate() {
				packetDialer = profileSocks5Dialer
			}
		}
	}

	// Set dialer and packet dialer.
	if dialer != nil {
		mux.SetDialer(dialer)
	}
	if packetDialer != nil {
		mux.SetPacketDialer(packetDialer)
	}

	// Set DNS resolver.
	// If DNS resolver is not provided, disable DNS resolution.
	// Connection to a domain name endpoint may fail.
	if resolver == nil {
		resolver = apicommon.NilDNSResolver{}
	}
	mux.SetResolver(resolver)

	// Set client DNS configuration.
	if dnsConfig != nil {
		mux.SetClientDNSConfig(dnsConfig)
	}

	trafficPattern, err := trafficpattern.NewConfig(activeProfile.TrafficPattern)
	if err != nil {
		return nil, err
	}
	mux.SetTrafficPattern(trafficPattern)

	// Set user name and password.
	user := activeProfile.GetUser()
	var hashedPassword []byte
	if user.GetHashedPassword() != "" {
		hashedPassword, err = hex.DecodeString(user.GetHashedPassword())
		if err != nil {
			return nil, fmt.Errorf(stderror.DecodeHashedPasswordFailedErr, err)
		}
	} else {
		hashedPassword = cipher.HashPassword([]byte(user.GetPassword()), []byte(user.GetName()))
	}
	mux = mux.SetClientUserNamePassword(user.GetName(), hashedPassword)

	// Set multiplex factor.
	multiplexFactor := 1
	switch activeProfile.GetMultiplexing().GetLevel() {
	case pb.MultiplexingLevel_MULTIPLEXING_OFF:
		multiplexFactor = 0
	case pb.MultiplexingLevel_MULTIPLEXING_LOW:
		multiplexFactor = 1
	case pb.MultiplexingLevel_MULTIPLEXING_MIDDLE:
		multiplexFactor = 2
	case pb.MultiplexingLevel_MULTIPLEXING_HIGH:
		multiplexFactor = 3
	}
	mux = mux.SetClientMultiplexFactor(multiplexFactor)

	// Set server endpoints.
	endpoints, err := clientEndpointsFromProfile(activeProfile)
	if err != nil {
		return nil, err
	}
	mux.SetEndpoints(endpoints)
	return mux, nil
}

func validateClientProfileDialer(profile *pb.ClientProfile) error {
	dialer := profile.GetDialer()
	if dialer == nil {
		return nil
	}
	if dialer.GetProtocol() != pb.ProxyProtocol_SOCKS5_PROXY_PROTOCOL {
		return fmt.Errorf("client profile dialer protocol %s is not supported", dialer.GetProtocol().String())
	}
	if dialer.GetHost() == "" {
		return fmt.Errorf("client profile dialer host is not set")
	}
	if dialer.GetPort() < 1 || dialer.GetPort() > 65535 {
		return fmt.Errorf("client profile dialer port number %d is invalid", dialer.GetPort())
	}
	auth := dialer.GetSocks5Authentication()
	if auth != nil {
		if auth.GetUser() == "" {
			return fmt.Errorf("client profile dialer socks5 authentication user is not set")
		}
		if auth.GetPassword() == "" {
			return fmt.Errorf("client profile dialer socks5 authentication password is not set")
		}
	}
	return nil
}

func newProfileSocks5Dialer(dialer *pb.ClientDialer) *socks5.ClientDialer {
	var credential *socks5.Credential
	if auth := dialer.GetSocks5Authentication(); auth != nil {
		credential = &socks5.Credential{
			User:     auth.GetUser(),
			Password: auth.GetPassword(),
		}
	}
	return socks5.NewClientDialer(net.JoinHostPort(dialer.GetHost(), strconv.Itoa(int(dialer.GetPort()))), credential, dialer.GetSocks5UDPAssociate())
}

func clientEndpointsFromProfile(activeProfile *pb.ClientProfile) ([]protocol.UnderlayProperties, error) {
	mtu := common.DefaultMTU
	if activeProfile.GetMtu() != 0 {
		mtu = int(activeProfile.GetMtu())
	}
	endpoints := make([]protocol.UnderlayProperties, 0)
	for _, serverInfo := range activeProfile.GetServers() {
		var proxyHost string
		var proxyIP net.IP
		if serverInfo.GetDomainName() != "" {
			proxyHost = serverInfo.GetDomainName()
		} else {
			proxyHost = serverInfo.GetIpAddress()
			proxyIP = net.ParseIP(proxyHost)
			if proxyIP == nil {
				return nil, fmt.Errorf(stderror.ParseIPFailed)
			}
		}
		portBindings, err := FlatPortBindings(serverInfo.GetPortBindings())
		if err != nil {
			return nil, fmt.Errorf(stderror.InvalidPortBindingsErr, err)
		}
		for _, bindingInfo := range portBindings {
			proxyPort := bindingInfo.GetPort()
			var endpoint protocol.UnderlayProperties
			switch bindingInfo.GetProtocol() {
			case pb.TransportProtocol_TCP:
				if proxyIP != nil {
					endpoint = protocol.NewUnderlayProperties(mtu, common.StreamTransport, nil,
						&net.TCPAddr{IP: proxyIP, Port: int(proxyPort)},
					)
				} else {
					endpoint = protocol.NewUnderlayProperties(mtu, common.StreamTransport, nil,
						&model.NetAddrSpec{Net: "tcp", AddrSpec: model.AddrSpec{FQDN: proxyHost, Port: int(proxyPort)}},
					)
				}
			case pb.TransportProtocol_UDP:
				if proxyIP != nil {
					endpoint = protocol.NewUnderlayProperties(mtu, common.PacketTransport, nil,
						&net.UDPAddr{IP: proxyIP, Port: int(proxyPort)},
					)
				} else {
					endpoint = protocol.NewUnderlayProperties(mtu, common.PacketTransport, nil,
						&model.NetAddrSpec{Net: "udp", AddrSpec: model.AddrSpec{FQDN: proxyHost, Port: int(proxyPort)}},
					)
				}
			default:
				return nil, fmt.Errorf(stderror.InvalidTransportProtocol)
			}
			if endpoint != nil {
				endpoints = append(endpoints, endpoint)
			}
		}
	}
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("no server listening endpoint found")
	}
	return endpoints, nil
}
