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

func TestApply2ClientConfig(t *testing.T) {
	beforeClientTest(t)

	// Apply config1, and then apply config2.
	configFile1 := "testdata/client_apply_config_1.json"
	if err := ApplyJSONClientConfig(configFile1); err != nil {
		t.Errorf("ApplyJSONClientConfig(%q) failed: %v", configFile1, err)
	}
	configFile2 := "testdata/client_apply_config_2.json"
	if err := ApplyJSONClientConfig(configFile2); err != nil {
		t.Errorf("ApplyJSONClientConfig(%q) failed: %v", configFile2, err)
	}
	merged, err := LoadClientConfig()
	if err != nil {
		t.Errorf("LoadClientConfig() failed: %v", err)
	}

	// Apply only config2. The client config should be the same.
	if err := deleteClientConfigFile(); err != nil {
		t.Fatalf("failed to delete client config file")
	}
	if err := StoreClientConfig(&pb.ClientConfig{}); err != nil {
		t.Fatalf("failed to create empty client config file")
	}
	if err := ApplyJSONClientConfig(configFile2); err != nil {
		t.Errorf("ApplyJSONClientConfig(%q) failed: %v", configFile2, err)
	}
	want, err := LoadClientConfig()
	if err != nil {
		t.Errorf("LoadClientConfig() failed: %v", err)
	}
	if !proto.Equal(merged, want) {
		mergedJSON, _ := common.MarshalJSON(merged)
		wantJSON, _ := common.MarshalJSON(want)
		t.Errorf("client config doesn't equal:\ngot = %v\nwant = %v", string(mergedJSON), string(wantJSON))
	}

	afterClientTest(t)
}

