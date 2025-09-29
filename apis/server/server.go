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
	"sync"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlcommon"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
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
