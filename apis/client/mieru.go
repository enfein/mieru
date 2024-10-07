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
	"sync"
	"time"

	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/apis/model"
	"github.com/enfein/mieru/v3/pkg/appctl"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/protocol"
	"github.com/enfein/mieru/v3/pkg/stderror"
	"github.com/enfein/mieru/v3/pkg/util"
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
	mtu := util.DefaultMTU
	if activeProfile.GetMtu() != 0 {
		mtu = int(activeProfile.GetMtu())
	}
	endpoints := make([]protocol.UnderlayProperties, 0)
	resolver := &util.DNSResolver{}
	for _, serverInfo := range activeProfile.GetServers() {
		var proxyHost string
		var proxyIP net.IP
		if serverInfo.GetDomainName() != "" {
			proxyHost = serverInfo.GetDomainName()
			proxyIP, err = resolver.LookupIP(context.Background(), proxyHost)
			if err != nil {
				return fmt.Errorf(stderror.LookupIPFailedErr, err)
			}
		} else {
			proxyHost = serverInfo.GetIpAddress()
			proxyIP = net.ParseIP(proxyHost)
			if proxyIP == nil {
				return fmt.Errorf(stderror.ParseIPFailed)
			}
		}
		ipVersion := util.GetIPVersion(proxyIP.String())
		portBindings, err := appctl.FlatPortBindings(serverInfo.GetPortBindings())
		if err != nil {
			return fmt.Errorf(stderror.InvalidPortBindingsErr, err)
		}
		for _, bindingInfo := range portBindings {
			proxyPort := bindingInfo.GetPort()
			switch bindingInfo.GetProtocol() {
			case appctlpb.TransportProtocol_TCP:
				endpoint := protocol.NewUnderlayProperties(mtu, ipVersion, util.TCPTransport, nil, &net.TCPAddr{IP: proxyIP, Port: int(proxyPort)})
				endpoints = append(endpoints, endpoint)
			case appctlpb.TransportProtocol_UDP:
				endpoint := protocol.NewUnderlayProperties(mtu, ipVersion, util.UDPTransport, nil, &net.UDPAddr{IP: proxyIP, Port: int(proxyPort)})
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

func (mc *mieruClient) DialContext(ctx context.Context) (net.Conn, error) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	if !mc.running {
		return nil, ErrClientIsNotRunning
	}
	return mc.mux.DialContext(ctx)
}

func (mc *mieruClient) HandshakeWithConnect(ctx context.Context, conn net.Conn, addr model.AddrSpec) error {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	var req bytes.Buffer
	req.Write([]byte{constant.Socks5Version, constant.Socks5ConnectCmd, 0})
	if err := addr.WriteToSocks5(&req); err != nil {
		return err
	}
	if _, err := conn.Write(req.Bytes()); err != nil {
		return fmt.Errorf("failed to write socks5 connection request to the server: %w", err)
	}

	util.SetReadTimeout(conn, 10*time.Second)
	defer func() {
		util.SetReadTimeout(conn, 0)
	}()

	resp := make([]byte, 3)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fmt.Errorf("failed to read socks5 connection response from the server: %w", err)
	}
	var respAddr model.AddrSpec
	if err := respAddr.ReadFromSocks5(conn); err != nil {
		return fmt.Errorf("failed to read socks5 connection address response from the server: %w", err)
	}
	if resp[1] != 0 {
		return fmt.Errorf("server returned socks5 error code %d", resp[1])
	}
	return nil
}
