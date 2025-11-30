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

package server

import (
	"fmt"
	"net"
	"sync"
	"time"

	apicommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/apis/model"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlcommon"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/protocol"
)

// This package should not depends on github.com/enfein/mieru/v3/pkg/appctl,
// which introduces gRPC dependency.

// mieruServer is the official implementation of mieru server APIs.
type mieruServer struct {
	initTask sync.Once
	mu       sync.RWMutex
	running  bool

	config *ServerConfig
	mux    *protocol.Mux
}

var _ Server = &mieruServer{}

// initOnce should be called when constructing the mieru server.
func (ms *mieruServer) initOnce() {
	ms.initTask.Do(func() {
		// Disable log.
		log.SetFormatter(&log.NilFormatter{})
	})
}

func (ms *mieruServer) Load() (*ServerConfig, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if ms.config == nil {
		return nil, ErrNoServerConfig
	}
	return ms.config, nil
}

func (ms *mieruServer) Store(config *ServerConfig) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if config == nil {
		return fmt.Errorf("%w: server config is nil", ErrInvalidServerConfig)
	}
	if err := validateServerConfig(config.Config); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidServerConfig, err)
	}
	ms.config = config
	return nil
}

func (ms *mieruServer) Start() error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if ms.config == nil {
		return ErrNoServerConfig
	}

	ms.mux = protocol.NewMux(false)
	if ms.config.StreamListenerFactory != nil {
		ms.mux.SetStreamListenerFactory(ms.config.StreamListenerFactory)
	}
	if ms.config.PacketListenerFactory != nil {
		ms.mux.SetPacketListenerFactory(ms.config.PacketListenerFactory)
	}
	ms.mux.SetServerUsers(appctlcommon.UserListToMap(ms.config.Config.GetUsers()))
	mtu := common.DefaultMTU
	if ms.config.Config.GetMtu() != 0 {
		mtu = int(ms.config.Config.GetMtu())
	}
	endpoints, err := appctlcommon.PortBindingsToUnderlayProperties(ms.config.Config.GetPortBindings(), mtu)
	if err != nil {
		return err
	}
	ms.mux.SetEndpoints(endpoints)
	if err := ms.mux.Start(); err != nil {
		return err
	}
	ms.running = true
	return nil
}

func (ms *mieruServer) Stop() error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.running = false
	if ms.mux != nil {
		ms.mux.Close()
	}
	return nil
}

func (ms *mieruServer) IsRunning() bool {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.running
}

func (ms *mieruServer) Accept() (net.Conn, *model.Request, error) {
	conn, err := ms.mux.Accept()
	if err != nil {
		return nil, nil, err
	}
	if _, ok := conn.(apicommon.UserContext); !ok {
		return nil, nil, fmt.Errorf("internal error: connection doesn't implement UserContext interface")
	}

	common.SetReadTimeout(conn, 10*time.Second)
	defer func() {
		common.SetReadTimeout(conn, 0)
	}()
	req := &model.Request{}
	if err := req.ReadFromSocks5(conn); err != nil {
		return nil, nil, err
	}
	return conn, req, nil
}

func validateServerConfig(config *appctlpb.ServerConfig) error {
	if config == nil {
		return fmt.Errorf("server config is nil")
	}
	if len(config.GetPortBindings()) == 0 {
		return fmt.Errorf("server port bindings are not set")
	}
	if _, err := appctlcommon.FlatPortBindings(config.GetPortBindings()); err != nil {
		return err
	}
	if len(config.GetUsers()) == 0 {
		return fmt.Errorf("server users are not set")
	}
	for _, user := range config.GetUsers() {
		if err := appctlcommon.ValidateServerConfigSingleUser(user); err != nil {
			return err
		}
	}
	if config.Egress != nil {
		return fmt.Errorf("egress is not allowed")
	}
	return nil
}
