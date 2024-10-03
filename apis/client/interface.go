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
	"net"
)

// Client contains methods supported by a mieru client.
type Client interface {
	ClientConfiguration
	ClientLifecycle

	DialContext(ctx context.Context) (net.Conn, error)
}

// ClientConfiguration contains methods to manage proxy client configuration.
type ClientConfiguration interface {
	Load() error
	Store() error
}

// ClientLifecycle contains methods to manage proxy client lifecycle.
type ClientLifecycle interface {
	Start() error
	Stop() error
	IsRunning() bool
}
