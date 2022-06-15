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
	"os"
	"testing"

	pb "github.com/enfein/mieru/pkg/appctl/appctlpb"
	"google.golang.org/protobuf/proto"
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
		mergedJSON, _ := jsonMarshalOption.Marshal(merged)
		wantJSON, _ := jsonMarshalOption.Marshal(want)
		t.Errorf("server config doesn't equal:\ngot = %v\nwant = %v", string(mergedJSON), string(wantJSON))
	}

	afterServerTest(t)
}

func TestServerApplyReject(t *testing.T) {
	cases := []string{
		"testdata/server_reject_mtu_too_big.json",
		"testdata/server_reject_mtu_too_small.json",
		"testdata/server_reject_no_password.json",
		"testdata/server_reject_no_port.json",
		"testdata/server_reject_no_protocol.json",
		"testdata/server_reject_no_user_name.json",
	}

	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			beforeServerTest(t)

			if err := ApplyJSONServerConfig(c); err == nil {
				t.Errorf("want error in ApplyJSONServerConfig(%q), got no error", c)
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
