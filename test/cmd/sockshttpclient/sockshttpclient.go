// Copyright (C) 2021  mieru authors
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

// sockshttpclient is a HTTP client that connects to HTTP server via a socks proxy.
// It will fetch the data from HTTP server and verifies the SHA-1 checksum is correct.
package main

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/socks5client"
)

const (
	NewConnTest   = "new_conn"
	ReuseConnTest = "reuse_conn"
)

var (
	dstHost        = flag.String("dst_host", "", "The host IP or domain name that HTTP server is running.")
	dstPort        = flag.Int("dst_port", 0, "The TCP port that HTTP server is listening.")
	localProxyHost = flag.String("local_proxy_host", "", "The host IP or domain name that local socks proxy is running.")
	localProxyPort = flag.Int("local_proxy_port", 0, "The TCP port that local socks proxy is listening.")
	noProxy        = flag.Bool("no_proxy", false, "Don't use proxy. Directly connect to the destination.")
	testCase       = flag.String("test_case", "new_conn", fmt.Sprintf("Supported: %q, %q.", NewConnTest, ReuseConnTest))
	intervalMs     = flag.Int("interval_ms", 0, "Sleep in milliseconds between two requests.")
	numRequest     = flag.Int("num_request", 0, "Number of HTTP requests send to server before exit. This option is not compatible with -test_time_sec.")
	printSpeed     = flag.Int("print_speed", 100, "Number of HTTP requests to print the network speed.")
	testTimeSec    = flag.Int("test_time_sec", 0, "Number of seconds to run the test before exit. This option is not compatible with -num_request.")

	totalBytes int64
	startTime  time.Time
)

type countedConn struct {
	net.Conn
}

func (c countedConn) Read(b []byte) (n int, err error) {
	n, err = c.Conn.Read(b)
	atomic.AddInt64(&totalBytes, int64(n))
	return
}

func (c countedConn) Write(b []byte) (n int, err error) {
	n, err = c.Conn.Write(b)
	atomic.AddInt64(&totalBytes, int64(n))
	return
}

func wrapConn(conn net.Conn) net.Conn {
	return countedConn{Conn: conn}
}

func init() {
	log.SetFormatter(&log.DaemonFormatter{})
}

func main() {
	flag.Parse()
	if *dstHost == "" || *dstPort == 0 {
		log.Fatalf("HTTP server host or port is not set")
	}
	if *localProxyHost == "" || *localProxyPort == 0 {
		log.Fatalf("Local socks proxy host or port is not set")
	}
	if *testCase != NewConnTest && *testCase != ReuseConnTest {
		log.Fatalf("Test case %q is unknown", *testCase)
	}
	if *intervalMs < 0 {
		log.Fatalf("Interval can't be a negative number")
	}
	if *numRequest <= 0 && *testTimeSec <= 0 {
		log.Fatalf("Must specify either -num_request or -test_time_sec")
	}
	if *numRequest > 0 && *testTimeSec > 0 {
		log.Fatalf("Can't specify both -num_request and -test_time_sec")
	}

	startTime = time.Now()
	if *testCase == NewConnTest {
		if *numRequest > 0 {
			for i := 1; i <= *numRequest; i++ {
				CreateNewConnAndDoRequest(i, *noProxy)
				if *printSpeed > 0 && i%*printSpeed == 0 {
					printNetworkSpeed(i)
				}
				time.Sleep(time.Millisecond * time.Duration(*intervalMs))
			}
		} else {
			done := make(chan bool)
			go func() {
				time.Sleep(time.Duration(*testTimeSec) * time.Second)
				close(done)
			}()
			i := 1
			for {
				select {
				case <-done:
					return
				default:
					CreateNewConnAndDoRequest(i, *noProxy)
					if *printSpeed > 0 && i%*printSpeed == 0 {
						printNetworkSpeed(i)
					}
					time.Sleep(time.Millisecond * time.Duration(*intervalMs))
					i++
				}
			}
		}
	} else if *testCase == ReuseConnTest {
		var conn net.Conn
		var err error
		for {
			if *noProxy {
				conn, err = net.Dial("tcp", *dstHost+":"+strconv.Itoa(*dstPort))
			} else {
				socksDialer := socks5client.DialSocksProxy(socks5client.SOCKS5, *localProxyHost+":"+strconv.Itoa(*localProxyPort), socks5client.ConnectCmd)
				conn, _, _, err = socksDialer("tcp", *dstHost+":"+strconv.Itoa(*dstPort))
			}
			if err == nil {
				break
			}
			if !errors.Is(err, io.EOF) {
				log.Fatalf("dial failed: %v", err)
			}
		}
		conn = wrapConn(conn)
		if *numRequest > 0 {
			for i := 1; i <= *numRequest; i++ {
				DoRequestWithExistingConn(conn, i)
				if *printSpeed > 0 && i%*printSpeed == 0 {
					printNetworkSpeed(i)
				}
				time.Sleep(time.Millisecond * time.Duration(*intervalMs))
			}
		} else {
			done := make(chan bool)
			go func() {
				time.Sleep(time.Duration(*testTimeSec) * time.Second)
				close(done)
			}()
			i := 1
			for {
				select {
				case <-done:
					return
				default:
					DoRequestWithExistingConn(conn, i)
					if *printSpeed > 0 && i%*printSpeed == 0 {
						printNetworkSpeed(i)
					}
					time.Sleep(time.Millisecond * time.Duration(*intervalMs))
					i++
				}
			}
		}
		conn.Close()
	}
}

