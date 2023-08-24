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
// along with this program.  If not, see <https://www.gnu.org/licenses/>

package protocolv2

import (
	"context"
	"net"

	"github.com/enfein/mieru/pkg/metrics"
	"github.com/enfein/mieru/pkg/netutil"
)

var (
	UnderlayMaxConn         = metrics.RegisterMetric("underlay", "MaxConn")
	UnderlayActiveOpens     = metrics.RegisterMetric("underlay", "ActiveOpens")
	UnderlayPassiveOpens    = metrics.RegisterMetric("underlay", "PassiveOpens")
	UnderlayCurrEstablished = metrics.RegisterMetric("underlay", "CurrEstablished")
	UnderlayMalformedUDP    = metrics.RegisterMetric("underlay", "UnderlayMalformedUDP")
	UnderlayUnsolicitedUDP  = metrics.RegisterMetric("underlay", "UnsolicitedUDP")
)

// UnderlayProperties defines network properties of a underlay.
type UnderlayProperties interface {
	// Layer 2 MTU of this network connection.
	MTU() int

	// The IP version used to establish the underlay.
	IPVersion() netutil.IPVersion

	// The transport protocol used to implement the underlay.
	TransportProtocol() netutil.TransportProtocol

	// Implement the LocalAddr() method in net.Conn interface.
	LocalAddr() net.Addr

	// Implemeent the RemoteAddr() method in the net.Conn interface.
	RemoteAddr() net.Addr
}

// Underlay contains methods implemented by a underlay network connection.
type Underlay interface {
	// Accept incoming sessions.
	net.Listener

	// Store basic network properties.
	UnderlayProperties

	// Add a session to the underlay connection.
	// Optionally, the remote network address can be specified for the session.
	// The session is ready to use when this returns.
	AddSession(*Session, net.Addr) error

	// Remove a session from the underlay connection.
	// The session is destroyed when this returns.
	RemoveSession(*Session) error

	// Run event loop.
	// The underlay needs to be closed when this returns.
	RunEventLoop(context.Context) error

	// Indicate the underlay is closed.
	Done() chan struct{}
}

type underlayDescriptor struct {
	mtu               int
	ipVersion         netutil.IPVersion
	transportProtocol netutil.TransportProtocol
	localAddr         net.Addr
	remoteAddr        net.Addr
}

var _ UnderlayProperties = underlayDescriptor{}

func (d underlayDescriptor) MTU() int {
	return d.mtu
}

func (d underlayDescriptor) IPVersion() netutil.IPVersion {
	return d.ipVersion
}

func (d underlayDescriptor) TransportProtocol() netutil.TransportProtocol {
	return d.transportProtocol
}

func (d underlayDescriptor) LocalAddr() net.Addr {
	return d.localAddr
}

func (d underlayDescriptor) RemoteAddr() net.Addr {
	return d.remoteAddr
}
