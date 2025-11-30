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
	"errors"
	"net"

	apicommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/apis/model"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
)

var (
	ErrNoServerConfig              = errors.New("no server config")
	ErrInvalidServerConfig         = errors.New("invalid server config")
	ErrServerIsNotRunning          = errors.New("server is not running")
	ErrStoreServerConfigAfterStart = errors.New("can't store server config after start")
)

// Server contains methods supported by a proxy server.
type Server interface {
	ServerConfigurationService
	ServerLifecycleService
	ServerNetworkService
}

// ServerConfigurationService contains methods to manage proxy server configuration.
type ServerConfigurationService interface {
	// Load returns the server config.
	// It returns ErrNoServerConfig if server config is never stored.
	Load() (*ServerConfig, error)

	// Store saves the server config.
	// It returns wrapped ErrInvalidServerConfig error
	// if the provided server config is invalid.
	// It returns ErrStoreServerConfigAfterStart error
	// if it is called after start.
	Store(*ServerConfig) error
}

// ServerLifecycleService contains methods to manage proxy server lifecycle.
type ServerLifecycleService interface {
	// Start activates the server with the stored configuration.
	// Calling Start function more than once has undefined behavior.
	Start() error

	// Stop deactivates the server.
	// Established network connections are NOT terminated.
	// After stop, the server can't be reused.
	Stop() error

	// IsRunning returns true if the server has been started
	// and has not been stopped.
	IsRunning() bool
}

type ServerNetworkService interface {
	// Accept accepts a new proxy connection from a client.
	// It returns the proxy connection and the socks5 request sent by the client.
	// Additional handshake is required with the returned proxy connection.
	//
	// The returned proxy connection implements UserContext interface.
	Accept() (net.Conn, *model.Request, error)
}

// ServerConfig stores proxy server configuration.
type ServerConfig struct {
	// Main configuration.
	Config *appctlpb.ServerConfig

	// A listener factory to create stream-oriented network listeners.
	//
	// If this field is not set, a default stream listener factory is used.
	StreamListenerFactory apicommon.StreamListenerFactory

	// A listener factory to create packet-oriented network listeners.
	//
	// If this field is not set, a default packet listener factory is used.
	PacketListenerFactory apicommon.PacketListenerFactory
}

// NewServer creates a blank mieru server with no server config.
func NewServer() Server {
	ms := &mieruServer{}
	ms.initOnce()
	return ms
}
