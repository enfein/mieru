package socks5

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func TestSocks5Connect(t *testing.T) {
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
	lAddr := l.Addr().(*net.TCPAddr)

	// Create a socks server.
	creds := StaticCredentials{
		"foo": "bar",
	}
	cator := UserPassAuthenticator{Credentials: creds}
	conf := &Config{
		AuthMethods:           []Authenticator{cator},
		AllowLocalDestination: true,
	}
	serv, err := New(conf)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// Socks server start listening.
	go func() {
		if err := serv.ListenAndServe("tcp", "127.0.0.1:12345"); err != nil {
			t.Errorf("ListenAndServe() failed: %v", err)
			return
		}
	}()
	time.Sleep(10 * time.Millisecond)

	// Dial to socks server.
	conn, err := net.Dial("tcp", "127.0.0.1:12345")
	if err != nil {
		t.Fatalf("net.Dial() failed: %v", err)
	}

	req := bytes.NewBuffer(nil)
	req.Write([]byte{5})
	req.Write([]byte{2, NoAuth, UserPassAuth})
	req.Write([]byte{1, 3, 'f', 'o', 'o', 3, 'b', 'a', 'r'})
	req.Write([]byte{5, 1, 0, 1, 127, 0, 0, 1})

	port := []byte{0, 0}
	binary.BigEndian.PutUint16(port, uint16(lAddr.Port))
	req.Write(port)

	req.Write([]byte("ping"))

	// Send all the bytes.
	conn.Write(req.Bytes())

	// Verify response from socks server.
	want := []byte{
		socks5Version, UserPassAuth,
		1, authSuccess,
		5,
		0,
		0,
		1,
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
	out[12] = 0
	out[13] = 0

	if !bytes.Equal(out, want) {
		t.Fatalf("got %v, want %v", out, want)
	}
}

func TestServerGroup(t *testing.T) {
	c := &Config{}
	s1, err := New(c)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	g := NewGroup()
	if err := g.Add("UDP", 12346, s1); err != nil {
		t.Fatalf("Add() failed: %v", err)
	}
	if g.IsEmpty() {
		t.Errorf("IsEmpty() = %v, want %v", true, false)
	}
	if err := g.CloseAndRemoveAll(); err != nil {
		t.Fatalf("CloseAndRemoveAll() failed: %v", err)
	}
	if !g.IsEmpty() {
		t.Errorf("After CloseAndRemoveAll(), IsEmpty() = %v, want %v", false, true)
	}
}
