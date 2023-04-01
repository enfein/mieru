// Copyright (C) 2021  mieru authors
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

package appctl

import (
	"sync"
)

type AppType int

const (
	UNKNOWN_APP AppType = iota
	CLIENT_APP          // client side application
	SERVER_APP          // server side application
)

var appType = UNKNOWN_APP
var appTypeOnce sync.Once

// SetAppType sets the application type. This method is only effective on the first call.
func SetAppType(t AppType) {
	if t == UNKNOWN_APP {
		panic("can't set AppType to UNKNOWN")
	}
	appTypeOnce.Do(func() {
		appType = t
	})
}

// IsClientApp returns true if our application type is client.
func IsClientApp() bool {
	return appType == CLIENT_APP
}

// IsServerApp returns true if our application type is server.
func IsServerApp() bool {
	return appType == SERVER_APP
}
