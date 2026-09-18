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
	"fmt"
	"net"
	"os"
	"strings"
	"testing"

	pb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/protocol"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestMergeServerConfig(t *testing.T) {
	initial := &pb.ServerConfig{
		PortBindings: []*pb.PortBinding{{Port: proto.Int32(8000), Protocol: pb.TransportProtocol_TCP.Enum()}},
		Users: []*pb.User{{
			Name:           proto.String("user1"),
			Password:       proto.String("old-password"),
			Quotas:         []*pb.Quota{{Days: proto.Int32(7), Megabytes: proto.Int32(1000)}},
			AllowPrivateIP: proto.Bool(true),
		}},
		LoggingLevel:   pb.LoggingLevel_DEBUG.Enum(),
		Mtu:            proto.Int32(1300),
		TrafficPattern: &pb.TrafficPattern{Seed: proto.Int32(1)},
	}
	replacement := &pb.ServerConfig{
		PortBindings: []*pb.PortBinding{
			{Port: proto.Int32(9000), Protocol: pb.TransportProtocol_TCP.Enum()},
			{PortRange: proto.String("10000-11000"), Protocol: pb.TransportProtocol_UDP.Enum()},
			{PortRange: proto.String("12000-13000"), Protocol: pb.TransportProtocol_TCP.Enum()},
		},
		Users: []*pb.User{
			{Name: proto.String("user1"), Password: proto.String("new-password")},
			{
				Name:     proto.String("user2"),
				Password: proto.String("password2"),
				Quotas: []*pb.Quota{
					{Days: proto.Int32(7), Megabytes: proto.Int32(1000)},
					{Days: proto.Int32(30), Megabytes: proto.Int32(2000)},
				},
				AllowLoopbackIP: proto.Bool(true),
			},
		},
		LoggingLevel: pb.LoggingLevel_INFO.Enum(),
		Mtu:          proto.Int32(1400),
		Egress: &pb.Egress{
			Proxies: []*pb.EgressProxy{{
				Name: proto.String("proxy1"), Protocol: pb.ProxyProtocol_SOCKS5_PROXY_PROTOCOL.Enum(),
				Host: proto.String("localhost"), Port: proto.Int32(1081),
			}},
			Rules: []*pb.EgressRule{
				{IpRanges: []string{"8.8.8.8/32"}, Action: pb.EgressAction_REJECT.Enum()},
				{DomainNames: []string{"example.com"}, Action: pb.EgressAction_PROXY.Enum(), ProxyNames: []string{"proxy1"}},
				{IpRanges: []string{"*"}, DomainNames: []string{"*"}, Action: pb.EgressAction_DIRECT.Enum()},
			},
		},
		Dns:            &pb.DNS{DualStack: pb.DualStack_PREFER_IPv4.Enum()},
		TrafficPattern: &pb.TrafficPattern{Seed: proto.Int32(2)},
	}
	cases := []struct {
		name  string
		dst   *pb.ServerConfig
		patch *pb.ServerConfig
		want  *pb.ServerConfig
	}{
		{"initialize", &pb.ServerConfig{}, replacement, replacement},
		{"replace fields and users", initial, replacement, replacement},
		{"preserve omitted fields", replacement, &pb.ServerConfig{}, replacement},
		{"preserve omitted users", replacement, &pb.ServerConfig{Users: replacement.Users[:1]}, replacement},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			config := proto.Clone(c.dst).(*pb.ServerConfig)
			patch := proto.Clone(c.patch).(*pb.ServerConfig)
			if err := ValidateServerConfigPatch(patch); err != nil {
				t.Fatal(err)
			}
			if err := MergeServerConfig(config, patch); err != nil {
				t.Fatal(err)
			}
			if err := ValidateFullServerConfig(config); err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(config, c.want) {
				t.Errorf("merged config = %v, want %v", config, c.want)
			}
		})
	}
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
		{
			name: "invalid_dns_host_ip",
			config: func() *pb.ServerConfig {
				c := validConfig()
				c.Dns = &pb.DNS{
					Hosts: map[string]string{
						"study.ok.com": "bad-ip",
					},
				}
				return c
			}(),
			wantErrString: `domain name "study.ok.com" has invalid IP address "bad-ip"`,
		},
		{
			name: "user_name_too_long",
			config: func() *pb.ServerConfig {
				c := validConfig()
				c.Users[0].Name = proto.String(strings.Repeat("a", 65))
				return c
			}(),
			wantErrString: "user name exceeds 64 bytes",
		},
		{
			name: "user_password_too_long",
			config: func() *pb.ServerConfig {
				c := validConfig()
				c.Users[0].Password = proto.String(strings.Repeat("a", 65))
				return c
			}(),
			wantErrString: "user password exceeds 64 bytes",
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
	defer afterServerTest(t)

	if err := StoreServerConfig(&pb.ServerConfig{Users: []*pb.User{
		{Name: proto.String("user1"), Password: proto.String("password1")},
		{Name: proto.String("user2"), Password: proto.String("password2")},
	}}); err != nil {
		t.Fatalf("StoreServerConfig() failed: %v", err)
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
		t.Fatalf("want 1 user, got %d user(s)", len(config.GetUsers()))
	}
	if config.GetUsers()[0].GetName() != "user1" {
		t.Errorf("want user name %q, got %q", "user1", config.GetUsers()[0].GetName())
	}
}

