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

package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net"

	"github.com/enfein/mieru/v3/apis/client"
	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/apis/model"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/util"
	"google.golang.org/protobuf/proto"
)

var (
	port           = flag.Int("port", 0, "mieru API client socks5 port")
	username       = flag.String("username", "", "mieru username")
	password       = flag.String("password", "", "mieru password")
	serverIP       = flag.String("server_ip", "", "IP address of mieru proxy server")
	serverPort     = flag.Int("server_port", 0, "Port number of mieru proxy server")
	serverProtocol = flag.String("server_protocol", "", "Transport protocol: TCP or UDP")
)

func main() {
	flag.Parse()
	if *port < 1 || *port > 65535 {
		panic(fmt.Sprintf("port %d is invalid", *port))
	}
	if *username == "" {
		panic("username is not set")
	}
	if *password == "" {
		panic("password is not set")
	}
	if *serverIP == "" {
		panic("server_ip is not set")
	}
	if net.ParseIP(*serverIP) == nil {
		panic(fmt.Sprintf("Failed to parse server_ip %q", *serverIP))
	}
	if *serverPort < 1 || *serverPort > 65535 {
		panic(fmt.Sprintf("server_port %d is invalid", *serverPort))
	}
	var transportProtocol *appctlpb.TransportProtocol
	switch *serverProtocol {
	case "TCP":
		transportProtocol = appctlpb.TransportProtocol_TCP.Enum()
	case "UDP":
		transportProtocol = appctlpb.TransportProtocol_UDP.Enum()
	default:
		panic(fmt.Sprintf("Transport protocol %q is invalid", *serverProtocol))
	}

	c := client.NewClient()
	if err := c.Store(&client.ClientConfig{
		Profile: &appctlpb.ClientProfile{
			ProfileName: proto.String("api"),
			User: &appctlpb.User{
				Name:     username,
				Password: password,
			},
			Servers: []*appctlpb.ServerEndpoint{
				{
					IpAddress: serverIP,
					PortBindings: []*appctlpb.PortBinding{
						{
							Port:     proto.Int32(int32(*serverPort)),
							Protocol: transportProtocol,
						},
					},
				},
			},
			Mtu: proto.Int32(1400),
		},
	}); err != nil {
		panic(err)
	}
	if _, err := c.Load(); err != nil {
		panic(err)
	}

	if err := c.Start(); err != nil {
		panic(err)
	}
	if !c.IsRunning() {
		panic("client is not running after start")
	}

	l, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: *port})
	if err != nil {
		panic(err)
	}
	for {
		conn, err := l.Accept()
		if err != nil {
			panic(err)
		}
		go handleOneSocks5Conn(c, conn)
	}
}

func handleOneSocks5Conn(c client.Client, conn net.Conn) {
	defer conn.Close()

	// Handle socks5 authentication.
	if err := socks5ClientHandshake(conn); err != nil {
		panic(fmt.Sprintf("socks5ClientHandshake() failed: %v", err))
	}

	// Find destination.
	socks5Header := make([]byte, 3)
	if _, err := io.ReadFull(conn, socks5Header); err != nil {
		panic(fmt.Sprintf("Read socks5 header failed: %v", err))
	}
	addr := model.AddrSpec{}
	if err := addr.ReadFromSocks5(conn); err != nil {
		panic(fmt.Sprintf("ReadFromSocks5() failed: %v", err))
	}

	// Dial to proxy server and do handshake.
	ctx := context.Background()
	proxyConn, err := c.DialContext(ctx)
	if err != nil {
		panic(fmt.Sprintf("DialContext() failed: %v", err))
	}
	defer proxyConn.Close()
	if err := c.HandshakeWithConnect(ctx, proxyConn, addr); err != nil {
		panic(fmt.Sprintf("HandshakeWithConnect() failed: %v", err))
	}

	// Send the connect response back to the application.
	var resp bytes.Buffer
	resp.Write([]byte{constant.Socks5Version, 0, 0})
	if err := addr.WriteToSocks5(&resp); err != nil {
		panic(fmt.Sprintf("WriteToSocks5() failed: %v", err))
	}
	if _, err := conn.Write(resp.Bytes()); err != nil {
		panic(fmt.Sprintf("Write socks5 response failed: %v", err))
	}

	// Exchange payload.
	util.BidiCopy(conn, proxyConn)
}

func socks5ClientHandshake(conn net.Conn) error {
	// Only accept socks5 with no authentication.
	socks5Header := make([]byte, 3)
	if _, err := io.ReadFull(conn, socks5Header); err != nil {
		return err
	}
	wantHeader := []byte{constant.Socks5Version, 1, constant.Socks5NoAuth}
	if !bytes.Equal(socks5Header, wantHeader) {
		return fmt.Errorf("got socks5 header %v, want %v", socks5Header, wantHeader)
	}
	if _, err := conn.Write([]byte{constant.Socks5Version, constant.Socks5AuthSuccess}); err != nil {
		return err
	}
	return nil
}
