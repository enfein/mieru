// Copyright 2012, Hailiang Wang. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socks5

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/enfein/mieru/v3/apis/constant"
)

// Client contains socks5 client configuration.
type Client struct {
	CmdType    byte
	Host       string
	Credential *Credential

	// Timeout limits proxy TCP connection establishment and the SOCKS5 handshake.
	Timeout time.Duration
}

// Dial returns the dial function to be used in http.Transport object.
// Argument proxyURI should be in the format: "socks5://user:password@127.0.0.1:1080?timeout=5s".
// Only socks5 protocol is supported.
//
// Deprecated: Use ClientDialer.DialContext with http.Transport.DialContext.
func Dial(proxyURI string, cmdType byte) func(string, string) (net.Conn, error) {
	if cmdType != constant.Socks5ConnectCmd {
		return dialError(fmt.Errorf("command type %d is not supported by Dial", cmdType))
	}
	dialer, err := NewClientDialerFromURI(proxyURI, false)
	if err != nil {
		return dialError(err)
	}
	return func(network, targetAddr string) (net.Conn, error) {
		return dialer.DialContext(context.Background(), network, targetAddr)
	}
}

// DialSocks5Proxy returns two connections that can be used to send TCP and UDP traffic.
//
// Deprecated: Use ClientDialer.DialContext or ClientDialer.ListenPacket.
func DialSocks5Proxy(c *Client) func(string, string) (net.Conn, *net.UDPConn, *net.UDPAddr, error) {
	if c == nil {
		return dialErrorLong(fmt.Errorf("socks5 client configuration is nil"))
	}
	if c.Host == "" {
		return dialErrorLong(fmt.Errorf("socks5 client configuration has no proxy host"))
	}
	if c.CmdType != constant.Socks5ConnectCmd && c.CmdType != constant.Socks5UDPAssociateCmd {
		return dialErrorLong(fmt.Errorf("command type %d is not supported by DialSocks5Proxy", c.CmdType))
	}
	dialer := NewClientDialer(c.Host, c.Credential, c.CmdType == constant.Socks5UDPAssociateCmd)
	dialer.Timeout = c.Timeout
	return func(network, targetAddr string) (net.Conn, *net.UDPConn, *net.UDPAddr, error) {
		return dialer.dial(context.Background(), c.CmdType, network, "", targetAddr)
	}
}

// TransceiveUDPPacket sends a single UDP associate message and returns the response.
func TransceiveUDPPacket(conn *net.UDPConn, proxyAddr, dstAddr *net.UDPAddr, payload []byte) ([]byte, error) {
	header := []byte{0, 0, 0}
	if dstAddr.IP.To4() != nil {
		header = append(header, constant.Socks5IPv4Address)
		header = append(header, dstAddr.IP.To4()...)
		header = append(header, byte(dstAddr.Port>>8))
		header = append(header, byte(dstAddr.Port))
	} else {
		header = append(header, constant.Socks5IPv6Address)
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
	if n < 4 {
		return nil, fmt.Errorf("UDP associate response is too short")
	}
	switch buf[3] {
	case constant.Socks5IPv4Address:
		if n <= 10 {
			return nil, fmt.Errorf("UDP associate response is too short for IPv4 address")
		}
		// Header length is 10 bytes.
		return buf[10:n], nil
	case constant.Socks5IPv6Address:
		if n <= 22 {
			return nil, fmt.Errorf("UDP associate response is too short for IPv6 address")
		}
		// Header length is 22 bytes.
		return buf[22:n], nil
	case constant.Socks5FQDNAddress:
		if n < 5 {
			return nil, fmt.Errorf("UDP associate response is too short for FQDN address")
		}
		domainLen := int(buf[4])
		headerLen := 7 + domainLen
		if n <= headerLen {
			return nil, fmt.Errorf("UDP associate response is too short for FQDN address")
		}
		return buf[headerLen:n], nil
	default:
		return nil, fmt.Errorf("UDP associate unsupported address type: %d", buf[3])
	}
}

// parseProxyURI resolves a socks5 URI and creates a proxy client.
func parseProxyURI(proxyURI string) (*Client, error) {
	uri, err := url.Parse(proxyURI)
	if err != nil {
		return nil, err
	}

	c := &Client{}
	if uri.Scheme != "socks5" {
		return nil, fmt.Errorf("unsupported protocol %s", uri.Scheme)
	}
	c.Host = uri.Host
	user := uri.User.Username()
	password, _ := uri.User.Password()
	if user != "" || password != "" {
		if user == "" || password == "" || len(user) > 255 || len(password) > 255 {
			return nil, fmt.Errorf("invalid user name or password")
		}
		c.Credential = &Credential{
			User:     user,
			Password: password,
		}
	}
	query := uri.Query()
	timeout := query.Get("timeout")
	if timeout != "" {
		var err error
		c.Timeout, err = time.ParseDuration(timeout)
		if err != nil {
			return nil, err
		}
	}
	return c, nil
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
