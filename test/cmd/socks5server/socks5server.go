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

// socks5server is a plain socks5 server used by integration tests.
package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"

	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/socks5"
)

var (
	host              = flag.String("host", "127.0.0.1", "The host IP or domain name for the socks5 server to listen on.")
	port              = flag.Int("port", 1080, "The TCP port for the socks5 server to listen on.")
	allowLoopback     = flag.Bool("allow_loopback", true, "Allow proxying to loopback destinations.")
	acceptedCountFile = flag.String("accepted_count_file", "", "Optional file path to write the total number of accepted network connections.")
)

func init() {
	log.SetFormatter(&log.DaemonFormatter{})
	log.SetLevel("INFO")
}

func main() {
	flag.Parse()
	if *port <= 0 || *port >= 65536 {
		log.Fatalf("Invalid socks5 listening port %d", *port)
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
	log.Infof("socks5 server is listening on %s", addr)
	if *acceptedCountFile == "" {
		if err := server.ListenAndServe("tcp", addr); err != nil {
			log.Fatalf("socks5 server failed: %v", err)
		}
		return
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("socks5 server failed to listen on %s: %v", addr, err)
	}
	listener = newAcceptedCountListener(listener, *acceptedCountFile)
	if err := server.Serve(listener); err != nil {
		log.Fatalf("socks5 server failed: %v", err)
	}
}

type acceptedCountListener struct {
	net.Listener
	count atomic.Int64
	file  string
}

func newAcceptedCountListener(listener net.Listener, file string) net.Listener {
	l := &acceptedCountListener{
		Listener: listener,
		file:     file,
	}
	if err := writeAcceptedCount(file, 0); err != nil {
		log.Fatalf("failed to initialize accepted count file %q: %v", file, err)
	}
	return l
}

func (l *acceptedCountListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	count := l.count.Add(1)
	if err := writeAcceptedCount(l.file, count); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// writeAcceptedCount writes the total number of accepted network connections.
func writeAcceptedCount(filePath string, count int64) error {
	dir := filepath.Dir(filePath)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp := filepath.Join(dir, fmt.Sprintf(".%s.tmp.%d", filepath.Base(filePath), os.Getpid()))
	data := []byte(strconv.FormatInt(count, 10) + "\n")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filePath)
}
