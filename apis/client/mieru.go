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

package client

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	apicommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/apis/model"
	"github.com/enfein/mieru/v3/pkg/appctl"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/protocol"
	"github.com/enfein/mieru/v3/pkg/stderror"
)

// mieruClient is the official implementation of mieru client APIs.
type mieruClient struct {
	initTask sync.Once
	mu       sync.RWMutex

	config *ClientConfig
	mux    *protocol.Mux

	running bool
}

var _ Client = &mieruClient{}

// initOnce should be called when constructing the mieru client.
func (mc *mieruClient) initOnce() {
	mc.initTask.Do(func() {
		// Disable log.
		log.SetFormatter(&log.NilFormatter{})
	})
}

func (mc *mieruClient) Load() (*ClientConfig, error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if mc.config == nil {
		return nil, ErrNoClientConfig
	}
	return mc.config, nil
}

func (mc *mieruClient) Store(config *ClientConfig) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if config == nil {
		return fmt.Errorf("%w: client config is nil", ErrInvalidConfigConfig)
	}
	if config.Profile == nil {
		return fmt.Errorf("%w: client config profile is nil", ErrInvalidConfigConfig)
	}
	if mc.running {
		return ErrStoreClientConfigAfterStart
	}
	if err := appctl.ValidateClientConfigSingleProfile(config.Profile); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidConfigConfig, err.Error())
	}
	mc.config = config
	return nil
}

func (mc *mieruClient) Start() error {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if mc.config == nil {
		return ErrNoClientConfig
	}

	var err error
	mc.mux = protocol.NewMux(true)
	activeProfile := mc.config.Profile

	// Set DNS resolver.
	var resolver apicommon.DNSResolver
	if mc.config.Resolver != nil {
		resolver = mc.config.Resolver
	} else {
		resolver = &net.Resolver{} // Default DNS resolver.
	}
	mc.mux.SetResolver(resolver)

	// Set user name and password.
	user := activeProfile.GetUser()
	var hashedPassword []byte
	if user.GetHashedPassword() != "" {
		hashedPassword, err = hex.DecodeString(user.GetHashedPassword())
		if err != nil {
			return fmt.Errorf(stderror.DecodeHashedPasswordFailedErr, err)
		}
	} else {
		hashedPassword = cipher.HashPassword([]byte(user.GetPassword()), []byte(user.GetName()))
	}
	mc.mux = mc.mux.SetClientUserNamePassword(user.GetName(), hashedPassword)

	// Set multiplex factor.
	multiplexFactor := 1
	switch activeProfile.GetMultiplexing().GetLevel() {
	case appctlpb.MultiplexingLevel_MULTIPLEXING_OFF:
		multiplexFactor = 0
	case appctlpb.MultiplexingLevel_MULTIPLEXING_LOW:
		multiplexFactor = 1
	case appctlpb.MultiplexingLevel_MULTIPLEXING_MIDDLE:
		multiplexFactor = 2
	case appctlpb.MultiplexingLevel_MULTIPLEXING_HIGH:
		multiplexFactor = 3
	}
	mc.mux = mc.mux.SetClientMultiplexFactor(multiplexFactor)

	// Set server endpoints.
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
			proxyIPs, err := resolver.LookupIP(context.Background(), "ip", proxyHost)
			if err != nil {
				return fmt.Errorf(stderror.LookupIPFailedErr, err)
			}
			if len(proxyIPs) == 0 {
				return fmt.Errorf(stderror.IPAddressNotFound, proxyHost)
			}
			proxyIP = proxyIPs[0]
		} else {
			proxyHost = serverInfo.GetIpAddress()
			proxyIP = net.ParseIP(proxyHost)
			if proxyIP == nil {
				return fmt.Errorf(stderror.ParseIPFailed)
			}
		}
		portBindings, err := appctl.FlatPortBindings(serverInfo.GetPortBindings())
		if err != nil {
			return fmt.Errorf(stderror.InvalidPortBindingsErr, err)
		}
		for _, bindingInfo := range portBindings {
			proxyPort := bindingInfo.GetPort()
			switch bindingInfo.GetProtocol() {
			case appctlpb.TransportProtocol_TCP:
				endpoint := protocol.NewUnderlayProperties(mtu, common.StreamTransport, nil, &net.TCPAddr{IP: proxyIP, Port: int(proxyPort)})
				endpoints = append(endpoints, endpoint)
			case appctlpb.TransportProtocol_UDP:
				endpoint := protocol.NewUnderlayProperties(mtu, common.PacketTransport, nil, &net.UDPAddr{IP: proxyIP, Port: int(proxyPort)})
				endpoints = append(endpoints, endpoint)
			default:
				return fmt.Errorf(stderror.InvalidTransportProtocol)
			}
		}
	}
	mc.mux.SetEndpoints(endpoints)

	mc.running = true
	return nil
}

func (mc *mieruClient) Stop() error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.running = false
	if mc.mux != nil {
		mc.mux.Close()
	}
	return nil
}

func (mc *mieruClient) IsRunning() bool {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.running
}

func (mc *mieruClient) DialContext(ctx context.Context, addr net.Addr) (net.Conn, error) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	if !mc.running {
		return nil, ErrClientIsNotRunning
	}

	// Check destination address.
	var netAddrSpec model.NetAddrSpec
	if err := netAddrSpec.From(addr); err != nil {
		return nil, fmt.Errorf("invalid destination address: %w", err)
	}
	if !strings.HasPrefix(netAddrSpec.Network(), "tcp") {
		return nil, fmt.Errorf("only tcp network is supported")
	}

	conn, err := mc.mux.DialContext(ctx)
	if err != nil {
		return nil, err
	}
	return mc.dialPostHandshake(conn, netAddrSpec)
}

func (mc *mieruClient) DialContextWithConn(ctx context.Context, conn net.Conn, addr net.Addr) (net.Conn, error) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	if !mc.running {
		return nil, ErrClientIsNotRunning
	}

	// Check destination address.
	var netAddrSpec model.NetAddrSpec
	if err := netAddrSpec.From(addr); err != nil {
		return nil, fmt.Errorf("invalid destination address: %w", err)
	}
	if !strings.HasPrefix(netAddrSpec.Network(), "tcp") {
		return nil, fmt.Errorf("only tcp network is supported")
	}

	subConn, err := mc.mux.DialContextWithConn(ctx, conn)
	if err != nil {
		return nil, err
	}
	return mc.dialPostHandshake(subConn, netAddrSpec)
}

func (mc *mieruClient) dialPostHandshake(conn net.Conn, netAddrSpec model.NetAddrSpec) (net.Conn, error) {
	var req bytes.Buffer
	req.Write([]byte{constant.Socks5Version, constant.Socks5ConnectCmd, 0})
	if err := netAddrSpec.WriteToSocks5(&req); err != nil {
		return nil, err
	}
	if _, err := conn.Write(req.Bytes()); err != nil {
		return nil, fmt.Errorf("failed to write socks5 connection request to the server: %w", err)
	}

	common.SetReadTimeout(conn, 10*time.Second)
	defer func() {
		common.SetReadTimeout(conn, 0)
	}()

	resp := make([]byte, 3)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return nil, fmt.Errorf("failed to read socks5 connection response from the server: %w", err)
	}
	var respAddr model.NetAddrSpec
	if err := respAddr.ReadFromSocks5(conn); err != nil {
		return nil, fmt.Errorf("failed to read socks5 connection address response from the server: %w", err)
	}
	if resp[1] != 0 {
		return nil, fmt.Errorf("server returned socks5 error code %d", resp[1])
	}
	return conn, nil
}
