package socks5

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	apicommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/pkg/stderror"
	"github.com/enfein/mieru/v3/pkg/testtool"
)

func TestNewRejectsUnsupportedUDPAssociateMode(t *testing.T) {
	_, err := New(&Config{UDPAssociateMode: UDPAssociateMode(99)})
	if err == nil {
		t.Fatal("New() error = nil, want error")
	}
}

func TestSocks5Connect(t *testing.T) {
	// Create a local listener as the destination target.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	defer l.Close()
	go func() {
		conn, err := l.Accept()
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
		if _, err := conn.Write([]byte("pong")); err != nil {
			t.Errorf("Write() failed: %v", err)
		}
	}()
	lAddr := l.Addr().(*net.TCPAddr)

	// Create a socks server.
	conf := &Config{
		AllowLoopbackDestination: true,
	}
	serv, err := New(conf)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	conn := servePipe(t, serv)

	req := bytes.NewBuffer(nil)
	req.Write([]byte{constant.Socks5Version, 1, constant.Socks5NoAuth})
	req.Write([]byte{constant.Socks5Version, constant.Socks5ConnectCmd, 0, constant.Socks5IPv4Address, 127, 0, 0, 1})
	port := []byte{0, 0}
	binary.BigEndian.PutUint16(port, uint16(lAddr.Port))
	req.Write(port)
	req.Write([]byte("ping"))

	// Send all the bytes.
	if _, err := conn.Write(req.Bytes()); err != nil {
		t.Fatalf("Write() failed: %v", err)
	}

	// Verify response from socks server.
	want := []byte{
		constant.Socks5Version, constant.Socks5NoAuth,
		constant.Socks5Version, 0, 0, constant.Socks5IPv4Address,
		127, 0, 0, 1,
		0, 0,
		'p', 'o', 'n', 'g',
	}
	out := make([]byte, len(want))
	conn.SetDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(conn, out); err != nil {
		t.Fatalf("io.ReadFull() failed: %v", err)
	}

	// Ignore the port number before compare the result.
	out[10] = 0
	out[11] = 0

	if !bytes.Equal(out, want) {
		t.Fatalf("got %v, want %v", out, want)
	}
}

func TestSocks5UDPAssociation(t *testing.T) {
	t.Skipf("Skip due to high failure rate in CI")
	udpUploadPktsCnt := UDPAssociateUploadPackets.Load()
	udpDownloadPktsCnt := UDPAssociateDownloadPackets.Load()

	// Create a local listener as the destination target.
	udpListenerAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.ResolveUDPAddr() failed: %v", err)
	}
	l, err := net.ListenUDP("udp", udpListenerAddr)
	if err != nil {
		t.Fatalf("net.ListenUDP() failed: %v", err)
	}
	udpListenerAddr, err = net.ResolveUDPAddr("udp", l.LocalAddr().String())
	if err != nil {
		t.Fatalf("net.ResolveUDPAddr() failed: %v", err)
	}
	_, udpListenPortStr, err := net.SplitHostPort(udpListenerAddr.String())
	if err != nil {
		t.Fatalf("net.SplitHostPort() failed: %v", err)
	}
	udpListenPort, err := strconv.Atoi(udpListenPortStr)
	if err != nil {
		t.Fatalf("strconv.Atoi() failed: %v", err)
	}
	go func() {
		defer l.Close()
		buf := make([]byte, 4)
		_, addr, err := l.ReadFrom(buf)
		if err != nil {
			t.Errorf("ReadFrom() failed: %v", err)
			return
		}

		want := []byte("ping")
		if !bytes.Equal(buf, want) {
			t.Errorf("got %v, want %v", buf, want)
			return
		}
		if _, err := l.WriteTo([]byte("pong"), addr); err != nil {
			t.Errorf("WriteTo() failed: %v", err)
		}
	}()

	// Create a socks server.
	conf := &Config{
		AllowLoopbackDestination: true,
	}
	serv, err := New(conf)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	conn := servePipe(t, serv)

	req := bytes.NewBuffer(nil)
	req.Write([]byte{constant.Socks5Version, 1, constant.Socks5NoAuth})
	req.Write([]byte{constant.Socks5Version, constant.Socks5UDPAssociateCmd, 0, constant.Socks5IPv4Address, 127, 0, 0, 1, 0, 0})

	// Send initial UDP association request.
	if _, err := conn.Write(req.Bytes()); err != nil {
		t.Fatalf("Write() failed: %v", err)
	}

	// Verify response from socks server.
	want := []byte{
		constant.Socks5Version, constant.Socks5NoAuth,
		constant.Socks5Version, 0, 0, constant.Socks5IPv4Address,
		0, 0, 0, 0,
		0, 0,
	}
	out := make([]byte, len(want))
	conn.SetDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(conn, out); err != nil {
		t.Fatalf("io.ReadFull() failed: %v", err)
	}

	// Ignore the port number before compare the result.
	t.Logf("socks5 server created UDP listener on port %d", int(out[10])<<8+int(out[11]))
	out[10] = 0
	out[11] = 0

	if !bytes.Equal(out, want) {
		t.Fatalf("got %v, want %v", out, want)
	}

	// Send subsequent UDP association request.
	wrappedConn := apicommon.NewPacketOverStreamTunnel(conn)
	req.Reset()
	req.Write([]byte{0, 0, 0, 1, 127, 0, 0, 1})
	req.WriteByte(byte(udpListenPort >> 8))
	req.WriteByte(byte(udpListenPort))
	req.Write([]byte("ping"))

	if _, err := wrappedConn.Write(req.Bytes()); err != nil {
		t.Fatalf("Write() failed: %v", err)
	}

	// Verify UDP response.
	want = append([]byte{0, 0, 0, 1, 127, 0, 0, 1, byte(udpListenPort >> 8), byte(udpListenPort)}, []byte("pong")...)
	out = make([]byte, len(want))
	if _, err := io.ReadFull(wrappedConn, out); err != nil {
		t.Fatalf("io.ReadFull() failed: %v", err)
	}
	if !bytes.Equal(out, want) {
		t.Fatalf("got %v, want %v", out, want)
	}

	// Verify metrics are updated.
	if UDPAssociateUploadPackets.Load() <= udpUploadPktsCnt {
		t.Errorf("UDPAssociateUploadPackets value %d is not increased", UDPAssociateUploadPackets.Load())
	}
	if UDPAssociateDownloadPackets.Load() <= udpDownloadPktsCnt {
		t.Errorf("UDPAssociateDownloadPackets value %d is not increased", UDPAssociateDownloadPackets.Load())
	}
}

// servePipe buffers pipelined authentication, requests, and payloads while the
// server writes its replies before reading the next part of the request.
func servePipe(t *testing.T, serv *Server) net.Conn {
	t.Helper()
	clientConn, serverConn := testtool.BufPipe()
	done := make(chan error, 1)
	go func() {
		done <- serv.ServeConn(serverConn)
	}()
	t.Cleanup(func() {
		clientConn.Close()
		serverConn.Close()
		select {
		case err := <-done:
			if err != nil && !stderror.IsEOF(err) && !stderror.IsClosed(err) {
				t.Errorf("ServeConn() failed: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("socks5 server did not stop")
		}
	})
	if err := clientConn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("SetDeadline() failed: %v", err)
	}
	return clientConn
}