func TestStoreServerConfigHashesPasswords(t *testing.T) {
	beforeServerTest(t)
	defer afterServerTest(t)

	if err := StoreServerConfig(&pb.ServerConfig{Users: []*pb.User{
		{Name: proto.String("user1"), Password: proto.String("password1")},
	}}); err != nil {
		t.Fatalf("StoreServerConfig() failed: %v", err)
	}
	config, err := LoadServerConfig()
	if err != nil {
		t.Fatalf("LoadServerConfig() failed: %v", err)
	}
	users := config.GetUsers()
	if len(users) != 1 {
		t.Fatalf("want 1 user, got %d user(s)", len(users))
	}
	for _, user := range users {
		if user.GetPassword() != "" {
			t.Errorf("user %q has plaintext password", user.GetName())
		}
		if user.GetHashedPassword() == "" {
			t.Errorf("user %q has no hashed password", user.GetName())
		}
	}
}

func TestServerStopClearsMuxRef(t *testing.T) {
	rpcServer := NewServerManagementService()
	mux := protocol.NewMux(false)
	t.Cleanup(func() {
		SetServerMuxRef(nil)
		if err := mux.Close(); err != nil {
			t.Errorf("mux.Close() failed: %v", err)
		}
	})
	SetServerMuxRef(mux)
	SetSocks5Server(nil)

	if _, err := rpcServer.Stop(context.Background(), &emptypb.Empty{}); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}
	if serverMuxRef.Load() != nil {
		t.Fatal("server mux ref is not cleared after Stop()")
	}
}

