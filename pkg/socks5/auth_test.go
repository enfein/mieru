package socks5

import (
	"bytes"
	"testing"

	"github.com/enfein/mieru/pkg/testtool"
)

func TestNoAuth(t *testing.T) {
	clientConn, serverConn := testtool.BufPipe()
	clientConn.Write([]byte{1, NoAuth})

	s, _ := New(&Config{})
	ctx, err := s.authenticate(serverConn)
	if err != nil {
		t.Fatalf("authenticate() failed: %v", err)
	}
	if ctx.Method != NoAuth {
		t.Fatal("Invalid Context Method")
	}

	out := make([]byte, 2)
	clientConn.Read(out)
	want := []byte{socks5Version, NoAuth}
	if !bytes.Equal(out, want) {
		t.Fatalf("response = %v, want %v", out, want)
	}
}

func TestPasswordAuthValid(t *testing.T) {
	clientConn, serverConn := testtool.BufPipe()
	clientConn.Write([]byte{2, NoAuth, UserPassAuth})
	clientConn.Write([]byte{1, 3, 'f', 'o', 'o', 3, 'b', 'a', 'r'})

	cred := StaticCredentials{
		"foo": "bar",
	}
	authenticator := UserPassAuthenticator{Credentials: cred}
	s, _ := New(&Config{AuthMethods: []Authenticator{authenticator}})
	ctx, err := s.authenticate(serverConn)
	if err != nil {
		t.Fatalf("authenticate() failed: %v", err)
	}
	if ctx.Method != UserPassAuth {
		t.Fatal("Invalid Context Method")
	}
	val, ok := ctx.Payload["Username"]
	if !ok {
		t.Fatal("Missing key Username in auth context's payload")
	}
	if val != "foo" {
		t.Fatal("Invalid Username in auth context's payload")
	}

	out := make([]byte, 4)
	clientConn.Read(out)
	want := []byte{socks5Version, UserPassAuth, 1, authSuccess}
	if !bytes.Equal(out, want) {
		t.Fatalf("response = %v, want %v", out, want)
	}
}

func TestPasswordAuthInvalid(t *testing.T) {
	clientConn, serverConn := testtool.BufPipe()
	clientConn.Write([]byte{2, NoAuth, UserPassAuth})
	clientConn.Write([]byte{1, 3, 'f', 'o', 'o', 3, 'b', 'a', 'z'})

	cred := StaticCredentials{
		"foo": "bar",
	}
	authenticator := UserPassAuthenticator{Credentials: cred}
	s, _ := New(&Config{AuthMethods: []Authenticator{authenticator}})
	ctx, err := s.authenticate(serverConn)
	if err != UserAuthFailed {
		t.Fatalf("authenticate() returns unexpected error: %v", err)
	}
	if ctx != nil {
		t.Fatal("Invalid Context Method")
	}

	out := make([]byte, 4)
	clientConn.Read(out)
	want := []byte{socks5Version, UserPassAuth, 1, authFailure}
	if !bytes.Equal(out, want) {
		t.Fatalf("response = %v, want %v", out, want)
	}
}

func TestNoSupportedAuth(t *testing.T) {
	clientConn, serverConn := testtool.BufPipe()
	clientConn.Write([]byte{1, NoAuth})

	cred := StaticCredentials{
		"foo": "bar",
	}
	authenticator := UserPassAuthenticator{Credentials: cred}
	s, _ := New(&Config{AuthMethods: []Authenticator{authenticator}})
	ctx, err := s.authenticate(serverConn)
	if err != NoSupportedAuth {
		t.Fatalf("authenticate() returns unexpected error: %v", err)
	}
	if ctx != nil {
		t.Fatal("Invalid Context Method")
	}

	out := make([]byte, 2)
	clientConn.Read(out)
	want := []byte{socks5Version, noAcceptable}
	if !bytes.Equal(out, want) {
		t.Fatalf("response = %v, want %v", out, want)
	}
}
