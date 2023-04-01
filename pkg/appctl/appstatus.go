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
	"sync/atomic"

	pb "github.com/enfein/mieru/pkg/appctl/appctlpb"
)

// currentAppStatus stores the current application running status.
var currentAppStatus atomic.Value

// GetAppStatus returns the application running status.
func GetAppStatus() pb.AppStatus {
	value := currentAppStatus.Load()
	if value == nil {
		return pb.AppStatus_UNKNOWN
	}
	return value.(pb.AppStatus)
}

// SetAppStatus sets the application running status.
func SetAppStatus(status pb.AppStatus) {
	if status == pb.AppStatus_UNKNOWN {
		panic("can't set app status to UNKNOWN")
	}
	currentAppStatus.Store(status)
}
