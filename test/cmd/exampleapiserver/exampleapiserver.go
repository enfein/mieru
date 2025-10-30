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
	"strconv"

	apicommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/apis/model"
	"github.com/enfein/mieru/v3/apis/server"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/socks5"
	"google.golang.org/protobuf/proto"
)

var (
	port     = flag.Int("port", 0, "mieru API server proxy port to listen")
	protocol = flag.String("protocol", "TCP", "Proxy transport protocol: TCP or UDP")
	username = flag.String("username", "", "mieru username")
	password = flag.String("password", "", "mieru password")
	debug    = flag.Bool("debug", false, "Display debug messages")
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
		panic(fmt.Sprintf("Store() failed: %v", err))
	}
	if _, err := s.Load(); err != nil {
		panic(fmt.Sprintf("Load() failed: %v", err))
	}

	if err := s.Start(); err != nil {
		panic(fmt.Sprintf("Start() failed: %v", err))
	}
	if !s.IsRunning() {
		panic("server is not running after start")
	}
	fmt.Printf("API server is listening to %s port %d\n", *protocol, *port)

	for {
		proxyConn, req, err := s.Accept()
		if err != nil {
			panic(fmt.Sprintf("Accept() failed: %v", err))
		}
		handleOneProxyConn(proxyConn, req)
	}
}

func handleOneProxyConn(proxyConn net.Conn, req *model.Request) {
	if *debug {
		fmt.Printf("Received %v\n", req)
	}
	defer proxyConn.Close()

	var isTCP, isUDP bool
	switch req.Command {
	case constant.Socks5ConnectCmd:
		isTCP = true
	case constant.Socks5UDPAssociateCmd:
		isUDP = true
	default:
		panic(fmt.Sprintf("Invalid socks5 command %d", req.Command))
	}

	if isTCP {
		target, err := net.Dial("tcp", req.DstAddr.String())
		if err != nil {
			panic(fmt.Sprintf("net.Dial() failed: %v", err))
		}
		defer target.Close()

		local := target.LocalAddr().(*net.TCPAddr)
		bind := model.AddrSpec{IP: local.IP, Port: local.Port}
		resp := &model.Response{
			Reply:    constant.Socks5ReplySuccess,
			BindAddr: bind,
		}
		if err := resp.WriteToSocks5(proxyConn); err != nil {
			panic(fmt.Sprintf("WriteToSocks5() failed: %v", err))
		}
		if *debug {
			fmt.Printf("Sent %v\n", resp)
		}

		common.BidiCopy(proxyConn, target)
	} else if isUDP {
		// Create a UDP listener on a random port.
		udpConn, err := net.ListenUDP("udp", nil)
		if err != nil {
			panic(fmt.Sprintf("net.ListenUDP() failed: %v", err))
		}
		defer udpConn.Close()

		_, udpPortStr, err := net.SplitHostPort(udpConn.LocalAddr().String())
		if err != nil {
			panic(fmt.Sprintf("net.SplitHostPort() failed: %v", err))
		}
		udpPort, err := strconv.Atoi(udpPortStr)
		if err != nil {
			panic(fmt.Sprintf("strconv.Atoi() failed: %v", err))
		}
		bind := model.AddrSpec{IP: net.IP{0, 0, 0, 0}, Port: udpPort}
		resp := &model.Response{
			Reply:    constant.Socks5ReplySuccess,
			BindAddr: bind,
		}
		if err := resp.WriteToSocks5(proxyConn); err != nil {
			panic(fmt.Sprintf("WriteToSocks5() failed: %v", err))
		}
		if *debug {
			fmt.Printf("Sent %v\n", resp)
		}

		tunnel := apicommon.NewPacketOverStreamTunnel(proxyConn)
		socks5.RunUDPAssociateLoop(udpConn, tunnel, &net.Resolver{})
	}
}
