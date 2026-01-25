package socks5

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	apicommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/egress"
	"github.com/enfein/mieru/v3/pkg/stderror"
	"github.com/enfein/mieru/v3/pkg/testtool"
)

func TestHandleConnect(t *testing.T) {
	// Create a local listener as the destination target.
	dst, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	go func() {
		conn, err := dst.Accept()
		if err != nil {
			t.Errorf("Accept() failed: %v", err)
			return
		}

		buf := make([]byte, 4)
		if _, err := io.ReadFull(conn, buf); err != nil {
			t.Errorf("io.ReadFull() failed: %v", err)
			return
		}

		want := []byte("ping")
		if !bytes.Equal(buf, want) {
			t.Errorf("got %v, want %v", buf, want)
			return
		}
		conn.Write([]byte("pong"))
	}()
	dstAddr := dst.Addr().(*net.TCPAddr)

	// Create a socks server.
	s := &Server{
		config: &Config{
			AllowLoopbackDestination: true,
		},
	}

	// Create the connect request.
	clientConn, serverConn := testtool.BufPipe()
	defer serverConn.Close()
	defer clientConn.Close()

	clientConn.Write([]byte{constant.Socks5Version, constant.Socks5ConnectCmd, 0, constant.Socks5IPv4Address, 127, 0, 0, 1})
	port := []byte{0, 0}
	binary.BigEndian.PutUint16(port, uint16(dstAddr.Port))
	clientConn.Write(port)
	clientConn.Write([]byte("ping"))

	// Socks server handles the request.
	go func() {
		req, err := s.readRequest(serverConn)
		if err != nil {
			t.Errorf("NewRequest() failed: %v", err)
		}
		if err := s.handleRequest(context.Background(), req, serverConn); err != nil && !stderror.IsEOF(err) && !stderror.IsClosed(err) {
			t.Errorf("handleRequest() failed: %v", err)
		}
	}()
	time.Sleep(100 * time.Millisecond)

	// Verify response from socks server.
	out := make([]byte, 14)
	if _, err := io.ReadFull(clientConn, out); err != nil {
		t.Fatalf("io.ReadFull() failed: %v", err)
	}
	want := []byte{
		constant.Socks5Version, constant.Socks5ReplySuccess, 0, constant.Socks5IPv4Address,
		127, 0, 0, 1,
		0, 0,
		'p', 'o', 'n', 'g',
	}

	// Ignore the port number before comparing the result.
	out[8] = 0
	out[9] = 0

	if !bytes.Equal(out, want) {
		t.Fatalf("got %v, want %v", out, want)
	}
}

func TestHandleBind(t *testing.T) {
	testcases := []struct {
		req  []byte
		resp []byte
	}{
		{
			[]byte{5, constant.Socks5BindCmd, 0, 1, 127, 0, 0, 1, 0, 1},
			[]byte{5, constant.Socks5ReplyCommandNotSupported, 0, 1, 0, 0, 0, 0, 0, 0},
		},
	}

	// Create a socks server.
	s := &Server{
		config: &Config{
			AllowLoopbackDestination: true,
		},
	}

	for _, tc := range testcases {
		errCnt := UnsupportedCommandErrors.Load()

		// Create the connect request.
		clientConn, serverConn := testtool.BufPipe()
		clientConn.Write(tc.req)

		// Socks server handles the request.
		req, err := s.readRequest(serverConn)
		if err != nil {
			t.Fatalf("NewRequest() failed: %v", err)
		}
		if err := s.handleRequest(context.Background(), req, serverConn); err != nil {
			t.Fatalf("handleRequest() failed: %v", err)
		}

		// Verify response from socks server.
		out := make([]byte, len(tc.resp))
		clientConn.Read(out)
		if !bytes.Equal(out, tc.resp) {
			t.Errorf("got %v, want %v", out, tc.resp)
		}
		if UnsupportedCommandErrors.Load() <= errCnt {
			t.Errorf("UnsupportedCommandErrors value is not changed")
		}
	}
}

