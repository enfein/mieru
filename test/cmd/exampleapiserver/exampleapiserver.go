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

package main

import (
	"flag"
	"fmt"
	"net"

	"github.com/enfein/mieru/v3/apis/model"
	"github.com/enfein/mieru/v3/apis/server"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"google.golang.org/protobuf/proto"
)

var (
	port         = flag.Int("port", 0, "mieru API server proxy port to listen")
	protocol     = flag.String("protocol", "TCP", "Proxy transport protocol: TCP or UDP")
	username     = flag.String("username", "", "mieru username")
	password     = flag.String("password", "", "mieru password")
	useEarlyConn = flag.Bool("use_early_conn", false, "Reply and payload use the same network packet")
	debug        = flag.Bool("debug", false, "Display debug messages")
)

func main() {
	flag.Parse()
	if *port < 1 || *port > 65535 {
		panic(fmt.Sprintf("port %d is invalid", *port))
	}
	var transportProtocol *appctlpb.TransportProtocol
	switch *protocol {
	case "TCP":
		transportProtocol = appctlpb.TransportProtocol_TCP.Enum()
	case "UDP":
		transportProtocol = appctlpb.TransportProtocol_UDP.Enum()
	default:
		panic(fmt.Sprintf("Transport protocol %q is invalid", *protocol))
	}
	if *username == "" {
		panic("username is not set")
	}
	if *password == "" {
		panic("password is not set")
	}

	s := server.NewServer()
	if err := s.Store(&server.ServerConfig{
		Config: &appctlpb.ServerConfig{
			PortBindings: []*appctlpb.PortBinding{
				{
					Port:     proto.Int32(int32(*port)),
					Protocol: transportProtocol,
				},
			},
			Users: []*appctlpb.User{
				{
					Name:            username,
					Password:        password,
					AllowPrivateIP:  proto.Bool(true),
					AllowLoopbackIP: proto.Bool(true),
				},
			},
		},
	}); err != nil {
		panic(err)
	}
	if _, err := s.Load(); err != nil {
		panic(err)
	}

	if err := s.Start(); err != nil {
		panic(err)
	}
	if !s.IsRunning() {
		panic("server is not running after start")
	}
	fmt.Printf("API server is listening to %s port %d\n", *protocol, *port)

	for {
		conn, req, err := s.Accept()
		if err != nil {
			panic(err)
		}
		handleOneProxyConn(s, conn, req)
	}
}

func handleOneProxyConn(s server.Server, conn net.Conn, req *model.Request) {}