func CreateNewConnAndDoRequest(seq int, noProxy bool) {
	var conn net.Conn
	var err error
	for {
		if noProxy {
			conn, err = net.Dial("tcp", *dstHost+":"+strconv.Itoa(*dstPort))
		} else {
			socksDialer := socks5client.DialSocksProxy(socks5client.SOCKS5, *localProxyHost+":"+strconv.Itoa(*localProxyPort), socks5client.ConnectCmd)
			conn, _, _, err = socksDialer("tcp", *dstHost+":"+strconv.Itoa(*dstPort))
		}
		if err == nil {
			break
		}
		if !errors.Is(err, io.EOF) {
			log.Fatalf("dial failed: %v", err)
		}
	}
	defer conn.Close()
	DoRequestWithExistingConn(wrapConn(conn), seq)
}

func DoRequestWithExistingConn(conn net.Conn, seq int) {
	req, err := http.NewRequest(http.MethodGet, "", nil)
	if err != nil {
		log.Fatalf("error create HTTP request: %v", err)
	}
	req.URL.Scheme = "http"
	req.URL.Host = *dstHost + ":" + strconv.Itoa(*dstPort)
	if err := req.Write(conn); err != nil {
		log.Fatalf("failed to write HTTP request: %v", err)
	}

	buf := bufio.NewReader(conn)
	resp, err := http.ReadResponse(buf, req)
	if err != nil {
		log.Fatalf("error connect to server %s:%d %v", *dstHost, *dstPort, err)
	}
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("server responded status %s", resp.Status)
	}

	providedCheckSum := resp.Header.Get("X-SHA1")
	if len(providedCheckSum) == 0 {
		log.Fatalf("server bug: SHA-1 check is not set")
	}

	body, err := io.ReadAll(resp.Body)
	defer resp.Body.Close()
	if err != nil {
		log.Fatalf("failed to read HTTP response: %v", err)
	}
	log.Debugf("Round %d: HTTP client received response body with %d bytes", seq, len(body))

	computedCheckSumArr := sha1.Sum(body)
	computedCheckSum := hex.EncodeToString(computedCheckSumArr[:])
	log.Debugf("HTTP client computed SHA-1 checksum: %s", computedCheckSum)

	if providedCheckSum != computedCheckSum {
		log.Fatalf("SHA-1 checksum not match. Provided by server: %s, computed with response data: %s",
			providedCheckSum, computedCheckSum)
	}
}

func printNetworkSpeed(seq int) {
	sec := int(time.Now().Sub(startTime) / time.Second)
	if sec <= 0 {
		sec = 1
	}
	log.Infof("Round %d: network speed is %d KB/s", seq, totalBytes/int64(1024*sec))
}
