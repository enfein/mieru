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
	"errors"
	"net"

	apicommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
)

var (
	ErrNoClientConfig              = errors.New("no client config")
	ErrInvalidConfigConfig         = errors.New("invalid client config")
	ErrClientIsNotRunning          = errors.New("client is not running")
	ErrStoreClientConfigAfterStart = errors.New("can't store client config after start")
)

// Client contains methods supported by a mieru client.
type Client interface {
	ClientConfigurationService
	ClientLifecycleService
	ClientNetworkService
}

// ClientConfigurationService contains methods to manage proxy client configuration.
type ClientConfigurationService interface {
	// Load returns the client config.
	// It returns ErrNoClientConfig if client config is never stored.
	Load() (*ClientConfig, error)

	// Store saves the client config.
	// It returns wrapped ErrInvalidConfigConfig error
	// if the provided client config is invalid.
	// It returns ErrStoreClientConfigAfterStart error
	// if it is called after start.
	Store(*ClientConfig) error
}

// ClientLifecycleService contains methods to manage proxy client lifecycle.
type ClientLifecycleService interface {
	// Start activates the client with the stored configuration.
	// Calling Start function more than once has undefined behavior.
	Start() error

	// Stop deactivates the client.
	// Established network connections are NOT terminated.
	// After stop, the client can't be reused.
	Stop() error

	// IsRunning returns true if the client has been started
	// and has not been stopped.
	IsRunning() bool
}

// ClientNetworkService contains methods to establish connections
// to destinations using proxy servers.
type ClientNetworkService interface {
	// DialContext returns a new proxy connection to reach the destination.
	// It uses the dialer in ClientConfig to connect to a proxy server endpoint.
	//
	// This is a streaming based proxy connection. If the destination is a packet
	// endpoint, packets are encapsulated in the streaming connection.
	//
	// It returns an error if the client has not been started,
	// or has been stopped.
	DialContext(context.Context, net.Addr) (net.Conn, error)
}

// ClientConfig stores proxy client configuration.
type ClientConfig struct {
	Profile *appctlpb.ClientProfile

	// A dialer to connect to proxy server via stream-oriented network connections.
	//
	// If this field is not set, a default dialer is used.
	Dialer apicommon.Dialer

	// If set, the resolver translates proxy server domain name into IP addresses.
	//
	// This field is not required, if Dialer is able to do DNS, or proxy server
	// endpoints are IP addresses rather than domain names.
	Resolver apicommon.DNSResolver
}

// NewClient creates a blank mieru client with no client config.
func NewClient() Client {
	mc := &mieruClient{}
	mc.initOnce()
	return mc
}
