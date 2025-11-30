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
	"context"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/apis/internal"
	"github.com/enfein/mieru/v3/apis/model"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlcommon"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/protocol"
)

// This package should not depends on github.com/enfein/mieru/v3/pkg/appctl,
// which introduces gRPC dependency.

// mieruClient is the official implementation of mieru client APIs.
type mieruClient struct {
	initTask sync.Once
	mu       sync.RWMutex
	running  bool

	config *ClientConfig
	mux    *protocol.Mux
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
		return fmt.Errorf("%w: client config is nil", ErrInvalidClientConfig)
	}
	if config.Profile == nil {
		return fmt.Errorf("%w: client config profile is nil", ErrInvalidClientConfig)
	}
	if mc.running {
		return ErrStoreClientConfigAfterStart
	}
	if err := appctlcommon.ValidateClientConfigSingleProfile(config.Profile); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidClientConfig, err.Error())
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

	mux, err := appctlcommon.NewClientMuxFromProfile(mc.config.Profile, mc.config.Dialer, mc.config.PacketDialer, mc.config.Resolver, mc.config.DNSConfig)
	if err != nil {
		return err
	}
	mc.mux = mux
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
	if !strings.HasPrefix(netAddrSpec.Network(), "tcp") && !strings.HasPrefix(netAddrSpec.Network(), "udp") {
		return nil, fmt.Errorf("unsupported network type %s", netAddrSpec.Network())
	}

	conn, err := mc.mux.DialContext(ctx)
	if err != nil {
		return nil, err
	}
	if mc.config.Profile.GetHandshakeMode() == appctlpb.HandshakeMode_HANDSHAKE_NO_WAIT {
		req := &model.Request{}
		if strings.HasPrefix(netAddrSpec.Network(), "tcp") {
			req.Command = constant.Socks5ConnectCmd
		} else if strings.HasPrefix(netAddrSpec.Network(), "udp") {
			req.Command = constant.Socks5UDPAssociateCmd
		} else {
			return nil, fmt.Errorf("unsupported network type %s", netAddrSpec.Network())
		}
		req.DstAddr = netAddrSpec.AddrSpec
		earlyConn := internal.NewEarlyConn(conn)
		earlyConn.SetRequest(req)
		return earlyConn, nil
	}
	_, err = internal.PostDialHandshake(conn, netAddrSpec)
	return conn, err
}
