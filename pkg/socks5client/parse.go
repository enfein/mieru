package socks5client

import (
	"fmt"
	"net/url"
	"time"
)

func parse(proxyURI string) (*Config, error) {
	uri, err := url.Parse(proxyURI)
	if err != nil {
		return nil, err
	}
	cfg := &Config{}
	switch uri.Scheme {
	case "socks5":
		// This is the only supported scheme.
	default:
		return nil, fmt.Errorf("unsupported SOCKS protocol %s", uri.Scheme)
	}
	cfg.Host = uri.Host
	user := uri.User.Username()
	password, _ := uri.User.Password()
	if user != "" || password != "" {
		if user == "" || password == "" || len(user) > 255 || len(password) > 255 {
			return nil, fmt.Errorf("invalid user name or password")
		}
		cfg.Auth = &Auth{
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