func TestServerProxyStartFailure(t *testing.T) {
	beforeServerTest(t)
	defer afterServerTest(t)

	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	defer listener.Close()
	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address type is %T, want *net.TCPAddr", listener.Addr())
	}

	config := &pb.ServerConfig{
		PortBindings: []*pb.PortBinding{
			{
				Port:     proto.Int32(int32(tcpAddr.Port)),
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
	if err := StoreServerConfig(config); err != nil {
		t.Fatalf("StoreServerConfig() failed: %v", err)
	}

	SetAppStatus(pb.AppStatus_IDLE)
	SetServerMuxRef(nil)
	SetSocks5Server(nil)
	t.Cleanup(func() {
		SetServerMuxRef(nil)
		SetSocks5Server(nil)
		SetAppStatus(pb.AppStatus_IDLE)
	})

	rpcServer := NewServerManagementService()
	if _, err := rpcServer.Start(context.Background(), &emptypb.Empty{}); err == nil {
		t.Fatal("Start() succeeded with occupied server port")
	}
	if got := GetAppStatus(); got != pb.AppStatus_STOPPED {
		t.Fatalf("app status = %s, want %s", got, pb.AppStatus_STOPPED)
	}
	if serverMuxRef.Load() != nil {
		t.Fatal("server mux ref is not cleared after proxy start failure")
	}
	if socks5ServerRef.Load() != nil {
		t.Fatal("socks5 server ref is not cleared after proxy start failure")
	}
}

func TestServerGetSessionInfoListRequiresMux(t *testing.T) {
	rpcServer := NewServerManagementService()
	SetServerMuxRef(nil)
	t.Cleanup(func() {
		SetServerMuxRef(nil)
	})

	if _, err := rpcServer.GetSessionInfoList(context.Background(), &emptypb.Empty{}); err == nil {
		t.Fatal("GetSessionInfoList() succeeded without server mux")
	} else if !strings.Contains(err.Error(), "server multiplexier is unavailable") {
		t.Fatalf("GetSessionInfoList() error = %q, want server mux unavailable", err.Error())
	}

	mux := protocol.NewMux(false)
	t.Cleanup(func() {
		if err := mux.Close(); err != nil {
			t.Errorf("mux.Close() failed: %v", err)
		}
	})
	SetServerMuxRef(mux)

	info, err := rpcServer.GetSessionInfoList(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetSessionInfoList() failed: %v", err)
	}
	if len(info.GetItems()) != 0 {
		t.Fatalf("GetSessionInfoList() returned %d items, want 0", len(info.GetItems()))
	}
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

func TestServerListenIPAddressConfig(t *testing.T) {
	config := &pb.ServerConfig{
		PortBindings: []*pb.PortBinding{{Port: proto.Int32(8000), Protocol: pb.TransportProtocol_TCP.Enum()}},
	}
	cases := []struct {
		name    string
		patch   string
		want    *string
		wantErr bool
	}{
		{"unset", `{}`, nil, false},
		{"set IPv4", `{"listenIPAddress":"127.0.0.1"}`, proto.String("127.0.0.1"), false},
		{"preserve", `{"loggingLevel":"INFO"}`, proto.String("127.0.0.1"), false},
		{"replace IPv6", `{"listenIPAddress":"::1"}`, proto.String("::1"), false},
		{"invalid hostname", `{"listenIPAddress":"localhost"}`, proto.String("::1"), true},
		{"invalid IP", `{"listenIPAddress":"300.1.2.3"}`, proto.String("::1"), true},
		{"invalid port", `{"listenIPAddress":"127.0.0.1:80"}`, proto.String("::1"), true},
		{"invalid brackets", `{"listenIPAddress":"[::1]"}`, proto.String("::1"), true},
		{"reset", `{"listenIPAddress":""}`, proto.String(""), false},
		{"preserve reset", `{}`, proto.String(""), false},
		{"IPv4 wildcard", `{"listenIPAddress":"0.0.0.0"}`, proto.String("0.0.0.0"), false},
		{"IPv6 wildcard", `{"listenIPAddress":"::"}`, proto.String("::"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			patch := &pb.ServerConfig{}
			if err := common.UnmarshalJSON([]byte(c.patch), patch); err != nil {
				t.Fatal(err)
			}
			full := proto.Clone(patch).(*pb.ServerConfig)
			full.PortBindings = []*pb.PortBinding{{Port: proto.Int32(8000), Protocol: pb.TransportProtocol_TCP.Enum()}}
			for _, err := range []error{ValidateServerConfigPatch(patch), ValidateFullServerConfig(full)} {
				if (err != nil) != c.wantErr {
					t.Fatalf("validation error = %v, want error %v", err, c.wantErr)
				}
			}
			if c.wantErr {
				return
			}
			if err := MergeServerConfig(config, patch); err != nil {
				t.Fatal(err)
			}
			if err := ValidateFullServerConfig(config); err != nil {
				t.Fatal(err)
			}
			if c.want == nil {
				if config.ListenIPAddress != nil {
					t.Fatalf("address = %q, want absent", config.GetListenIPAddress())
				}
			} else if config.ListenIPAddress == nil || config.GetListenIPAddress() != *c.want {
				t.Fatalf("address = %v (%q), want %q", config.ListenIPAddress, config.GetListenIPAddress(), *c.want)
			}
		})
	}
}

func TestServerReloadListenIPAddressConflict(t *testing.T) {
	beforeServerTest(t)
	defer afterServerTest(t)
	for _, transport := range []pb.TransportProtocol{pb.TransportProtocol_TCP, pb.TransportProtocol_UDP} {
		for _, address := range []string{"", "127.0.0.1"} {
			t.Run(fmt.Sprintf("%s/address=%s", transport, address), func(t *testing.T) {
				var port int
				var err error
				if transport == pb.TransportProtocol_TCP {
					port, err = common.UnusedTCPPort()
				} else {
					port, err = common.UnusedUDPPort()
				}
				if err != nil {
					t.Fatal(err)
				}
				config := &pb.ServerConfig{
					ListenIPAddress: proto.String(address),
					Users:           []*pb.User{{Name: proto.String("user"), Password: proto.String("password")}},
					PortBindings:    []*pb.PortBinding{{Port: proto.Int32(int32(port)), Protocol: transport.Enum()}},
				}
				proxy, err := newServerProxy(config)
				if err != nil {
					t.Fatal(err)
				}
				// Disable port reuse so overlapping addresses reliably fail to bind.
				factory := &net.ListenConfig{}
				proxy.mux.SetStreamListenerFactory(factory).SetPacketListenerFactory(factory)
				t.Cleanup(func() {
					SetServerMuxRef(nil)
					proxy.mux.Close()
				})
				if err := proxy.mux.Start(); err != nil {
					t.Fatal(err)
				}
				SetServerMuxRef(proxy.mux)
				if address == "" {
					config.ListenIPAddress = proto.String("127.0.0.1")
				} else {
					config.ListenIPAddress = proto.String("")
				}
				if err := StoreServerConfig(config); err != nil {
					t.Fatal(err)
				}
				service := NewServerManagementService()
				for i := 0; i < 2; i++ {
					if _, err := service.Reload(context.Background(), &emptypb.Empty{}); err == nil {
						t.Fatal("Reload() succeeded with a conflicting listenIPAddress")
					} else if !strings.Contains(err.Error(), "mita stop and mita start") {
						t.Fatalf("Reload() error lacks restart instructions: %v", err)
					}
				}
				config.ListenIPAddress = proto.String(address)
				if err := StoreServerConfig(config); err != nil {
					t.Fatal(err)
				}
				if _, err := service.Reload(context.Background(), &emptypb.Empty{}); err != nil {
					t.Fatalf("Reload() failed after restoring the original address: %v", err)
				}
			})
		}
	}
}

func TestServerProxyListenIPAddress(t *testing.T) {
	beforeServerTest(t)
	defer afterServerTest(t)
	for _, reload := range []bool{false, true} {
		for _, address := range []string{"", "127.0.0.1", "::1"} {
			t.Run(fmt.Sprintf("reload=%v/address=%s", reload, address), func(t *testing.T) {
				config := &pb.ServerConfig{
					ListenIPAddress: proto.String(address),
					Users:           []*pb.User{{Name: proto.String("user"), Password: proto.String("password")}},
					PortBindings: []*pb.PortBinding{
						{Port: proto.Int32(8000), Protocol: pb.TransportProtocol_TCP.Enum()},
						{Port: proto.Int32(8000), Protocol: pb.TransportProtocol_UDP.Enum()},
					},
				}
				var mux *protocol.Mux
				if reload {
					mux = protocol.NewMux(false)
					SetServerMuxRef(mux)
					t.Cleanup(func() { SetServerMuxRef(nil) })
					if err := StoreServerConfig(config); err != nil {
						t.Fatal(err)
					}
					if _, err := NewServerManagementService().Reload(context.Background(), &emptypb.Empty{}); err != nil {
						t.Fatal(err)
					}
				} else {
					proxy, err := newServerProxy(config)
					if err != nil {
						t.Fatal(err)
					}
					mux = proxy.mux
				}
				t.Cleanup(func() { mux.Close() })
				factory := &recordingListenerFactory{addresses: make(chan string, 2)}
				mux.SetStreamListenerFactory(factory).SetPacketListenerFactory(factory)
				if err := mux.Start(); err == nil {
					t.Fatal("Start() succeeded with failing listener factory")
				}
				for i := 0; i < 2; i++ {
					select {
					case got := <-factory.addresses:
						if want := net.JoinHostPort(address, "8000"); got != want {
							t.Errorf("listener address = %q, want %q", got, want)
						}
					default:
						t.Fatal("listener factory was not called")
					}
				}
			})
		}
	}
}

type recordingListenerFactory struct{ addresses chan string }

func (f *recordingListenerFactory) Listen(_ context.Context, _, address string) (net.Listener, error) {
	f.addresses <- address
	return nil, fmt.Errorf("test listener failure")
}

func (f *recordingListenerFactory) ListenPacket(_ context.Context, _, address string) (net.PacketConn, error) {
	f.addresses <- address
	return nil, fmt.Errorf("test listener failure")
}
