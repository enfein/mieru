package socks5client

import (
	"fmt"
	"net"
)

func (cfg *config) dialSocks5(targetAddr string) (conn net.Conn, err error) {
	conn, _, _, err = cfg.dialSocks5Long(targetAddr)
	return
}

func (cfg *config) dialSocks5Long(targetAddr string) (conn net.Conn, udpConn *net.UDPConn, proxyUDPAddr *net.UDPAddr, err error) {
	proxy := cfg.Host
	conn, err = net.DialTimeout("tcp", proxy, cfg.Timeout)
	if err != nil {
		return nil, nil, nil, err
	}
	defer func() {
		if err != nil && conn != nil {
			conn.Close()
		}
	}()

	var req requestBuilder
	version := byte(5) // socks version 5
	method := byte(0)  // method 0: no authentication (only anonymous access supported for now)
	if cfg.Auth != nil {
		method = 2 // method 2: username/password
	}

	// version identifier/method selection request
	req.add(
		version, // socks version
		1,       // number of methods
		method,
	)

	var resp []byte
	resp, err = cfg.sendReceive(conn, req.Bytes())
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
		req.add(
			version,                      // user/password version
			byte(len(cfg.Auth.Username)), // length of username
		)
		req.add([]byte(cfg.Auth.Username)...)
		req.add(byte(len(cfg.Auth.Password)))
		req.add([]byte(cfg.Auth.Password)...)
		resp, err = cfg.sendReceive(conn, req.Bytes())
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

	// detail request
	var host string
	var port uint16
	host, port, err = splitHostPort(targetAddr)
	if err != nil {
		return nil, nil, nil, err
	}
	req.Reset()
	req.add(
		5,               // version number
		cfg.CmdType,     // command
		0,               // reserved, must be zero
		3,               // address type, 3 means domain name
		byte(len(host)), // address length
	)
	req.add([]byte(host)...)
	req.add(
		byte(port>>8), // higher byte of destination port
		byte(port),    // lower byte of destination port (big endian)
	)
	resp, err = cfg.sendReceive(conn, req.Bytes())
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
