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

// For each incoming UDP packet, if the data satisfy [A-Za-z]+, a rot13
// of the input is send back. Otherwise, there is no response.
package main

import (
	"flag"
	"net"
	"os"
	"strconv"

	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/testtool"
)

var port = flag.Int("port", 0, "UDP server listening port.")

func main() {
	log.SetFormatter(&log.DaemonFormatter{})
	flag.Parse()
	if *port <= 0 || *port >= 65536 {
		log.Fatalf("Invalid UDP listening port %d", *port)
	}
	addr, err := net.ResolveUDPAddr("udp", ":"+strconv.Itoa(*port))
	if err != nil {
		log.Fatalf("net.ResolveUDPAddr() failed: %v", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatalf("net.ListenUDP() failed: %v", err)
	}
	log.Infof("UDP server is initialized, listening to %s", addr.String())
	defer conn.Close()
	buf := make([]byte, 1500)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Errorf("Read() failed: %v", err)
			os.Exit(1)
		}
		if n == 0 {
			continue
		}
		out, err := testtool.TestHelperRot13(buf[:n])
		if err != nil {
			log.Errorf("rot13() failed: %v", err)
			continue
		}
		if _, err = conn.WriteToUDP(out, addr); err != nil {
			log.Errorf("Write() failed: %v", err)
			os.Exit(1)
		}
	}
}
