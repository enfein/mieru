// Copyright (C) 2022  mieru authors
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

// socksudpclient is a client that connects to a server via a socks UDP association.
package main

import (
	"bytes"
	"flag"
	mrand "math/rand"
	"net"
	"strconv"
	"time"

	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/socks5client"
	"github.com/enfein/mieru/pkg/testtool"
	"github.com/enfein/mieru/pkg/util"
)

var (
	dstHost        = flag.String("dst_host", "", "The host IP that the server is running.")
	dstPort        = flag.Int("dst_port", 0, "The UDP port that the server is listening.")
	localProxyHost = flag.String("local_proxy_host", "", "The host IP that local socks proxy is running.")
	localProxyPort = flag.Int("local_proxy_port", 0, "The TCP port that local socks proxy is listening.")
	intervalMs     = flag.Int("interval_ms", 0, "Sleep in milliseconds between two requests.")
	numRequest     = flag.Int("num_request", 1, "Number of requests send to server in each connection.")
	numConn        = flag.Int("num_conn", 1, "Number of connections")
	maxPayload     = flag.Int("max_payload", 1400, "Maxinum number of bytes in a UDP packet.")
)

func init() {
	log.SetFormatter(&log.DaemonFormatter{})
	mrand.Seed(time.Now().UnixNano())
}

func main() {
	flag.Parse()
	if *dstHost == "" || *dstPort == 0 {
		log.Fatalf("Server host or port is not set")
	}
	if *localProxyHost == "" || *localProxyPort == 0 {
		log.Fatalf("Local socks proxy host or port is not set")
	}
	if *intervalMs < 0 {
		log.Fatalf("Interval can't be a negative number")
	}
	if *numRequest <= 0 {
		log.Fatalf("Number of request must be bigger than 0")
	}
	if *numConn <= 0 {
		log.Fatalf("Number of connections must be bigger than 0")
	}
	if *maxPayload <= 0 {
		log.Fatalf("Max UDP payload size must be bigger than 0")
	}

	dstAddr, err := net.ResolveUDPAddr("udp", *dstHost+":"+strconv.Itoa(*dstPort))
	if err != nil {
		log.Fatalf("Resolve destination UDP address failed: %v", err)
	}
	totalRequests := 0
	for i := 0; i < *numConn; i++ {
		CreateNewConnAndDoRequest(*numRequest, dstAddr)
		totalRequests += *numRequest
		log.Infof("Sent %d UDP requests to proxy address %s", totalRequests, dstAddr.String())
	}
}

func CreateNewConnAndDoRequest(nRequest int, dstAddr *net.UDPAddr) {
	socksDialer := socks5client.DialSocks5Proxy(&socks5client.Config{
		Host:    *localProxyHost + ":" + strconv.Itoa(*localProxyPort),
		CmdType: socks5client.UDPAssociateCmd,
	})
	ctrlConn, udpConn, proxyAddr, err := socksDialer("tcp", *dstHost+":"+strconv.Itoa(*dstPort))
	if err != nil {
		log.Fatalf("dial to socks: %v", err)
	}
	defer ctrlConn.Close()
	defer udpConn.Close()

	for i := 0; i < nRequest; i++ {
		DoRequestWithExistingConn(udpConn, proxyAddr, dstAddr)
		time.Sleep(time.Millisecond * time.Duration(*intervalMs))
	}
}

func DoRequestWithExistingConn(conn *net.UDPConn, proxyAddr, dstAddr *net.UDPAddr) {
	payloadSize := mrand.Intn(*maxPayload) + 1
	payload := testtool.TestHelperGenRot13Input(payloadSize)

	conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	defer conn.SetReadDeadline(util.ZeroTime())
	resp, err := socks5client.SendUDP(conn, proxyAddr, dstAddr, payload)
	if err != nil {
		log.Fatalf("socks5client.SendUDP() failed: %v", err)
	}

	rot13, err := testtool.TestHelperRot13(resp)
	if err != nil {
		log.Fatalf("UDP client TestHelperRot13() failed: %v", err)
	}
	if !bytes.Equal(payload, rot13) {
		log.Fatalf("UDP client received unexpected response %d %d %d", payloadSize, len(resp), len(rot13))
	}
}
