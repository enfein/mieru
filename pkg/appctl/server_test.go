// Copyright (C) 2021  mieru authors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package appctl

import (
	"context"
	"os"
	"strings"
	"testing"

	pb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/common"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestApply2ServerConfig(t *testing.T) {

	beforeServerTest(t)

	// Apply config1, and then apply config2.
	configFile1 := "testdata/server_apply_config_1.json"
	if err := ApplyJSONServerConfig(configFile1); err != nil {
		t.Errorf("ApplyJSONServerConfig() failed: %v", err)
	}
	configFile2 := "testdata/server_apply_config_2.json"
	if err := ApplyJSONServerConfig(configFile2); err != nil {
		t.Errorf("ApplyJSONServerConfig() failed: %v", err)
	}
	merged, err := LoadServerConfig()
	if err != nil {
		t.Errorf("LoadServerConfig() failed: %v", err)
	}

	// Apply only config2. The server config should be the same.
	if err := deleteServerConfigFile(); err != nil {
		t.Fatalf("failed to delete server config file")
	}
	if err := StoreServerConfig(&pb.ServerConfig{}); err != nil {
		t.Fatalf("failed to create empty server config file")
	}
	if err := ApplyJSONServerConfig(configFile2); err != nil {
		t.Errorf("ApplyJSONServerConfig() failed: %v", err)
	}
	want, err := LoadServerConfig()
	if err != nil {
		t.Errorf("LoadServerConfig() failed: %v", err)
	}
	if !proto.Equal(merged, want) {
		mergedJSON, _ := common.MarshalJSON(merged)
		wantJSON, _ := common.MarshalJSON(want)
		t.Errorf("server config doesn't equal:\ngot = %v\nwant = %v", string(mergedJSON), string(wantJSON))
	}

	afterServerTest(t)
}

func TestServerApplyReject(t *testing.T) {
	validConfig := func() *pb.ServerConfig {
		return &pb.ServerConfig{
			PortBindings: []*pb.PortBinding{
				{
					Port:     proto.Int32(10001),
					Protocol: pb.TransportProtocol_TCP.Enum(),
				},
			},
			Users: []*pb.User{
				{
					Name:     proto.String("hello"),
					Password: proto.String("world"),
				},
			},
		}
	}

	cases := []struct {
		name          string
		config        *pb.ServerConfig
		wantErrString string
	}{
		{
			name: "invalid_metrics_logging_interval",
			config: func() *pb.ServerConfig {
				c := validConfig()
				c.AdvancedSettings = &pb.ServerAdvancedSettings{
					MetricsLoggingInterval: proto.String("1"),
				}
				return c
			}(),
			wantErrString: `metrics logging interval "1" is invalid`,
		},
		{
			name: "invalid_port_range_1",
			config: func() *pb.ServerConfig {
				c := validConfig()
				c.PortBindings[0].Port = nil
				c.PortBindings[0].PortRange = proto.String("1-2-3")
				return c
			}(),
			wantErrString: "unable to parse port range",
		},
		{
			name: "invalid_port_range_2",
			config: func() *pb.ServerConfig {
				c := validConfig()
				c.PortBindings[0].Port = nil
				c.PortBindings[0].PortRange = proto.String("2-1")
				return c
			}(),
			wantErrString: "begin of port range 2 is bigger than end of port range 1",
		},
		{
			name: "invalid_port_range_3",
			config: func() *pb.ServerConfig {
				c := validConfig()
				c.PortBindings[0].Port = nil
				c.PortBindings[0].PortRange = proto.String("0-1")
				return c
			}(),
			wantErrString: "port number 0 is invalid",
		},
		{
			name: "invalid_quota_days",
			config: func() *pb.ServerConfig {
				c := validConfig()
				c.Users[0].Quotas = []*pb.Quota{
					{Days: proto.Int32(0), Megabytes: proto.Int32(1)},
				}
				return c
			}(),
			wantErrString: "quota: number of days 0 is invalid",
		},
		{
			name: "invalid_quota_megabytes",
			config: func() *pb.ServerConfig {
				c := validConfig()
				c.Users[0].Quotas = []*pb.Quota{
					{Days: proto.Int32(1), Megabytes: proto.Int32(0)},
				}
				return c
			}(),
			wantErrString: "quota: traffic volume in megabyte 0 is invalid",
		},
		{
			name: "metrics_logging_interval_too_small",
			config: func() *pb.ServerConfig {
				c := validConfig()
				c.AdvancedSettings = &pb.ServerAdvancedSettings{MetricsLoggingInterval: proto.String("1ms")}
				return c
			}(),
			wantErrString: "is less than 1 second",
		},
		{
			name: "mtu_too_big",
			config: func() *pb.ServerConfig {
				c := validConfig()
				c.Mtu = proto.Int32(9000)
				return c
			}(),
			wantErrString: "MTU value 9000 is out of range",
		},
		{
			name: "mtu_too_small",
			config: func() *pb.ServerConfig {
				c := validConfig()
				c.Mtu = proto.Int32(100)
				return c
			}(),
			wantErrString: "MTU value 100 is out of range",
		},
		{
			name: "no_password",
			config: func() *pb.ServerConfig {
				c := validConfig()
				c.Users[0].Password = nil
				return c
			}(),
			wantErrString: "user password is not set",
		},
		{
			name: "no_port_bindings",
			config: func() *pb.ServerConfig {
				c := validConfig()
				c.PortBindings = nil
				return c
			}(),
			wantErrString: "server port binding is not set",
		},
		{
			name: "no_port",
			config: func() *pb.ServerConfig {
				c := validConfig()
				c.PortBindings[0].Port = nil
				return c
			}(),
			wantErrString: "unable to parse port range",
		},
		{
			name: "no_protocol",
			config: func() *pb.ServerConfig {
				c := validConfig()
				c.PortBindings[0].Protocol = nil
				return c
			}(),
			wantErrString: "protocol is not set",
		},
		{
			name: "no_user_name",
			config: func() *pb.ServerConfig {
				c := validConfig()
				c.Users[0].Name = nil
				return c
			}(),
			wantErrString: "user name is not set",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			beforeServerTest(t)
			err := ValidateFullServerConfig(c.config)
			if err == nil {
				t.Fatalf("want error in ValidateFullServerConfig(%q), got no error", c.name)
			}
			if !strings.Contains(err.Error(), c.wantErrString) {
				t.Errorf("in ValidateFullServerConfig(%q), want error string %q, got %q", c.name, c.wantErrString, err.Error())
			}
			afterServerTest(t)
		})
	}
}

