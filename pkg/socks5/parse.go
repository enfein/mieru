package socks5

import (
	"fmt"
	"net/url"
	"time"
)

func parse(proxyURI string) (*Client, error) {
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
