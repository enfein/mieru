package socks5

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/enfein/mieru/pkg/metrics"
)

type MockConn struct {
	buf bytes.Buffer
}

func (m *MockConn) Write(b []byte) (int, error) {
	return m.buf.Write(b)
}

func (m *MockConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: []byte{127, 0, 0, 1}, Port: 65432}
}

func TestRequestConnect(t *testing.T) {
	// Create a local listener as the destination target.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	go func() {
		conn, err := l.Accept()
		if err != nil {
			t.Errorf("Accept() failed: %v", err)
			return
		}
		defer conn.Close()

		buf := make([]byte, 4)
		if _, err := io.ReadAtLeast(conn, buf, 4); err != nil {
			t.Errorf("io.ReadAtLeast() failed: %v", err)
			return
		}

		want := []byte("ping")
		if !bytes.Equal(buf, want) {
			t.Errorf("got %v, want %v", buf, want)
			return
		}
		conn.Write([]byte("pong"))
	}()
	lAddr := l.Addr().(*net.TCPAddr)

	// Create a socks server.
	s := &Server{
		config: &Config{
			Rules:                 PermitAll(),
			AllowLocalDestination: true,
		},
	}

	// Create the connect request.
	buf := bytes.NewBuffer(nil)
	buf.Write([]byte{5, 1, 0, 1, 127, 0, 0, 1})

	port := []byte{0, 0}
	binary.BigEndian.PutUint16(port, uint16(lAddr.Port))
	buf.Write(port)

	buf.Write([]byte("ping"))

	// Socks server handles the request.
	resp := &MockConn{}
	req, err := NewRequest(buf)
	if err != nil {
		t.Fatalf("NewRequest() failed: %v", err)
	}

	if err := s.handleRequest(req, resp); err != nil {
		t.Fatalf("handleRequest() failed: %v", err)
	}

	// Verify response from socks server.
	out := resp.buf.Bytes()
	want := []byte{
		5,
		0,
		0,
		1,
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

func TestRequestConnectRuleFail(t *testing.T) {
	// Create a local listener as the destination target.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	go func() {
		conn, err := l.Accept()
		if err != nil {
			t.Errorf("Accept() failed: %v", err)
			return
		}
		defer conn.Close()

		buf := make([]byte, 4)
		if _, err := io.ReadAtLeast(conn, buf, 4); err != nil {
			t.Errorf("io.ReadAtLeast() failed: %v", err)
			return
		}

		want := []byte("ping")
		if !bytes.Equal(buf, want) {
			t.Errorf("got %v, want %v", buf, want)
			return
		}
		conn.Write([]byte("pong"))
	}()
	lAddr := l.Addr().(*net.TCPAddr)

	// Create a socks server.
	s := &Server{
		config: &Config{
			Rules:                 PermitNone(),
			AllowLocalDestination: true,
		},
	}

	// Create the connect request.
	buf := bytes.NewBuffer(nil)
	buf.Write([]byte{5, 1, 0, 1, 127, 0, 0, 1})

	port := []byte{0, 0}
	binary.BigEndian.PutUint16(port, uint16(lAddr.Port))
	buf.Write(port)

	buf.Write([]byte("ping"))

	// Socks server handles the request.
	resp := &MockConn{}
	req, err := NewRequest(buf)
	if err != nil {
		t.Fatalf("NewRequest() failed: %v", err)
	}

	if err := s.handleRequest(req, resp); !strings.Contains(err.Error(), "blocked by rules") {
		t.Fatalf("handleRequest() failed: %v", err)
	}

	// Verify response from socks server.
	out := resp.buf.Bytes()
	want := []byte{
		5,
		2,
		0,
		1,
		0, 0, 0, 0,
		0, 0,
	}

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
			[]byte{5, BindCommand, 0, 1, 127, 0, 0, 1, 0, 1},
			[]byte{5, commandNotSupported, 0, 1, 0, 0, 0, 0, 0, 0},
		},
		{
			[]byte{5, AssociateCommand, 0, 1, 127, 0, 0, 1, 0, 1},
			[]byte{5, commandNotSupported, 0, 1, 0, 0, 0, 0, 0, 0},
		},
	}

	// Create a socks server.
	s := &Server{
		config: &Config{
			Rules:                 PermitAll(),
			AllowLocalDestination: true,
		},
	}

	for _, tc := range testcases {
		errCnt := metrics.Socks5UnsupportedCommandErrors

		// Create the connect request.
		buf := bytes.NewBuffer(nil)
		buf.Write(tc.req)

		// Socks server handles the request.
		resp := &MockConn{}
		req, err := NewRequest(buf)
		if err != nil {
			t.Fatalf("NewRequest() failed: %v", err)
		}

		if err := s.handleRequest(req, resp); err != nil {
			t.Fatalf("handleRequest() failed: %v", err)
		}

		// Verify response from socks server.
		out := resp.buf.Bytes()
		if !bytes.Equal(out, tc.resp) {
			t.Errorf("got %v, want %v", out, tc.resp)
		}
		if metrics.Socks5UnsupportedCommandErrors <= errCnt {
			t.Errorf("metrics.Socks5UnsupportedCommandErrors value is not changed")
		}
	}
}
