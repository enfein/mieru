package socks5client

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/enfein/mieru/pkg/socks5"
	"github.com/enfein/mieru/pkg/util"
)

var httpTestServer = func() *http.Server {
	httpTestPort, err := util.UnusedTCPPort()
	if err != nil {
		panic(err)
	}
	s := &http.Server{
		Addr: ":" + strconv.Itoa(httpTestPort),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Write([]byte("hello"))
		}),
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	started := make(chan struct{})
	go func() {
		l, err := net.Listen("tcp", s.Addr)
		if err != nil {
			panic(err)
		}
		close(started)
		if err = http.Serve(l, s.Handler); err != nil {
			panic(err)
		}
	}()
	runtime.Gosched()
	<-started
	tcpReady(httpTestPort, 2*time.Second)
	return s
}()

func newTestSocksServer(port int) {
	conf := &socks5.Config{
		AllowLocalDestination: true,
	}
	srv, err := socks5.New(conf)
	if err != nil {
		panic(err)
	}
	started := make(chan struct{})
	go func() {
		l, err := net.Listen("tcp", "0.0.0.0:"+strconv.Itoa(port))
		if err != nil {
			panic(err)
		}
		close(started)
		if err := srv.Serve(l); err != nil {
			panic(err)
		}
	}()
	runtime.Gosched()
	<-started
	tcpReady(port, 2*time.Second)
}

func tcpReady(port int, timeout time.Duration) {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), timeout)
	if err != nil {
		panic(err)
	}
	conn.Close()
}

func TestSocks5Anonymous(t *testing.T) {
	port, err := util.UnusedTCPPort()
	if err != nil {
		t.Fatalf("util.UnusedTCPPort() failed: %v", err)
	}
	newTestSocksServer(port)
	dialSocksProxy := Dial(fmt.Sprintf("socks5://127.0.0.1:%d?timeout=5s", port), ConnectCmd)
	tr := &http.Transport{Dial: dialSocksProxy}
	httpClient := &http.Client{Transport: tr}
	resp, err := httpClient.Get(fmt.Sprintf("http://localhost" + httpTestServer.Addr))
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	if string(respBody) != "hello" {
		t.Fatalf("expect response hello but got %s", respBody)
	}
}
