package socks5client

import (
	"fmt"
	"net"

	"github.com/enfein/mieru/pkg/log"
)

func (cfg *config) dialSocks5(targetAddr string) (_ net.Conn, err error) {
	proxy := cfg.Host

	if log.IsLevelEnabled(log.DebugLevel) {
		log.Debugf("socks5 client dial to %s %s", "tcp", proxy)
	}
	conn, err := net.DialTimeout("tcp", proxy, cfg.Timeout)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
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

	resp, err := cfg.sendReceive(conn, req.Bytes())
	if err != nil {
		return nil, err
	} else if len(resp) != 2 {
		return nil, fmt.Errorf("server does not respond properly")
	} else if resp[0] != 5 {
		return nil, fmt.Errorf("server does not support Socks 5")
	} else if resp[1] != method {
		return nil, fmt.Errorf("socks method negotiation failed")
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
		resp, err := cfg.sendReceive(conn, req.Bytes())
		if err != nil {
			return nil, err
		} else if len(resp) != 2 {
			return nil, fmt.Errorf("server does not respond properly")
		} else if resp[0] != version {
			return nil, fmt.Errorf("server does not support user/password version 1")
		} else if resp[1] != 0 { // not success
			return nil, fmt.Errorf("user/password login failed")
		}
	}

	// detail request
	host, port, err := splitHostPort(targetAddr)
	if err != nil {
		return nil, err
	}
	req.Reset()
	req.add(
		5,               // version number
		1,               // connect command
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
	} else if len(resp) != 10 {
		return nil, fmt.Errorf("server does not respond properly")
	} else if resp[1] != 0 {
		return nil, fmt.Errorf("can't complete SOCKS5 connection")
	}

	return conn, nil
}