func TestClientApplyReject(t *testing.T) {
	validConfig := func() *pb.ClientConfig {
		return &pb.ClientConfig{
			ActiveProfile: proto.String("default"),
			Profiles: []*pb.ClientProfile{
				{
					ProfileName: proto.String("default"),
					User: &pb.User{
						Name:     proto.String("hello"),
						Password: proto.String("world"),
					},
					Servers: []*pb.ServerEndpoint{
						{
							IpAddress: proto.String("127.0.0.1"),
							PortBindings: []*pb.PortBinding{
								{
									Port:     proto.Int32(10001),
									Protocol: pb.TransportProtocol_TCP.Enum(),
								},
							},
						},
					},
				},
			},
			Socks5Port: proto.Int32(1080),
		}
	}

	cases := []struct {
		name          string
		config        *pb.ClientConfig
		wantErrString string
	}{
		{
			name: "active_profile_mismatch",
			config: func() *pb.ClientConfig {
				c := validConfig()
				c.ActiveProfile = proto.String("p2")
				return c
			}(),
			wantErrString: "active profile is not found in the profile list",
		},
		{
			name: "invalid_http_port",
			config: func() *pb.ClientConfig {
				c := validConfig()
				c.HttpProxyPort = proto.Int32(70000)
				return c
			}(),
			wantErrString: "HTTP proxy port number 70000 is invalid",
		},
		{
			name: "invalid_rpc_port",
			config: func() *pb.ClientConfig {
				c := validConfig()
				c.RpcPort = proto.Int32(70000)
				return c
			}(),
			wantErrString: "RPC port number 70000 is invalid",
		},
		{
			name: "metrics_logging_interval_too_small",
			config: func() *pb.ClientConfig {
				c := validConfig()
				c.AdvancedSettings = &pb.ClientAdvancedSettings{
					MetricsLoggingInterval: proto.String("0s"),
				}
				return c
			}(),
			wantErrString: "is less than 1 second",
		},
		{
			name: "mtu_too_big",
			config: func() *pb.ClientConfig {
				c := validConfig()
				c.Profiles[0].Mtu = proto.Int32(9000)
				return c
			}(),
			wantErrString: "MTU value 9000 is out of range",
		},
		{
			name: "mtu_too_small",
			config: func() *pb.ClientConfig {
				c := validConfig()
				c.Profiles[0].Mtu = proto.Int32(100)
				return c
			}(),
			wantErrString: "MTU value 100 is out of range",
		},
		{
			name: "no_active_profile",
			config: func() *pb.ClientConfig {
				c := validConfig()
				c.ActiveProfile = nil
				return c
			}(),
			wantErrString: "active profile is not set",
		},
		{
			name: "no_password",
			config: func() *pb.ClientConfig {
				c := validConfig()
				c.Profiles[0].User.Password = nil
				return c
			}(),
			wantErrString: "password is not set",
		},
		{
			name: "no_port_binding",
			config: func() *pb.ClientConfig {
				c := validConfig()
				c.Profiles[0].Servers[0].PortBindings = nil
				return c
			}(),
			wantErrString: "server port binding is not set",
		},
		{
			name: "no_port",
			config: func() *pb.ClientConfig {
				c := validConfig()
				c.Profiles[0].Servers[0].PortBindings[0].Port = nil
				return c
			}(),
			wantErrString: "unable to parse port range",
		},
		{
			name: "no_profile_name",
			config: func() *pb.ClientConfig {
				c := validConfig()
				c.Profiles[0].ProfileName = nil
				return c
			}(),
			wantErrString: "profile name is not set",
		},
		{
			name: "no_profiles",
			config: func() *pb.ClientConfig {
				c := validConfig()
				c.Profiles = nil
				return c
			}(),
			wantErrString: "profiles are not set",
		},
		{
			name: "no_protocol",
			config: func() *pb.ClientConfig {
				c := validConfig()
				c.Profiles[0].Servers[0].PortBindings[0].Protocol = nil
				return c
			}(),
			wantErrString: "protocol is not set",
		},
		{
			name: "no_server_addr",
			config: func() *pb.ClientConfig {
				c := validConfig()
				c.Profiles[0].Servers[0].IpAddress = nil
				return c
			}(),
			wantErrString: "neither server IP address nor domain name is set",
		},
		{
			name: "no_servers",
			config: func() *pb.ClientConfig {
				c := validConfig()
				c.Profiles[0].Servers = nil
				return c
			}(),
			wantErrString: "servers are not set",
		},
		{
			name: "no_user_name",
			config: func() *pb.ClientConfig {
				c := validConfig()
				c.Profiles[0].User.Name = nil
				return c
			}(),
			wantErrString: "user name is not set",
		},
		{
			name: "same_port_http_rpc",
			config: func() *pb.ClientConfig {
				c := validConfig()
				c.HttpProxyPort = proto.Int32(1080)
				c.RpcPort = proto.Int32(1080)
				c.Socks5Port = nil
				return c
			}(),
			wantErrString: "socks5 port number 0 is invalid",
		},
		{
			name: "same_port_http_socks5",
			config: func() *pb.ClientConfig {
				c := validConfig()
				c.HttpProxyPort = proto.Int32(1080)
				return c
			}(),
			wantErrString: "HTTP proxy port number 1080 is the same as socks5 port number",
		},
		{
			name: "same_port_rpc_socks5",
			config: func() *pb.ClientConfig {
				c := validConfig()
				c.RpcPort = proto.Int32(1080)
				return c
			}(),
			wantErrString: "RPC port number 1080 is the same as socks5 port number",
		},
		{
			name: "socks5_auth_no_password",
			config: func() *pb.ClientConfig {
				c := validConfig()
				c.Socks5Authentication = []*pb.Auth{
					{User: proto.String("user")},
				}
				return c
			}(),
			wantErrString: "socks5 authentication password is not set",
		},
		{
			name: "socks5_auth_no_user",
			config: func() *pb.ClientConfig {
				c := validConfig()
				c.Socks5Authentication = []*pb.Auth{
					{Password: proto.String("pass")},
				}
				return c
			}(),
			wantErrString: "socks5 authentication user is not set",
		},
		{
			name: "user_has_quota",
			config: func() *pb.ClientConfig {
				c := validConfig()
				c.Profiles[0].User.Quotas = []*pb.Quota{{Days: proto.Int32(1), Megabytes: proto.Int32(1)}}
				return c
			}(),
			wantErrString: "user quota is not supported by proxy client",
		},
		{
			name: "wrong_ipv4_address",
			config: func() *pb.ClientConfig {
				c := validConfig()
				c.Profiles[0].Servers[0].IpAddress = proto.String("127.0.0.256")
				return c
			}(),
			wantErrString: "failed to parse IP address",
		},
		{
			name: "wrong_ipv6_address",
			config: func() *pb.ClientConfig {
				c := validConfig()
				c.Profiles[0].Servers[0].IpAddress = proto.String("::ffff::1")
				return c
			}(),
			wantErrString: "failed to parse IP address",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			beforeClientTest(t)
			err := applyClientConfig(c.config)
			if err == nil {
				t.Fatalf("want error in applyClientConfig(%q), got no error", c.name)
			}
			if !strings.Contains(err.Error(), c.wantErrString) {
				t.Errorf("in applyClientConfig(%q), want error string %q, got %q", c.name, c.wantErrString, err.Error())
			}
			afterClientTest(t)
		})
	}
}

