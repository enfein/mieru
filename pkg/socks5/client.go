// Copyright 2012, Hailiang Wang. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socks5

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/pkg/util"
)

// Client contains socks5 client configuration.
type Client struct {
	Host       string
	Credential *Credential
	Timeout    time.Duration
	CmdType    byte
}

// Dial returns the dial function to be used in http.Transport object.
// Argument proxyURI should be in the format: "socks5://user:password@127.0.0.1:1080?timeout=5s".
// Only socks5 protocol is supported.
func Dial(proxyURI string, cmdType byte) func(string, string) (net.Conn, error) {
	if cmdType != constant.Socks5ConnectCmd && cmdType != constant.Socks5BindCmd && cmdType != constant.Socks5UDPAssociateCmd {
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
func DialSocks5Proxy(c *Client) func(string, string) (net.Conn, *net.UDPConn, *net.UDPAddr, error) {
	if c == nil {
		return dialErrorLong(fmt.Errorf("socks5 client configuration is nil"))
	}
	if c.Host == "" {
		return dialErrorLong(fmt.Errorf("socks5 client configuration has no proxy host"))
	}
	if c.CmdType != constant.Socks5ConnectCmd && c.CmdType != constant.Socks5BindCmd && c.CmdType != constant.Socks5UDPAssociateCmd {
		return dialErrorLong(fmt.Errorf("socks5 client configuration command type %v is invalid", c.CmdType))
	}
	return func(_, targetAddr string) (net.Conn, *net.UDPConn, *net.UDPAddr, error) {
		return c.dialSocks5Long(targetAddr)
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
	if n <= 10 {
		return nil, fmt.Errorf("UDP associate response is too short")
	}
	if buf[3] == constant.Socks5IPv4Address {
		// Header length is 10 bytes.
		return buf[10:n], nil
	} else if buf[3] == constant.Socks5IPv6Address {
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

func (c *Client) dialSocks5(targetAddr string) (conn net.Conn, err error) {
	conn, _, _, err = c.dialSocks5Long(targetAddr)
	return
}

func (c *Client) dialSocks5Long(targetAddr string) (conn net.Conn, udpConn *net.UDPConn, proxyUDPAddr *net.UDPAddr, err error) {
	ctx := context.Background()
	var cancelFunc context.CancelFunc
	if c.Timeout != 0 {
		ctx, cancelFunc = context.WithTimeout(ctx, c.Timeout)
		defer cancelFunc()
	}
	dialer := &net.Dialer{}
	conn, err = dialer.DialContext(ctx, "tcp", c.Host)
	if err != nil {
		return nil, nil, nil, err
	}
	defer func() {
		if err != nil && conn != nil {
			conn.Close()
		}
	}()

	// Prepare the first request.
	var req bytes.Buffer
	version := byte(constant.Socks5Version)
	method := byte(constant.Socks5NoAuth)
	if c.Credential != nil {
		method = constant.Socks5UserPassAuth
	}
	req.Write([]byte{
		version,
		1, // number of methods
		method,
	})

	// Process the first response.
	var resp []byte
	resp, err = util.SendReceive(ctx, conn, req.Bytes())
	if err != nil {
		return nil, nil, nil, err
	} else if len(resp) != 2 {
		return nil, nil, nil, fmt.Errorf("server does not respond properly")
	} else if resp[0] != version {
		return nil, nil, nil, fmt.Errorf("server does not support socks5")
	} else if resp[1] != method {
		return nil, nil, nil, fmt.Errorf("socks method negotiation failed")
	}
	if c.Credential != nil {
		version := byte(constant.Socks5UserPassAuthVersion)
		req.Reset()
		req.Write([]byte{version})
		req.Write([]byte{byte(len(c.Credential.User))})
		req.Write([]byte(c.Credential.User))
		req.Write([]byte{byte(len(c.Credential.Password))})
		req.Write([]byte(c.Credential.Password))

		resp, err = util.SendReceive(ctx, conn, req.Bytes())
		if err != nil {
			return nil, nil, nil, err
		} else if len(resp) != 2 {
			return nil, nil, nil, fmt.Errorf("server does not respond properly")
		} else if resp[0] != version {
			return nil, nil, nil, fmt.Errorf("server does not support user/password version 1")
		} else if resp[1] != constant.Socks5AuthSuccess {
			return nil, nil, nil, fmt.Errorf("user/password login failed")
		}
	}

	// Prepare the second request.
	host, portStr, err := net.SplitHostPort(targetAddr)
	if err != nil {
		return nil, nil, nil, err
	}
	portInt, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return nil, nil, nil, err
	}
	port := uint16(portInt)

	req.Reset()
	req.Write([]byte{
		constant.Socks5Version,
		c.CmdType,
		0, // reserved, must be zero
	})

	hostIP := net.ParseIP(host)
	if hostIP == nil {
		// Domain name.
		req.Write([]byte{constant.Socks5FQDNAddress, byte(len(host))})
		req.Write([]byte(host))
	} else {
		hostIPv4 := hostIP.To4()
		if hostIPv4 != nil {
			// IPv4
			req.Write([]byte{constant.Socks5IPv4Address})
			req.Write(hostIPv4)
		} else {
			// IPv6
			req.Write([]byte{constant.Socks5IPv6Address})
			req.Write(hostIP)
		}
	}
	req.Write([]byte{
		byte(port >> 8), // higher byte of destination port
		byte(port),      // lower byte of destination port (big endian)
	})

	// Process the second response.
	resp, err = util.SendReceive(ctx, conn, req.Bytes())
	if err != nil {
		return
	} else if len(resp) < 10 {
		return nil, nil, nil, fmt.Errorf("server response is too short")
	} else if resp[1] != 0 {
		return nil, nil, nil, fmt.Errorf("socks5 connection is not successful")
	}

	if c.CmdType == constant.Socks5UDPAssociateCmd {
		// Get the endpoint to relay UDP packets.
		var ip net.IP
		switch resp[3] {
		case constant.Socks5IPv4Address:
			ip = net.IP(resp[4:8])
			port = uint16(resp[8])<<8 + uint16(resp[9])
		case constant.Socks5IPv6Address:
			if len(resp) < 22 {
				return nil, nil, nil, fmt.Errorf("server response is too short")
			}
			ip = net.IP(resp[4:20])
			port = uint16(resp[20])<<8 + uint16(resp[21])
		default:
			return nil, nil, nil, fmt.Errorf("unsupported bind address")
		}
		proxyUDPAddr = &net.UDPAddr{IP: ip, Port: int(port)}

		// Listen to a new UDP endpoint.
		udpAddr := &net.UDPAddr{IP: net.IP{0, 0, 0, 0}, Port: 0}
		udpConn, err = net.ListenUDP("udp4", udpAddr)
		if err != nil {
			return nil, nil, nil, err
		}
		return conn, udpConn, proxyUDPAddr, nil
	}

	return conn, nil, nil, nil
}
