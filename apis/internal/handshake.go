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

package internal

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/apis/model"
	"github.com/enfein/mieru/v3/pkg/common"
)

// PostDialHandshake completes the handshake after API client is connected to a server.
func PostDialHandshake(conn net.Conn, destination model.NetAddrSpec) (*model.Response, error) {
	req := &model.Request{
		DstAddr: destination.AddrSpec,
	}
	isTCP := strings.HasPrefix(destination.Network(), "tcp")
	isUDP := strings.HasPrefix(destination.Network(), "udp")
	if isTCP {
		req.Command = constant.Socks5ConnectCmd
	} else if isUDP {
		req.Command = constant.Socks5UDPAssociateCmd
	} else {
		return nil, fmt.Errorf("unsupported network type %s", destination.Network())
	}
	if err := req.WriteToSocks5(conn); err != nil {
		return nil, fmt.Errorf("failed to write socks5 connection request to the server: %w", err)
	}

	common.SetReadTimeout(conn, 10*time.Second)
	defer func() {
		common.SetReadTimeout(conn, 0)
	}()
	resp := &model.Response{}
	if err := resp.ReadFromSocks5(conn); err != nil {
		return nil, fmt.Errorf("failed to read socks5 connection response from the server: %w", err)
	}
	if resp.Reply != 0 {
		return nil, fmt.Errorf("server returned socks5 error code %d", resp.Reply)
	}
	return resp, nil
}
