package socks5

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/testtool"
)

func newTestSocksServer(port int) {
	conf := &Config{
		AllowLocalDestination: true,
	}
	srv, err := New(conf)
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
	runtime.Gosched()
	testtool.WaitForTCPReady(port, 5*time.Second)
}

func TestSocks5Anonymous(t *testing.T) {
	httpTestServer := testtool.NewTestHTTPServer([]byte("hello"))

	port, err := common.UnusedTCPPort()
	if err != nil {
		t.Fatalf("common.UnusedTCPPort() failed: %v", err)
	}
	newTestSocksServer(port)

	dialSocksProxy := Dial(fmt.Sprintf("socks5://127.0.0.1:%d?timeout=5s", port), constant.Socks5ConnectCmd)
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