func TestHandleAssociate(t *testing.T) {
	// Create a local UDP listener as the destination target.
	udpServerAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.ResolveUDPAddr() failed: %v", err)
	}
	udpServer, err := net.ListenUDP("udp", udpServerAddr)
	if err != nil {
		t.Fatalf("net.ListenUDP() failed: %v", err)
	}
	defer udpServer.Close()
	udpServerAddr, err = net.ResolveUDPAddr("udp", udpServer.LocalAddr().String())
	if err != nil {
		t.Fatalf("net.ResolveUDPAddr() failed: %v", err)
	}
	udpServerPort := udpServerAddr.Port

	// The UDP server reads a single UDP packet "ping" and responds with "pong".
	go func() {
		buf := make([]byte, 4)
		_, addr, err := udpServer.ReadFrom(buf)
		if err != nil {
			t.Errorf("ReadFrom() failed: %v", err)
			return
		}

		want := []byte("ping")
		if !bytes.Equal(buf, want) {
			t.Errorf("got %v, want %v", buf, want)
			return
		}
		if _, err := udpServer.WriteTo([]byte("pong"), addr); err != nil {
			t.Errorf("WriteTo() failed: %v", err)
		}
	}()

	// Create a socks server.
	s := &Server{
		config: &Config{
			AllowLoopbackDestination: true,
			Resolver:                 &net.Resolver{},
		},
	}

	clientConn, serverConn := testtool.BufPipe()
	defer serverConn.Close()
	defer clientConn.Close()

	// Create UDP Associate request.
	clientConn.Write([]byte{
		constant.Socks5Version, constant.Socks5UDPAssociateCmd, 0, constant.Socks5IPv4Address,
		127, 0, 0, 1,
		byte(udpServerPort >> 8), byte(udpServerPort),
	})

	// Socks server handles the request.
	go func() {
		req, err := s.readRequest(serverConn)
		if err != nil {
			t.Errorf("readRequest() failed: %v", err)
			return
		}
		if err := s.handleRequest(context.Background(), req, serverConn); err != nil && !stderror.IsEOF(err) && !stderror.IsClosed(err) {
			t.Errorf("handleRequest() failed: %v", err)
		}
	}()
	time.Sleep(100 * time.Millisecond)

	// Verify response from socks server.
	out := make([]byte, 10)
	if _, err := io.ReadFull(clientConn, out); err != nil {
		t.Fatalf("io.ReadFull() failed: %v", err)
	}
	want := []byte{
		constant.Socks5Version, constant.Socks5ReplySuccess, 0, constant.Socks5IPv4Address,
		0, 0, 0, 0,
		0, 0,
	}

	// Verify the port is non-zero (a valid port was assigned).
	port := binary.BigEndian.Uint16(out[8:10])
	if port == 0 {
		t.Errorf("expected non-zero port, got 0")
	}

	// Ignore the port number before comparing the result.
	out[8] = 0
	out[9] = 0
	if !bytes.Equal(out, want) {
		t.Fatalf("got %v, want %v", out, want)
	}

	// Send UDP packet "ping".
	wrappedConn := apicommon.NewPacketOverStreamTunnel(clientConn)
	udpReq := bytes.NewBuffer(nil)
	udpReq.Write([]byte{0, 0, 0, constant.Socks5IPv4Address, 127, 0, 0, 1})
	udpReq.WriteByte(byte(udpServerPort >> 8))
	udpReq.WriteByte(byte(udpServerPort))
	udpReq.Write([]byte("ping"))
	if _, err := wrappedConn.Write(udpReq.Bytes()); err != nil {
		t.Fatalf("Write() failed: %v", err)
	}

	// Verify UDP server responds with "pong".
	wantUDP := append([]byte{0, 0, 0, constant.Socks5IPv4Address, 127, 0, 0, 1, byte(udpServerPort >> 8), byte(udpServerPort)}, []byte("pong")...)
	outUDP := make([]byte, len(wantUDP))
	if _, err := io.ReadFull(wrappedConn, outUDP); err != nil {
		t.Fatalf("io.ReadFull() failed: %v", err)
	}
	if !bytes.Equal(outUDP, wantUDP) {
		t.Fatalf("got %v, want %v", outUDP, wantUDP)
	}
}

