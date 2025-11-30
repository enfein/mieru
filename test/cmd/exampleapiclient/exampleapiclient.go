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
	"strconv"

	"github.com/enfein/mieru/v3/apis/client"
	apicommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/apis/model"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/socks5"
	"google.golang.org/protobuf/proto"
)

var (
	port           = flag.Int("port", 0, "mieru API client socks5 port to listen")
	username       = flag.String("username", "", "mieru username")
	password       = flag.String("password", "", "mieru password")
	serverIP       = flag.String("server_ip", "", "IP address of mieru proxy server")
	serverPort     = flag.Int("server_port", 0, "Port number of mieru proxy server")
	serverProtocol = flag.String("server_protocol", "TCP", "Transport protocol: TCP or UDP")
	handshakeMode  = flag.String("handshake_mode", "HANDSHAKE_STANDARD", "Handshake mode: HANDSHAKE_STANDARD or HANDSHAKE_NO_WAIT")
	bypassDNS      = flag.Bool("bypass_dns", false, "Bypass proxy server DNS resolution")
	debug          = flag.Bool("debug", false, "Display debug messages")
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
	var handshakeModeConfig appctlpb.HandshakeMode
	switch *handshakeMode {
	case "HANDSHAKE_STANDARD":
		handshakeModeConfig = appctlpb.HandshakeMode_HANDSHAKE_STANDARD
	case "HANDSHAKE_NO_WAIT":
		handshakeModeConfig = appctlpb.HandshakeMode_HANDSHAKE_NO_WAIT
	default:
		panic(fmt.Sprintf("Handshake mode %q is invalid", *handshakeMode))
	}
	var dnsConfig *apicommon.ClientDNSConfig
	if *bypassDNS {
		dnsConfig = &apicommon.ClientDNSConfig{
			BypassDialerDNS: true,
		}
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
			Mtu:           proto.Int32(1400),
			HandshakeMode: &handshakeModeConfig,
		},
		DNSConfig: dnsConfig,
	}); err != nil {
		panic(fmt.Sprintf("Store() failed: %v", err))
	}
	if _, err := c.Load(); err != nil {
		panic(fmt.Sprintf("Load() failed: %v", err))
	}

	if err := c.Start(); err != nil {
		panic(fmt.Sprintf("Start() failed: %v", err))
	}
	if !c.IsRunning() {
		panic("client is not running after start")
	}

	l, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: *port})
	if err != nil {
		panic(fmt.Sprintf("net.ListenTCP() failed: %v", err))
	}
	fmt.Printf("API client is listening to %v\n", l.Addr())

	for {
		conn, err := l.Accept()
		if err != nil {
			panic(fmt.Sprintf("Accept() failed: %v", err))
		}
		go handleOneSocks5Conn(c, conn)
	}
}

func handleOneSocks5Conn(c client.Client, conn net.Conn) {
	if *debug {
		fmt.Printf("Handling socks5 request from %v\n", conn.RemoteAddr())
	}
	defer conn.Close()

	// Handle socks5 authentication.
	if err := socks5AuthHandshake(conn); err != nil {
		panic(fmt.Sprintf("socks5AuthHandshake() failed: %v", err))
	}

	// Find destination.
	var req model.Request
	if err := req.ReadFromSocks5(conn); err != nil {
		panic(fmt.Sprintf("ReadFromSocks5() failed: %v", err))
	}
	var isTCP, isUDP bool
	switch req.Command {
	case constant.Socks5ConnectCmd:
		isTCP = true
	case constant.Socks5UDPAssociateCmd:
		isUDP = true
	default:
		panic(fmt.Sprintf("Invalid socks5 command %d", req.Command))
	}
	netAddr, err := req.ToNetAddrSpec()
	if err != nil {
		panic(fmt.Sprintf("ToNetAddrSpec() failed: %v", err))
	}
	if *debug {
		fmt.Printf("Destination address: %v\n", req.DstAddr)
	}

	// Dial to proxy server and do handshake.
	ctx := context.Background()
	var proxyConn net.Conn
	proxyConn, err = c.DialContext(ctx, netAddr)
	if err != nil {
		panic(fmt.Sprintf("DialContext() failed: %v", err))
	}
	defer proxyConn.Close()

	// Send the connect response back to the application
	// and start bi-direction copy.
	if isTCP {
		// The actual server bound address can't be collected
		// from the API. Send a fake server bound address back to client.
		resp := &model.Response{
			Reply:    constant.Socks5ReplySuccess,
			BindAddr: netAddr.AddrSpec,
		}
		if err := resp.WriteToSocks5(conn); err != nil {
			panic(fmt.Sprintf("WriteToSocks5() failed: %v", err))
		}

		common.BidiCopy(conn, proxyConn)
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
		udpBindAddr := model.AddrSpec{IP: net.IP{0, 0, 0, 0}, Port: udpPort}
		resp := &model.Response{
			Reply:    constant.Socks5ReplySuccess,
			BindAddr: udpBindAddr,
		}
		if err := resp.WriteToSocks5(conn); err != nil {
			panic(fmt.Sprintf("WriteToSocks5() failed: %v", err))
		}

		tunnel := apicommon.NewPacketOverStreamTunnel(proxyConn)
		socks5.BidiCopyUDP(udpConn, tunnel)
	}
}

func socks5AuthHandshake(conn net.Conn) error {
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
