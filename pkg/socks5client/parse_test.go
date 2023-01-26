package socks5client

import (
	"reflect"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	t.Parallel()
	testcases := []struct {
		name string
		uri  string
		cfg  Config
	}{
		{
			name: "full config",
			uri:  "socks5://u1:p1@127.0.0.1:8080?timeout=2s",
			cfg: Config{
				Auth: &Auth{
					Username: "u1",
					Password: "p1",
				},
				Host:    "127.0.0.1:8080",
				Timeout: 2 * time.Second,
			},
		},
		{
			name: "simple socks5",
			uri:  "socks5://127.0.0.1:8080",
			cfg: Config{
				Host: "127.0.0.1:8080",
			},
		},
	}
	for _, tc := range testcases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := parse(tc.uri)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(cfg, &tc.cfg) {
				t.Fatalf("expect %v got %v", tc.cfg, cfg)
			}
		})
	}
}