func TestHandleForwardingTCP(t *testing.T) {
	// Create a local listener as the destination target.
	dst, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	defer dst.Close()
	go func() {
		conn, err := dst.Accept()
		if err != nil {
			t.Errorf("Accept() failed: %v", err)
			return
		}
		defer conn.Close()

		buf := make([]byte, 4)
		if _, err := io.ReadFull(conn, buf); err != nil {
			t.Errorf("io.ReadFull() failed: %v", err)
			return
		}

		want := []byte("ping")
		if !bytes.Equal(buf, want) {
			t.Errorf("got %v, want %v", buf, want)
			return
		}
		conn.Write([]byte("pong"))
	}()
	dstAddr := dst.Addr().(*net.TCPAddr)

	// Create a downstream socks5 server that will connect to the destination.
	downstreamListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	defer downstreamListener.Close()
	downstreamAddr := downstreamListener.Addr().(*net.TCPAddr)

	downstreamServer := &Server{
		config: &Config{
			AllowLoopbackDestination: true,
		},
	}

	go func() {
		conn, err := downstreamListener.Accept()
		if err != nil {
			t.Errorf("downstream Accept() failed: %v", err)
			return
		}
		defer conn.Close()

		// Handle socks5 authentication, no authentication required.
		authReq := make([]byte, 3)
		if _, err := io.ReadFull(conn, authReq); err != nil {
			t.Errorf("downstream io.ReadFull() auth failed: %v", err)
			return
		}
		if authReq[0] != constant.Socks5Version || authReq[1] != 1 || authReq[2] != constant.Socks5NoAuth {
			t.Errorf("downstream got unexpected auth request: %v", authReq)
			return
		}
		if _, err := conn.Write([]byte{constant.Socks5Version, constant.Socks5NoAuth}); err != nil {
			t.Errorf("downstream write auth response failed: %v", err)
			return
		}

		// Read and handle the socks5 request.
		req, err := downstreamServer.readRequest(conn)
		if err != nil {
			t.Errorf("downstream readRequest() failed: %v", err)
			return
		}
		if err := downstreamServer.handleRequest(context.Background(), req, conn); err != nil && !stderror.IsEOF(err) && !stderror.IsClosed(err) {
			t.Errorf("downstream handleRequest() failed: %v", err)
		}
	}()

	// Create the front socks5 server with forwarding configuration.
	proxyName := "test-proxy"
	proxyAction := appctlpb.EgressAction_PROXY
	frontServer := &Server{
		config: &Config{
			AllowLoopbackDestination: true,
			Egress: &appctlpb.Egress{
				Proxies: []*appctlpb.EgressProxy{
					{
						Name: &proxyName,
						Host: func() *string { s := "127.0.0.1"; return &s }(),
						Port: func() *int32 { p := int32(downstreamAddr.Port); return &p }(),
					},
				},
				Rules: []*appctlpb.EgressRule{
					{
						IpRanges:   []string{"*"},
						Action:     &proxyAction,
						ProxyNames: []string{proxyName},
					},
				},
			},
		},
	}

	// Create the connect request.
	clientConn, serverConn := testtool.BufPipe()
	defer serverConn.Close()
	defer clientConn.Close()

	clientConn.Write([]byte{constant.Socks5Version, constant.Socks5ConnectCmd, 0, constant.Socks5IPv4Address, 127, 0, 0, 1})
	port := []byte{0, 0}
	binary.BigEndian.PutUint16(port, uint16(dstAddr.Port))
	clientConn.Write(port)
	clientConn.Write([]byte("ping"))

	// Front socks5 server handles the request and forwards to downstream.
	go func() {
		req, err := frontServer.readRequest(serverConn)
		if err != nil {
			t.Errorf("readRequest() failed: %v", err)
			return
		}

		// Find the action, should be PROXY.
		egressInput := egress.Input{
			Protocol: appctlpb.ProxyProtocol_SOCKS5_PROXY_PROTOCOL,
			Data:     req.Raw,
		}
		action := frontServer.FindAction(context.Background(), egressInput)
		if action.Action != appctlpb.EgressAction_PROXY {
			t.Errorf("expected PROXY action, got %v", action.Action)
			return
		}
		if err := frontServer.handleForwarding(req, serverConn, action.Proxy); err != nil && !stderror.IsEOF(err) && !stderror.IsClosed(err) {
			t.Errorf("handleForwarding() failed: %v", err)
		}
	}()
	time.Sleep(100 * time.Millisecond)

	// Verify response from socks server.
	out := make([]byte, 14)
	if _, err := io.ReadFull(clientConn, out); err != nil {
		t.Fatalf("io.ReadFull() failed: %v", err)
	}
	want := []byte{
		constant.Socks5Version, constant.Socks5ReplySuccess, 0, constant.Socks5IPv4Address,
		127, 0, 0, 1,
		0, 0,
		'p', 'o', 'n', 'g',
	}

	// Ignore the port number before comparing the result.
	out[8] = 0
	out[9] = 0

	if !bytes.Equal(out, want) {
		t.Fatalf("got %v, want %v", out, want)
	}
}

