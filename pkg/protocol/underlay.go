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

package protocol

import (
	"context"
	"net"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/metrics"
)

var (
	UnderlayMaxConn         = metrics.RegisterMetric("underlay", "MaxConn", metrics.GAUGE)
	UnderlayActiveOpens     = metrics.RegisterMetric("underlay", "ActiveOpens", metrics.COUNTER)
	UnderlayPassiveOpens    = metrics.RegisterMetric("underlay", "PassiveOpens", metrics.COUNTER)
	UnderlayCurrEstablished = metrics.RegisterMetric("underlay", "CurrEstablished", metrics.GAUGE)
	UnderlayMalformedUDP    = metrics.RegisterMetric("underlay", "UnderlayMalformedUDP", metrics.COUNTER)
	UnderlayUnsolicitedUDP  = metrics.RegisterMetric("underlay", "UnsolicitedUDP", metrics.COUNTER)
)

// UnderlayProperties defines network properties of a underlay.
type UnderlayProperties interface {
	// Maximum transission unit of this network connection
	// in the current network layer.
	MTU() int

	// The transport protocol used to implement the underlay.
	TransportProtocol() common.TransportProtocol

	// LocalAddr implements net.Conn interface.
	LocalAddr() net.Addr

	// RemoteAddr implements net.Conn interface.
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

	// Returns the number of sessions.
	SessionCount() int

	// Returns detailed information of all the sessions.
	SessionInfos() []*appctlpb.SessionInfo

	// Number of bytes received from the network.
	InBytes() int64

	// Number of bytes sent to the network.
	OutBytes() int64

	// Run event loop.
	// The underlay needs to be closed when this returns.
	RunEventLoop(context.Context) error

	// Return the schedule controller.
	Scheduler() *ScheduleController

	// Indicate the underlay is closed.
	Done() chan struct{}
}

// underlayDescriptor implements UnderlayProperties.
type underlayDescriptor struct {
	mtu               int
	transportProtocol common.TransportProtocol
	localAddr         net.Addr
	remoteAddr        net.Addr
}

var _ UnderlayProperties = &underlayDescriptor{}

func (d *underlayDescriptor) MTU() int {
	return d.mtu
}

func (d *underlayDescriptor) TransportProtocol() common.TransportProtocol {
	return d.transportProtocol
}

func (d *underlayDescriptor) LocalAddr() net.Addr {
	return d.localAddr
}

func (d *underlayDescriptor) RemoteAddr() net.Addr {
	return d.remoteAddr
}

// NewUnderlayProperties creates a new instance of UnderlayProperties.
func NewUnderlayProperties(mtu int, transportProtocol common.TransportProtocol, localAddr net.Addr, remoteAddr net.Addr) UnderlayProperties {
	d := &underlayDescriptor{
		mtu:               mtu,
		transportProtocol: transportProtocol,
		localAddr:         localAddr,
		remoteAddr:        remoteAddr,
	}
	if localAddr == nil {
		d.localAddr = common.NilNetAddr()
	}
	if remoteAddr == nil {
		d.remoteAddr = common.NilNetAddr()
	}
	return d
}
