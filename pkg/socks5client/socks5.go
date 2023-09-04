package socks5client

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/enfein/mieru/pkg/util"
)

func (cfg *Config) dialSocks5(targetAddr string) (conn net.Conn, err error) {
	conn, _, _, err = cfg.dialSocks5Long(targetAddr)
	return
}

func (cfg *Config) dialSocks5Long(targetAddr string) (conn net.Conn, udpConn *net.UDPConn, proxyUDPAddr *net.UDPAddr, err error) {
	ctx := context.Background()
	var cancelFunc context.CancelFunc
	if cfg.Timeout != 0 {
		ctx, cancelFunc = context.WithTimeout(ctx, cfg.Timeout)
		defer cancelFunc()
	}
	dialer := &net.Dialer{}
	conn, err = dialer.DialContext(ctx, "tcp", cfg.Host)
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
	version := byte(5) // socks version 5
	method := byte(0)  // method 0: no authentication (only anonymous access supported for now)
	if cfg.Auth != nil {
		method = 2 // method 2: username/password
	}
	req.Write([]byte{
		version, // socks version
		1,       // number of methods
		method,
	})

	// Process the first response.
	var resp []byte
	resp, err = util.SendReceive(ctx, conn, req.Bytes())
	if err != nil {
		return nil, nil, nil, err
	} else if len(resp) != 2 {
		return nil, nil, nil, fmt.Errorf("server does not respond properly")
	} else if resp[0] != 5 {
		return nil, nil, nil, fmt.Errorf("server does not support Socks 5")
	} else if resp[1] != method {
		return nil, nil, nil, fmt.Errorf("socks method negotiation failed")
	}
	if cfg.Auth != nil {
		version := byte(1) // user/password version 1
		req.Reset()
		req.Write([]byte{
			version,                      // user/password version
			byte(len(cfg.Auth.Username)), // length of username
		})
		req.Write([]byte(cfg.Auth.Username))
		req.Write([]byte{byte(len(cfg.Auth.Password))})
		req.Write([]byte(cfg.Auth.Password))

		resp, err = util.SendReceive(ctx, conn, req.Bytes())
		if err != nil {
			return nil, nil, nil, err
		} else if len(resp) != 2 {
			return nil, nil, nil, fmt.Errorf("server does not respond properly")
		} else if resp[0] != version {
			return nil, nil, nil, fmt.Errorf("server does not support user/password version 1")
		} else if resp[1] != 0 { // not success
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
		5,           // version number
		cfg.CmdType, // command
		0,           // reserved, must be zero
	})

	hostIP := net.ParseIP(host)
	if hostIP == nil {
		// Domain name.
		req.Write([]byte{3, byte(len(host))})
		req.Write([]byte(host))
	} else {
		hostIPv4 := hostIP.To4()
		if hostIPv4 != nil {
			// IPv4
			req.Write([]byte{1})
			req.Write(hostIPv4)
		} else {
			// IPv6
			req.Write([]byte{4})
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
		return nil, nil, nil, fmt.Errorf("SOCKS5 connection is not successful")
	}

	if cfg.CmdType == UDPAssociateCmd {
		// Get the endpoint to relay UDP packets.
		var ip net.IP
		switch resp[3] {
		case IPv4:
			ip = net.IP(resp[4:8])
			port = uint16(resp[8])<<8 + uint16(resp[9])
		case IPv6:
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
