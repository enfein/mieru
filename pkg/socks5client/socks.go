// Copyright 2012, Hailiang Wang. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package socks5client implements a Socks5 proxy client.
package socks5client

import (
	"fmt"
	"net"
	"time"
)

// Socks5 command types.
const (
	ConnectCmd      byte = 1
	BindCmd         byte = 2
	UDPAssociateCmd byte = 3
)

// Socks5 Address types.
const (
	IPv4   byte = 1
	Domain byte = 3
	IPv6   byte = 4
)

type (
	// Config contains socks5 client configuration.
	Config struct {
		Host    string
		Auth    *Auth
		Timeout time.Duration
		CmdType byte
	}

	// Auth contains socks5 client username and password.
	Auth struct {
		Username string
		Password string
	}
)

// Dial returns the dial function to be used in http.Transport object.
// Argument proxyURI should be in the format: "socks5://user:password@127.0.0.1:1080?timeout=5s".
// Only socks5 protocol is supported.
func Dial(proxyURI string, cmdType byte) func(string, string) (net.Conn, error) {
	if cmdType != ConnectCmd && cmdType != BindCmd && cmdType != UDPAssociateCmd {
		return dialError(fmt.Errorf("command type %d is invalid", cmdType))
	}
	cfg, err := parse(proxyURI)
	if err != nil {
		return dialError(err)
	}
	cfg.CmdType = cmdType
	return func(_, targetAddr string) (conn net.Conn, err error) {
		return cfg.dialSocks5(targetAddr)
	}
}

// DialSocks5Proxy returns two connections that can be used to send TCP and UDP traffic.
func DialSocks5Proxy(cfg *Config) func(string, string) (net.Conn, *net.UDPConn, *net.UDPAddr, error) {
	if cfg == nil {
		return dialErrorLong(fmt.Errorf("socks5 client configuration is nil"))
	}
	if cfg.Host == "" {
		return dialErrorLong(fmt.Errorf("socks5 client configuration has no proxy host"))
	}
	if cfg.CmdType != ConnectCmd && cfg.CmdType != BindCmd && cfg.CmdType != UDPAssociateCmd {
		return dialErrorLong(fmt.Errorf("socks5 client configuration command type %v is invalid", cfg.CmdType))
	}
	return func(_, targetAddr string) (net.Conn, *net.UDPConn, *net.UDPAddr, error) {
		return cfg.dialSocks5Long(targetAddr)
	}
}

// SendUDP sends a UDP associate message and returns the response.
func SendUDP(conn *net.UDPConn, proxyAddr, dstAddr *net.UDPAddr, payload []byte) ([]byte, error) {
	header := []byte{0, 0, 0}
	if dstAddr.IP.To4() != nil {
		header = append(header, 1)
		header = append(header, dstAddr.IP.To4()...)
		header = append(header, byte(dstAddr.Port>>8))
		header = append(header, byte(dstAddr.Port))
	} else {
		header = append(header, 4)
		header = append(header, dstAddr.IP.To16()...)
		header = append(header, byte(dstAddr.Port>>8))
		header = append(header, byte(dstAddr.Port))
	}
	if _, err := conn.WriteToUDP(append(header, payload...), proxyAddr); err != nil {
		return nil, fmt.Errorf("WriteToUDP() failed: %v", err)
	}
	buf := make([]byte, 65536)
	n, readAddr, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, fmt.Errorf("ReadFromUDP() failed: %v", err)
	}
	if readAddr.Port != proxyAddr.Port {
		// We don't compare the IP address because a wildcard address like 0.0.0.0 can be used.
		return nil, fmt.Errorf("unexpected read from a different address")
	}
	if n <= 10 {
		return nil, fmt.Errorf("UDP associate response is too short")
	}
	if buf[3] == IPv4 {
		// Header length is 10 bytes.
		return buf[10:n], nil
	} else if buf[3] == IPv6 {
		// Header length is 22 bytes.
		return buf[22:n], nil
	} else {
		return nil, fmt.Errorf("UDP assciate unsupport address type")
	}
}

func dialError(err error) func(string, string) (net.Conn, error) {
	return func(_, _ string) (net.Conn, error) {
		return nil, err
	}
}

func dialErrorLong(err error) func(string, string) (net.Conn, *net.UDPConn, *net.UDPAddr, error) {
	return func(_, _ string) (net.Conn, *net.UDPConn, *net.UDPAddr, error) {
		return nil, nil, nil, err
	}
}
