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
	"math"
	"time"
)

const (
	// RPCTimeout is the timeout to complete a RPC call.
	// We use a very large timeout that works for embedded computer
	// and anti-virus sandbox environment.
	RPCTimeout = time.Second * 10

	// MaxRecvMsgSize is the maximum message size in bytes that
	// can be received in gRPC.
	MaxRecvMsgSize = math.MaxInt32
)