func TestHandleForwardingUDP(t *testing.T) {
	// Create a local UDP listener as the destination target.
	dstAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.ResolveUDPAddr() failed: %v", err)
	}
	dst, err := net.ListenUDP("udp", dstAddr)
	if err != nil {
		t.Fatalf("net.ListenUDP() failed: %v", err)
	}
	defer dst.Close()
	dstAddr, err = net.ResolveUDPAddr("udp", dst.LocalAddr().String())
	if err != nil {
		t.Fatalf("net.ResolveUDPAddr() failed: %v", err)
	}
	dstPort := dstAddr.Port

	// The UDP server reads a single UDP packet "ping" and responds with "pong".
	go func() {
		buf := make([]byte, 4)
		_, addr, err := dst.ReadFrom(buf)
		if err != nil {
			t.Errorf("ReadFrom() failed: %v", err)
			return
		}

		want := []byte("ping")
		if !bytes.Equal(buf, want) {
			t.Errorf("got %v, want %v", buf, want)
			return
		}
		if _, err := dst.WriteTo([]byte("pong"), addr); err != nil {
			t.Errorf("WriteTo() failed: %v", err)
		}
	}()

	// Create a downstream socks5 server that handles UDP associate.
	downstreamListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	defer downstreamListener.Close()
	downstreamAddr := downstreamListener.Addr().(*net.TCPAddr)

	go func() {
		conn, err := downstreamListener.Accept()
		if err != nil {
			t.Errorf("downstream Accept() failed: %v", err)
			return
		}
		defer conn.Close()

		// Handle socks5 authentication, no authentication required.
		authReq := make([]byte, 3)
		if _, err := io.ReadFull(conn, authReq); err != nil {
			t.Errorf("downstream io.ReadFull() auth failed: %v", err)
			return
		}
		if authReq[0] != constant.Socks5Version || authReq[1] != 1 || authReq[2] != constant.Socks5NoAuth {
			t.Errorf("downstream got unexpected auth request: %v", authReq)
			return
		}
		if _, err := conn.Write([]byte{constant.Socks5Version, constant.Socks5NoAuth}); err != nil {
			t.Errorf("downstream write auth response failed: %v", err)
			return
		}

		// Read socks5 request header.
		reqHeader := make([]byte, 10)
		if _, err := io.ReadFull(conn, reqHeader); err != nil {
			t.Errorf("downstream io.ReadFull() request failed: %v", err)
			return
		}
		if reqHeader[1] != constant.Socks5UDPAssociateCmd {
			t.Errorf("expected UDP associate command, got %v", reqHeader[1])
			return
		}

		// Create a UDP listener for the socks5 UDP relay.
		downstreamUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
		if err != nil {
			t.Errorf("downstream net.ListenUDP() failed: %v", err)
			return
		}
		defer downstreamUDP.Close()
		downstreamUDPPort := downstreamUDP.LocalAddr().(*net.UDPAddr).Port

		// Send success response with our UDP listener's address.
		resp := []byte{
			constant.Socks5Version, constant.Socks5ReplySuccess, 0, constant.Socks5IPv4Address,
			127, 0, 0, 1,
			byte(downstreamUDPPort >> 8), byte(downstreamUDPPort),
		}
		if _, err := conn.Write(resp); err != nil {
			t.Errorf("downstream write response failed: %v", err)
			return
		}

		// Receive packet from socks5 proxy client.
		buf := make([]byte, 1024)
		n, clientAddr, err := downstreamUDP.ReadFromUDP(buf)
		if err != nil {
			t.Errorf("downstream ReadFromUDP() failed: %v", err)
			return
		}

		// Parse socks5 UDP header.
		if n < 10 {
			t.Errorf("downstream UDP packet too short: %d", n)
			return
		}
		dstPort := int(buf[8])<<8 | int(buf[9])
		data := buf[10:n]

		// Send data to destination.
		dstAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: dstPort}
		if _, err := downstreamUDP.WriteToUDP(data, dstAddr); err != nil {
			t.Errorf("downstream WriteToUDP() to destination failed: %v", err)
			return
		}

		// Receive response from destination.
		n, _, err = downstreamUDP.ReadFromUDP(buf[10:])
		if err != nil {
			t.Errorf("downstream ReadFromUDP() from destination failed: %v", err)
			return
		}

		// Build response with socks5 UDP header.
		respUDP := []byte{0, 0, 0, constant.Socks5IPv4Address, 127, 0, 0, 1, byte(dstPort >> 8), byte(dstPort)}
		respUDP = append(respUDP, buf[10:10+n]...)
		if _, err := downstreamUDP.WriteToUDP(respUDP, clientAddr); err != nil {
			t.Errorf("downstream WriteToUDP() to client failed: %v", err)
			return
		}

		// Keep TCP connection alive until it's closed.
		io.ReadAll(conn)
	}()

	// Create the front socks5 server with forwarding configuration.
	proxyName := "test-proxy"
	proxyAction := appctlpb.EgressAction_PROXY
	frontServer := &Server{
		config: &Config{
			AllowLoopbackDestination: true,
			Egress: &appctlpb.Egress{
				Proxies: []*appctlpb.EgressProxy{
					{
						Name: &proxyName,
						Host: func() *string { s := "127.0.0.1"; return &s }(),
						Port: func() *int32 { p := int32(downstreamAddr.Port); return &p }(),
					},
				},
				Rules: []*appctlpb.EgressRule{
					{
						IpRanges:   []string{"*"},
						Action:     &proxyAction,
						ProxyNames: []string{proxyName},
					},
				},
			},
		},
	}

	// Create the UDP associate request.
	clientConn, serverConn := testtool.BufPipe()
	defer serverConn.Close()
	defer clientConn.Close()

	clientConn.Write([]byte{
		constant.Socks5Version, constant.Socks5UDPAssociateCmd, 0, constant.Socks5IPv4Address,
		127, 0, 0, 1,
		byte(dstPort >> 8), byte(dstPort),
	})

	// Front socks5 server handles the request and forwards to downstream.
	go func() {
		req, err := frontServer.readRequest(serverConn)
		if err != nil {
			t.Errorf("readRequest() failed: %v", err)
			return
		}

		egressInput := egress.Input{
			Protocol: appctlpb.ProxyProtocol_SOCKS5_PROXY_PROTOCOL,
			Data:     req.Raw,
		}
		action := frontServer.FindAction(context.Background(), egressInput)
		if action.Action != appctlpb.EgressAction_PROXY {
			t.Errorf("expected PROXY action, got %v", action.Action)
			return
		}
		if err := frontServer.handleForwarding(req, serverConn, action.Proxy); err != nil && !stderror.IsEOF(err) && !stderror.IsClosed(err) {
			t.Errorf("handleForwarding() failed: %v", err)
		}
	}()
	time.Sleep(100 * time.Millisecond)

	// Verify response from socks server.
	out := make([]byte, 10)
	if _, err := io.ReadFull(clientConn, out); err != nil {
		t.Fatalf("io.ReadFull() failed: %v", err)
	}
	want := []byte{
		constant.Socks5Version, constant.Socks5ReplySuccess, 0, constant.Socks5IPv4Address,
		127, 0, 0, 1,
		0, 0,
	}

	// Verify the port is non-zero.
	port := binary.BigEndian.Uint16(out[8:10])
	if port == 0 {
		t.Errorf("expected non-zero port, got 0")
	}

	// Ignore the port number before comparing the result.
	out[8] = 0
	out[9] = 0
	if !bytes.Equal(out, want) {
		t.Fatalf("got %v, want %v", out, want)
	}

	// Send UDP packet "ping".
	wrappedConn := apicommon.NewPacketOverStreamTunnel(clientConn)
	udpReq := bytes.NewBuffer(nil)
	udpReq.Write([]byte{0, 0, 0, constant.Socks5IPv4Address, 127, 0, 0, 1})
	udpReq.WriteByte(byte(dstPort >> 8))
	udpReq.WriteByte(byte(dstPort))
	udpReq.Write([]byte("ping"))
	if _, err := wrappedConn.Write(udpReq.Bytes()); err != nil {
		t.Fatalf("Write() failed: %v", err)
	}

	// Verify UDP server responds with "pong".
	wantUDP := append([]byte{0, 0, 0, constant.Socks5IPv4Address, 127, 0, 0, 1, byte(dstPort >> 8), byte(dstPort)}, []byte("pong")...)
	outUDP := make([]byte, len(wantUDP))
	if _, err := io.ReadFull(wrappedConn, outUDP); err != nil {
		t.Fatalf("io.ReadFull() failed: %v", err)
	}
	if !bytes.Equal(outUDP, wantUDP) {
		t.Fatalf("got %v, want %v", outUDP, wantUDP)
	}
}
