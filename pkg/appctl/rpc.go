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
	"runtime"
	"time"
)

var rpcTimeout = time.Millisecond * 1000

func init() {
	if runtime.GOOS == "windows" {
		// When anti-virus software is installed in windows, this application is typically executed
		// in a sandbox environment. In such environment, RPC response can be super slow.
		// Set RPC timeout to 10 seconds in windows system to reduce the frequency of this problem.
		rpcTimeout = time.Millisecond * 10000
	}
}

// RPCTimeout is the timeout to complete a RPC call.
func RPCTimeout() time.Duration {
	return rpcTimeout
}
