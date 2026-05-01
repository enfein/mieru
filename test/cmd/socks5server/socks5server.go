// Copyright (C) 2026  mieru authors
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

// socks5server is a plain SOCKS5 server used by integration tests.
package main

import (
	"flag"
	"net"
	"strconv"

	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/socks5"
)

var (
	host          = flag.String("host", "127.0.0.1", "The host IP or domain name for the SOCKS5 server to listen on.")
	port          = flag.Int("port", 1080, "The TCP port for the SOCKS5 server to listen on.")
	allowLoopback = flag.Bool("allow_loopback", true, "Allow proxying to loopback destinations.")
)

func init() {
	log.SetFormatter(&log.DaemonFormatter{})
	log.SetLevel("INFO")
}

func main() {
	flag.Parse()
	if *port <= 0 || *port >= 65536 {
		log.Fatalf("Invalid SOCKS5 listening port %d", *port)
	}

	server, err := socks5.New(&socks5.Config{
		AllowLoopbackDestination: *allowLoopback,
		Resolver:                 &net.Resolver{},
		UDPAssociateMode:         socks5.UDPAssociateModeDatagram,
	})
	if err != nil {
		log.Fatalf("socks5.New() failed: %v", err)
	}

	addr := net.JoinHostPort(*host, strconv.Itoa(*port))
	log.Infof("SOCKS5 server is listening on %s", addr)
	if err := server.ListenAndServe("tcp", addr); err != nil {
		log.Fatalf("SOCKS5 server failed: %v", err)
	}
}