func TestClientLoadConfigFromJSON(t *testing.T) {
	beforeClientTest(t)

	// Copy the JSON configuration file to test directory.
	b, err := os.ReadFile("testdata/client_apply_config_1.json")
	if err != nil {
		t.Fatalf("os.ReadFile() failed: %v", err)
	}
	configFilePath := cachedClientConfigDir + string(os.PathSeparator) + "client_load.json"
	if err := os.WriteFile(configFilePath, b, 0660); err != nil {
		t.Fatalf("os.WriteFile() failed: %v", err)
	}
	cachedClientConfigFilePath = ""

	if err := os.Setenv("MIERU_CONFIG_JSON_FILE", configFilePath); err != nil {
		t.Fatalf("os.Setenv() failed: %v", err)
	}
	defer os.Unsetenv("MIERU_CONFIG_JSON_FILE")

	// Load JSON configuration file.
	config, err := LoadClientConfig()
	if err != nil {
		t.Fatalf("LoadClientConfig() failed: %v", err)
	}
	if config.GetActiveProfile() != "default" {
		t.Errorf("client config data is unexpected")
	}

	afterClientTest(t)
}

func TestClientStoreConfigToJSON(t *testing.T) {
	beforeClientTest(t)

	configFilePath := cachedClientConfigDir + string(os.PathSeparator) + "client_store.json"
	cachedClientConfigFilePath = ""

	if err := os.Setenv("MIERU_CONFIG_JSON_FILE", configFilePath); err != nil {
		t.Fatalf("os.Setenv() failed: %v", err)
	}
	defer os.Unsetenv("MIERU_CONFIG_JSON_FILE")

	// Store JSON configuration file.
	if err := StoreClientConfig(&pb.ClientConfig{}); err != nil {
		t.Fatalf("StoreClientConfig() failed: %v", err)
	}
	if _, err := os.Stat(configFilePath); err != nil {
		t.Errorf("client config is not found at %q", configFilePath)
	}

	afterClientTest(t)
}

func TestClientDeleteProfile(t *testing.T) {
	beforeClientTest(t)

	configFile := "testdata/client_before_delete_profile.json"
	if err := ApplyJSONClientConfig(configFile); err != nil {
		t.Fatalf("ApplyJSONClientConfig(%q) failed: %v", configFile, err)
	}
	if err := DeleteClientConfigProfile("default"); err != nil {
		t.Errorf("DeleteClientConfigProfile(%q) failed: %v", "default", err)
	}
	if err := DeleteClientConfigProfile("this profile doesn't exist"); err != nil {
		t.Errorf("DeleteClientConfigProfile(%q) failed: %v", "this profile doesn't exist", err)
	}
	got, err := LoadClientConfig()
	if err != nil {
		t.Errorf("LoadClientConfig() failed: %v", err)
	}

	// Compare the result with client_after_delete_profile.json
	wantFile := "testdata/client_after_delete_profile.json"
	if err := deleteClientConfigFile(); err != nil {
		t.Fatalf("failed to delete client config file")
	}
	if err := StoreClientConfig(&pb.ClientConfig{}); err != nil {
		t.Fatalf("failed to create empty client config file")
	}
	if err := ApplyJSONClientConfig(wantFile); err != nil {
		t.Fatalf("ApplyJSONClientConfig(%q) failed: %v", wantFile, err)
	}
	want, err := LoadClientConfig()
	if err != nil {
		t.Errorf("LoadClientConfig() failed: %v", err)
	}
	if !proto.Equal(got, want) {
		gotJSON, _ := common.MarshalJSON(got)
		wantJSON, _ := common.MarshalJSON(want)
		t.Errorf("client config doesn't equal:\ngot = %v\nwant = %v", string(gotJSON), string(wantJSON))
	}

	afterClientTest(t)
}

func TestClientDeleteProfileRejectActiveProfile(t *testing.T) {
	beforeClientTest(t)

	configFile := "testdata/client_before_delete_profile.json"
	if err := ApplyJSONClientConfig(configFile); err != nil {
		t.Fatalf("ApplyJSONClientConfig(%q) failed: %v", configFile, err)
	}
	if err := DeleteClientConfigProfile("new"); err == nil {
		t.Errorf("want error in DeleteClientConfigProfile(%q), got no error", "new")
	}

	afterClientTest(t)
}

func TestClientGetVersion(t *testing.T) {
	rpcServer := NewClientManagementService()
	_, err := rpcServer.GetVersion(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetVersion() failed: %v", err)
	}
}

func beforeClientTest(t *testing.T) {
	dir := os.TempDir()
	if dir == "" {
		t.Fatalf("failed to get system temporary directory for the test")
	}
	cachedClientConfigDir = dir
	if err := deleteClientConfigFile(); err != nil {
		t.Fatalf("failed to clean client config file before the test")
	}
	if err := StoreClientConfig(&pb.ClientConfig{}); err != nil {
		t.Fatalf("failed to create empty client config file before the test")
	}
}

func afterClientTest(t *testing.T) {
	if err := deleteClientConfigFile(); err != nil {
		t.Fatalf("failed to clean client config file after the test")
	}
}
