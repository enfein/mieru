package socks5client

import (
	"fmt"
	"net/url"
	"time"
)

type (
	config struct {
		Proto   int
		Host    string
		Auth    *auth
		Timeout time.Duration
		CmdType byte
	}
	auth struct {
		Username string
		Password string
	}
)

func parse(proxyURI string) (*config, error) {
	uri, err := url.Parse(proxyURI)
	if err != nil {
		return nil, err
	}
	cfg := &config{}
	switch uri.Scheme {
	case "socks4":
		cfg.Proto = SOCKS4
	case "socks4a":
		cfg.Proto = SOCKS4A
	case "socks5":
		cfg.Proto = SOCKS5
	default:
		return nil, fmt.Errorf("unknown SOCKS protocol %s", uri.Scheme)
	}
	cfg.Host = uri.Host
	user := uri.User.Username()
	password, _ := uri.User.Password()
	if user != "" || password != "" {
		if user == "" || password == "" || len(user) > 255 || len(password) > 255 {
			return nil, fmt.Errorf("invalid user name or password")
		}
		cfg.Auth = &auth{
			Username: user,
			Password: password,
		}
	}
	query := uri.Query()
	timeout := query.Get("timeout")
	if timeout != "" {
		var err error
		cfg.Timeout, err = time.ParseDuration(timeout)
		if err != nil {
			return nil, err
		}
	}
	return cfg, nil
}
