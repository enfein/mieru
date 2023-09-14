package socks5

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"

	"github.com/enfein/mieru/pkg/testtool"
)

func TestRequestConnect(t *testing.T) {
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

	// Create a socks server.
	s := &Server{
		config: &Config{
			AllowLocalDestination: true,
		},
	}

	// Create the connect request.
	clientConn, serverConn := testtool.BufPipe()
	clientConn.Write([]byte{5, 1, 0, 1, 127, 0, 0, 1})
	port := []byte{0, 0}
	binary.BigEndian.PutUint16(port, uint16(dstAddr.Port))
	clientConn.Write(port)
	clientConn.Write([]byte("ping"))

	// Socks server handles the request.
	req, err := NewRequest(serverConn)
	if err != nil {
		t.Fatalf("NewRequest() failed: %v", err)
	}
	if err := s.handleRequest(context.Background(), req, serverConn); err != nil {
		t.Fatalf("handleRequest() failed: %v", err)
	}

	// Verify response from socks server.
	out := make([]byte, 14)
	clientConn.Read(out)
	want := []byte{
		5, 0, 0, 1,
		127, 0, 0, 1,
		0, 0,
		'p', 'o', 'n', 'g',
	}

	// Ignore the port number before compare the result.
	out[8] = 0
	out[9] = 0

	if !bytes.Equal(out, want) {
		t.Fatalf("got %v, want %v", out, want)
	}
}

func TestRequestUnsupportedCommand(t *testing.T) {
	testcases := []struct {
		req  []byte
		resp []byte
	}{
		{
			[]byte{5, bindCommand, 0, 1, 127, 0, 0, 1, 0, 1},
			[]byte{5, commandNotSupported, 0, 1, 0, 0, 0, 0, 0, 0},
		},
	}

	// Create a socks server.
	s := &Server{
		config: &Config{
			AllowLocalDestination: true,
		},
	}

	for _, tc := range testcases {
		errCnt := UnsupportedCommandErrors.Load()

		// Create the connect request.
		clientConn, serverConn := testtool.BufPipe()
		clientConn.Write(tc.req)

		// Socks server handles the request.
		req, err := NewRequest(serverConn)
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