func TestServerDeleteUser(t *testing.T) {

	beforeServerTest(t)

	configFile := "testdata/server_apply_config_2.json"
	if err := ApplyJSONServerConfig(configFile); err != nil {
		t.Fatalf("ApplyJSONServerConfig() failed: %v", err)
	}

	names := []string{"user2", "user3", "user4"}
	if err := DeleteServerUsers(names); err != nil {
		t.Errorf("DeleteUsers() failed: %v", err)
	}
	config, err := LoadServerConfig()
	if err != nil {
		t.Fatalf("LoadServerConfig() failed: %v", err)
	}
	if len(config.GetUsers()) != 1 {
		t.Errorf("want 1 user, got %d user(s)", len(config.GetUsers()))
	}
	if config.GetUsers()[0].GetName() != "user1" {
		t.Errorf("want user name %q, got %q", "user1", config.GetUsers()[0].GetName())
	}

	afterServerTest(t)
}

func TestServerHashUserPassword(t *testing.T) {

	beforeServerTest(t)

	configFile := "testdata/server_apply_config_1.json"
	if err := ApplyJSONServerConfig(configFile); err != nil {
		t.Errorf("ApplyJSONServerConfig() failed: %v", err)
	}
	config, err := LoadServerConfig()
	if err != nil {
		t.Errorf("LoadServerConfig() failed: %v", err)
	}
	users := config.GetUsers()
	if len(users) == 0 {
		t.Errorf("no user found in server config")
	}
	for _, user := range users {
		if user.GetPassword() != "" {
			t.Errorf("user %q has plaintext password", user.GetName())
		}
		if user.GetHashedPassword() == "" {
			t.Errorf("user %q has no hashed password", user.GetName())
		}
	}

	afterServerTest(t)
}

func TestServerGetVersion(t *testing.T) {
	rpcServer := NewServerManagementService()
	_, err := rpcServer.GetVersion(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetVersion() failed: %v", err)
	}
}

func beforeServerTest(t *testing.T) {
	dir := os.TempDir()
	if dir == "" {
		t.Fatalf("failed to get system temporary directory for the test")
	}
	cachedServerConfigDir = dir
	cachedServerConfigFilePath = dir + string(os.PathSeparator) + "server.conf.pb"
	if err := deleteServerConfigFile(); err != nil {
		t.Fatalf("failed to clean server config file before the test")
	}
	if err := StoreServerConfig(&pb.ServerConfig{}); err != nil {
		t.Fatalf("failed to create empty server config file before the test")
	}
}

func afterServerTest(t *testing.T) {
	if err := deleteServerConfigFile(); err != nil {
		t.Fatalf("failed to clean server config file after the test")
	}
}
