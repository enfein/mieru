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
	"github.com/enfein/mieru/pkg/netutil"
)

// Underlay contains methods implemented by a underlay network connection.
type Underlay interface {
	// Layer 2 MTU of this network connection.
	MTU() int

	// The IP version used to establish the underlay.
	IPVersion() netutil.IPVersion

	// The transport protocol used to implement the underlay.
	TransportProtocol() netutil.TransportProtocol

	// Add a session to the underlay connection.
	AddSession(*Session) error

	// Remove a session from the underlay connection.
	RemoveSession(sessionID uint32) error

	// Run input and output loop.
	RunEventLoop() error

	// Close the underlay connection.
	Close() error
}
