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

package cli

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/user"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/enfein/mieru/pkg/appctl"
	"github.com/enfein/mieru/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/pkg/cipher"
	"github.com/enfein/mieru/pkg/http2socks"
	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/metrics"
	"github.com/enfein/mieru/pkg/netutil"
	"github.com/enfein/mieru/pkg/socks5"
	"github.com/enfein/mieru/pkg/stderror"
	"github.com/enfein/mieru/pkg/tcpsession"
	"github.com/enfein/mieru/pkg/udpsession"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// RegisterServerCommands registers all the server side CLI commands.
func RegisterServerCommands() {
	binaryName = "mita"
	RegisterCallback(
		[]string{"", "help"},
		func(s []string) error {
			return unexpectedArgsError(s, 2)
		},
		serverHelpFunc,
	)
	RegisterCallback(
		[]string{"", "start"},
		func(s []string) error {
			return unexpectedArgsError(s, 2)
		},
		serverStartFunc,
	)
	RegisterCallback(
		[]string{"", "run"},
		func(s []string) error {
			return unexpectedArgsError(s, 2)
		},
		serverRunFunc,
	)
	RegisterCallback(
		[]string{"", "stop"},
		func(s []string) error {
			return unexpectedArgsError(s, 2)
		},
		serverStopFunc,
	)
	RegisterCallback(
		[]string{"", "status"},
		func(s []string) error {
			return unexpectedArgsError(s, 2)
		},
		serverStatusFunc,
	)
	RegisterCallback(
		[]string{"", "apply", "config"},
		func(s []string) error {
			if len(s) < 4 {
				return fmt.Errorf("usage: mita apply config <FILE>. no config file is provided")
			} else if len(s) > 4 {
				return fmt.Errorf("usage: mita apply config <FILE>. more than 1 config file is provided")
			}
			return nil
		},
		serverApplyConfigFunc,
	)
	RegisterCallback(
		[]string{"", "describe", "config"},
		func(s []string) error {
			return unexpectedArgsError(s, 3)
		},
		serverDescribeConfigFunc,
	)
	RegisterCallback(
		[]string{"", "delete", "user"},
		func(s []string) error {
			if len(s) < 4 {
				return fmt.Errorf("usage: mita delete user <USER_NAME>. no user is provided")
			}
			return nil
		},
		serverDeleteUserFunc,
	)
	RegisterCallback(
		[]string{"", "version"},
		func(s []string) error {
			return unexpectedArgsError(s, 2)
		},
		versionFunc,
	)
	RegisterCallback(
		[]string{"", "check", "update"},
		func(s []string) error {
			return unexpectedArgsError(s, 3)
		},
		checkUpdateFunc,
	)
	RegisterCallback(
		[]string{"", "get", "thread-dump"},
		func(s []string) error {
			return unexpectedArgsError(s, 3)
		},
		serverGetThreadDumpFunc,
	)
	RegisterCallback(
		[]string{"", "get", "heap-profile"},
		func(s []string) error {
			if len(s) < 4 {
				return fmt.Errorf("usage: mita get heap-profile <FILE>. no file save path is provided")
			} else if len(s) > 4 {
				return fmt.Errorf("usage: mita get heap-profile <FILE>. more than 1 file save path is provided")
			}
			return nil
		},
		serverGetHeapProfileFunc,
	)
	RegisterCallback(
		[]string{"", "profile", "cpu", "start"},
		func(s []string) error {
			if len(s) < 5 {
				return fmt.Errorf("usage: mita profile cpu start <FILE>. no file save path is provided")
			} else if len(s) > 5 {
				return fmt.Errorf("usage: mita profile cpu start <FILE>. more than 1 file save path is provided")
			}
			return nil
		},
		serverStartCPUProfileFunc,
	)
	RegisterCallback(
		[]string{"", "profile", "cpu", "stop"},
		func(s []string) error {
			return unexpectedArgsError(s, 4)
		},
		serverStopCPUProfileFunc,
	)
}

var serverHelpFunc = func(s []string) error {
	format := "  %-32v%-46v"
	helpCmd := fmt.Sprintf(format, "help", "Show mieru server help")
	startCmd := fmt.Sprintf(format, "start", "Start mieru server")
	stopCmd := fmt.Sprintf(format, "stop", "Stop mieru server")
	statusCmd := fmt.Sprintf(format, "status", "Check mieru server status")
	applyConfigCmd := fmt.Sprintf(format, "apply config <FILE>", "Apply server configuration from JSON file")
	describeConfigCmd := fmt.Sprintf(format, "describe config", "Show current server configuration")
	deleteUserCmd := fmt.Sprintf(format, "delete user <USER_NAME>", "Delete users from server configuration")
	versionCmd := fmt.Sprintf(format, "version", "Show mieru server version")
	checkUpdateCmd := fmt.Sprintf(format, "check update", "Check mieru server update")
	log.Infof("Usage: %s <COMMAND> [<ARGS>]", binaryName)
	log.Infof("")
	log.Infof("Commands:")
	log.Infof("%s", helpCmd)
	log.Infof("%s", startCmd)
	log.Infof("%s", stopCmd)
	log.Infof("%s", statusCmd)
	log.Infof("%s", applyConfigCmd)
	log.Infof("%s", describeConfigCmd)
	log.Infof("%s", deleteUserCmd)
	log.Infof("%s", versionCmd)
	log.Infof("%s", checkUpdateCmd)
	return nil
}

var serverStartFunc = func(s []string) error {
	appStatus, err := appctl.GetServerStatusWithRPC(context.Background())
	if err != nil {
		return fmt.Errorf(stderror.GetServerStatusFailedErr, err)
	}
	if err := appctl.IsServerDaemonRunning(appStatus); err != nil {
		return fmt.Errorf(stderror.ServerNotRunningErr, err)
	}
	if err := appctl.IsServerProxyRunning(appStatus); err == nil {
		log.Infof("mieru server proxy is running")
		return nil
	}

	// Start server proxy.
	client, err := appctl.NewServerLifecycleRPCClient()
	if err != nil {
		return fmt.Errorf(stderror.CreateServerLifecycleRPCClientFailedErr, err)
	}
	timedctx, cancelFunc := context.WithTimeout(context.Background(), appctl.RPCTimeout)
	defer cancelFunc()
	_, err = client.Start(timedctx, &appctlpb.Empty{})
	if err != nil {
		return fmt.Errorf(stderror.StartServerProxyFailedErr, err)
	}
	log.Infof("mieru server proxy is started")
	return nil
}

var serverRunFunc = func(s []string) error {
	if _, found := os.LookupEnv("MITA_LOG_NO_TIMESTAMP"); found {
		log.SetFormatter(&log.DaemonFormatter{NoTimestamp: true})
	} else {
		log.SetFormatter(&log.DaemonFormatter{})
	}

	appctl.SetAppStatus(appctlpb.AppStatus_IDLE)

	var rpcTasks sync.WaitGroup
	rpcTasks.Add(1)

	// Run the RPC server in the background.
	go func() {
		rpcAddr := appctl.ServerUDS
		if err := syscall.Unlink(rpcAddr); err != nil {
			// Unlink() fails when the file path doesn't exist, which is not a big problem.
			log.Debugf("syscall.Unlink(%q) failed: %v", rpcAddr, err)
		}
		rpcListener, err := net.Listen("unix", rpcAddr)
		if err != nil {
			log.Fatalf("listen on RPC address %q failed: %v", rpcAddr, err)
		}
		if _, found := os.LookupEnv("MITA_INSECURE_UDS"); !found {
			if err = updateServerUDSPermission(); err != nil {
				log.Fatalf("update server unix domain socket permission failed: %v", err)
			}
		}
		grpcServer := grpc.NewServer()
		appctl.SetServerRPCServerRef(grpcServer)
		appctlpb.RegisterServerLifecycleServiceServer(grpcServer, appctl.NewServerLifecycleService())
		appctlpb.RegisterServerConfigServiceServer(grpcServer, appctl.NewServerConfigService())
		close(appctl.ServerRPCServerStarted)
		log.Infof("mieru server daemon RPC server is running")
		if err = grpcServer.Serve(rpcListener); err != nil {
			log.Fatalf("run gRPC server failed: %v", err)
		}
		log.Infof("mieru server daemon RPC server is stopped")
		rpcTasks.Done()
	}()
	<-appctl.ServerRPCServerStarted

	// Load server config. If server config file doesn't exist, create a new one.
	config, err := appctl.LoadServerConfig()
	if err != nil {
		if err == stderror.ErrFileNotExist {
			if err = appctl.StoreServerConfig(&appctlpb.ServerConfig{}); err != nil {
				return fmt.Errorf(stderror.CreateEmptyServerConfigFailedErr, err)
			}
		}
	}

	// Load server config again if needed.
	if config == nil {
		config, err = appctl.LoadServerConfig()
		if err != nil {
			return fmt.Errorf(stderror.LoadServerConfigFailedErr, err)
		}
	}

	// Set logging level based on server config.
	loggingLevel := config.GetLoggingLevel().String()
	if loggingLevel != appctlpb.LoggingLevel_DEFAULT.String() {
		log.SetLevel(loggingLevel)
	}

	// Disable client side metrics.
	if clientDecryptionMetricGroup := metrics.GetMetricGroupByName(cipher.ClientDecryptionMetricGroupName); clientDecryptionMetricGroup != nil {
		clientDecryptionMetricGroup.DisableLogging()
	}
	if httpMetricGroup := metrics.GetMetricGroupByName(http2socks.HTTPMetricGroupName); httpMetricGroup != nil {
		httpMetricGroup.DisableLogging()
	}

	// Start proxy if server config is valid.
	if err = appctl.ValidateFullServerConfig(config); err == nil {
		appctl.SetAppStatus(appctlpb.AppStatus_STARTING)

		// Set MTU for UDP sessions.
		if config.GetMtu() != 0 {
			udpsession.SetGlobalMTU(config.GetMtu())
		}

		n := len(config.GetPortBindings())
		var proxyTasks sync.WaitGroup
		var initProxyTasks sync.WaitGroup
		proxyTasks.Add(n)
		initProxyTasks.Add(n)

		for i := 0; i < n; i++ {
			// Create the egress socks5 server.
			socks5Config := &socks5.Config{
				AllowLocalDestination: config.GetAdvancedSettings().GetAllowLocalDestination(),
			}
			socks5Server, err := socks5.New(socks5Config)
			if err != nil {
				return fmt.Errorf(stderror.CreateSocks5ServerFailedErr, err)
			}
			protocol := config.GetPortBindings()[i].GetProtocol()
			port := config.GetPortBindings()[i].GetPort()
			if err := appctl.GetSocks5ServerGroup().Add(protocol.String(), int(port), socks5Server); err != nil {
				return fmt.Errorf(stderror.AddSocks5ServerToGroupFailedErr, err)
			}

			// Run the egress socks5 server in the background.
			go func() {
				socks5Addr := netutil.MaybeDecorateIPv6(netutil.AllIPAddr()) + ":" + strconv.Itoa(int(port))
				var l net.Listener
				var err error
				if protocol == appctlpb.TransportProtocol_TCP {
					l, err = tcpsession.ListenWithOptions(socks5Addr, appctl.UserListToMap(config.GetUsers()))
					if err != nil {
						log.Fatalf("tcpsession.ListenWithOptions(%q) failed: %v", socks5Addr, err)
					}
				} else if protocol == appctlpb.TransportProtocol_UDP {
					l, err = udpsession.ListenWithOptions(socks5Addr, appctl.UserListToMap(config.GetUsers()))
					if err != nil {
						log.Fatalf("udpsession.ListenWithOptions(%q) failed: %v", socks5Addr, err)
					}
				} else {
					log.Fatalf("found unknown transport protocol %s in server config", protocol.String())
				}
				initProxyTasks.Done()
				log.Infof("mieru server daemon socks5 server %q is running", socks5Addr)
				if err = socks5Server.Serve(l); err != nil {
					log.Fatalf("run socks5 server %q failed: %v", socks5Addr, err)
				}
				log.Infof("mieru server daemon socks5 server %q is stopped", socks5Addr)
				proxyTasks.Done()
			}()
		}

		initProxyTasks.Wait()
		metrics.EnableLogging()
		appctl.SetAppStatus(appctlpb.AppStatus_RUNNING)
		proxyTasks.Wait()
	}
	// If fails to validate server configuration, do nothing.
	// Most likely the server configuration is empty.
	// It will be set by new RPC calls.

	rpcTasks.Wait()

	// Stop CPU profiling, if previously started.
	pprof.StopCPUProfile()

	log.Infof("mieru server daemon exit now")
	return nil
}

var serverStopFunc = func(s []string) error {
	appStatus, err := appctl.GetServerStatusWithRPC(context.Background())
	if err != nil {
		return fmt.Errorf(stderror.GetServerStatusFailedErr, err)
	}
	if err := appctl.IsServerDaemonRunning(appStatus); err != nil {
		return fmt.Errorf(stderror.ServerNotRunningErr, err)
	}
	if err := appctl.IsServerProxyRunning(appStatus); err != nil {
		return fmt.Errorf(stderror.ServerProxyNotRunningErr, err)
	}

	// Stop server proxy.
	client, err := appctl.NewServerLifecycleRPCClient()
	if err != nil {
		return fmt.Errorf(stderror.CreateServerLifecycleRPCClientFailedErr, err)
	}
	timedctx, cancelFunc := context.WithTimeout(context.Background(), appctl.RPCTimeout)
	defer cancelFunc()
	if _, err = client.Stop(timedctx, &appctlpb.Empty{}); err != nil {
		return fmt.Errorf(stderror.StopServerProxyFailedErr, err)
	}
	log.Infof("mieru server proxy is stopped")
	return nil
}

var serverStatusFunc = func(s []string) error {
	appStatus, err := appctl.GetServerStatusWithRPC(context.Background())
	if err != nil {
		if stderror.IsConnRefused(err) {
			// This is the most common reason, no need to show more details.
			return fmt.Errorf(stderror.ServerNotRunning)
		} else if stderror.IsPermissionDenied(err) {
			currentUser, err := user.Current()
			if err != nil {
				cmd := strings.Join(s, " ")
				return fmt.Errorf("unable to determine the OS user which executed command %q", cmd)
			}
			return fmt.Errorf("unable to connect to mieru server daemon through %q, please retry after running \"sudo usermod -a -G mita %s\" command and reboot the system", appctl.ServerUDS, currentUser.Username)
		} else {
			return fmt.Errorf(stderror.GetServerStatusFailedErr, err)
		}
	}
	if err := appctl.IsServerDaemonRunning(appStatus); err != nil {
		return fmt.Errorf(stderror.ServerNotRunningErr, err)
	}
	if err := appctl.IsServerProxyRunning(appStatus); err != nil {
		log.Infof("%s", err.Error())
	} else {
		log.Infof("mieru server status is %q", appctlpb.AppStatus_RUNNING.String())
	}
	return nil
}

var serverApplyConfigFunc = func(s []string) error {
	appStatus, err := appctl.GetServerStatusWithRPC(context.Background())
	if err != nil {
		return fmt.Errorf(stderror.GetServerStatusFailedErr, err)
	}
	if err := appctl.IsServerDaemonRunning(appStatus); err != nil {
		return fmt.Errorf(stderror.ServerNotRunningErr, err)
	}

	path := s[3]
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("os.ReadFile(%q) failed: %w", path, err)
	}
	patch := &appctlpb.ServerConfig{}
	if err = appctl.Unmarshal(b, patch); err != nil {
		return fmt.Errorf("protojson.Unmarshal() failed: %w", err)
	}
	if err := appctl.ValidateServerConfigPatch(patch); err != nil {
		return fmt.Errorf(stderror.ValidateServerConfigPatchFailedErr, err)
	}

	client, err := appctl.NewServerConfigRPCClient()
	if err != nil {
		return fmt.Errorf(stderror.CreateServerConfigRPCClientFailedErr, err)
	}
	timedctx, cancelFunc := context.WithTimeout(context.Background(), appctl.RPCTimeout)
	defer cancelFunc()
	_, err = client.SetConfig(timedctx, patch)
	if err != nil {
		return fmt.Errorf(stderror.SetServerConfigFailedErr, err)
	}
	return nil
}

var serverDescribeConfigFunc = func(s []string) error {
	appStatus, err := appctl.GetServerStatusWithRPC(context.Background())
	if err != nil {
		return fmt.Errorf(stderror.GetServerStatusFailedErr, err)
	}
	if err := appctl.IsServerDaemonRunning(appStatus); err != nil {
		return fmt.Errorf(stderror.ServerNotRunningErr, err)
	}

	client, err := appctl.NewServerConfigRPCClient()
	if err != nil {
		return fmt.Errorf(stderror.CreateServerConfigRPCClientFailedErr, err)
	}
	timedctx, cancelFunc := context.WithTimeout(context.Background(), appctl.RPCTimeout)
	defer cancelFunc()
	config, err := client.GetConfig(timedctx, &appctlpb.Empty{})
	if err != nil {
		return fmt.Errorf(stderror.GetServerConfigFailedErr, err)
	}
	jsonBytes, err := appctl.Marshal(config)
	if err != nil {
		return fmt.Errorf("protojson.Marshal() failed: %w", err)
	}
	log.Infof("%s", string(jsonBytes))
	return nil
}

var serverDeleteUserFunc = func(s []string) error {
	appStatus, err := appctl.GetServerStatusWithRPC(context.Background())
	if err != nil {
		return fmt.Errorf(stderror.GetServerStatusFailedErr, err)
	}
	if err := appctl.IsServerDaemonRunning(appStatus); err != nil {
		return fmt.Errorf(stderror.ServerNotRunningErr, err)
	}

	client, err := appctl.NewServerConfigRPCClient()
	if err != nil {
		return fmt.Errorf(stderror.CreateServerConfigRPCClientFailedErr, err)
	}
	timedctx, cancelFunc := context.WithTimeout(context.Background(), appctl.RPCTimeout)
	defer cancelFunc()
	config, err := client.GetConfig(timedctx, &appctlpb.Empty{})
	if err != nil {
		return fmt.Errorf(stderror.GetServerConfigFailedErr, err)
	}
	users := config.GetUsers()
	remaining := make([]*appctlpb.User, 0)
	for _, user := range users {
		shouldDelete := false
		for _, toDelete := range s[3:] {
			if user.GetName() == toDelete {
				shouldDelete = true
				break
			}
		}
		if !shouldDelete {
			remaining = append(remaining, user)
		}
	}
	config.Users = remaining
	_, err = client.SetConfig(timedctx, config)
	if err != nil {
		return fmt.Errorf(stderror.SetServerConfigFailedErr, err)
	}
	return nil
}

var serverGetThreadDumpFunc = func(s []string) error {
	appStatus, err := appctl.GetServerStatusWithRPC(context.Background())
	if err != nil {
		return fmt.Errorf(stderror.GetServerStatusFailedErr, err)
	}
	if err := appctl.IsServerDaemonRunning(appStatus); err != nil {
		return fmt.Errorf(stderror.ServerNotRunningErr, err)
	}

	client, err := appctl.NewServerLifecycleRPCClient()
	if err != nil {
		return fmt.Errorf(stderror.CreateServerLifecycleRPCClientFailedErr, err)
	}
	timedctx, cancelFunc := context.WithTimeout(context.Background(), appctl.RPCTimeout)
	defer cancelFunc()
	dump, err := client.GetThreadDump(timedctx, &appctlpb.Empty{})
	if err != nil {
		return fmt.Errorf(stderror.GetThreadDumpFailedErr, err)
	}
	log.Infof("%s", dump.GetThreadDump())
	return nil
}

var serverGetHeapProfileFunc = func(s []string) error {
	appStatus, err := appctl.GetServerStatusWithRPC(context.Background())
	if err != nil {
		return fmt.Errorf(stderror.GetServerStatusFailedErr, err)
	}
	if err := appctl.IsServerDaemonRunning(appStatus); err != nil {
		return fmt.Errorf(stderror.ServerNotRunningErr, err)
	}

	client, err := appctl.NewServerLifecycleRPCClient()
	if err != nil {
		return fmt.Errorf(stderror.CreateServerLifecycleRPCClientFailedErr, err)
	}
	timedctx, cancelFunc := context.WithTimeout(context.Background(), appctl.RPCTimeout)
	defer cancelFunc()
	if _, err := client.GetHeapProfile(timedctx, &appctlpb.ProfileSavePath{FilePath: proto.String(s[3])}); err != nil {
		return fmt.Errorf(stderror.GetHeapProfileFailedErr, err)
	}
	log.Infof("heap profile is saved to %q", s[3])
	return nil
}

var serverStartCPUProfileFunc = func(s []string) error {
	appStatus, err := appctl.GetServerStatusWithRPC(context.Background())
	if err != nil {
		return fmt.Errorf(stderror.GetServerStatusFailedErr, err)
	}
	if err := appctl.IsServerDaemonRunning(appStatus); err != nil {
		return fmt.Errorf(stderror.ServerNotRunningErr, err)
	}

	client, err := appctl.NewServerLifecycleRPCClient()
	if err != nil {
		return fmt.Errorf(stderror.CreateServerLifecycleRPCClientFailedErr, err)
	}
	timedctx, cancelFunc := context.WithTimeout(context.Background(), appctl.RPCTimeout)
	defer cancelFunc()
	if _, err := client.StartCPUProfile(timedctx, &appctlpb.ProfileSavePath{FilePath: proto.String(s[4])}); err != nil {
		return fmt.Errorf(stderror.StartCPUProfileFailedErr, err)
	}
	log.Infof("CPU profile will be saved to %q", s[4])
	return nil
}

var serverStopCPUProfileFunc = func(s []string) error {
	appStatus, err := appctl.GetServerStatusWithRPC(context.Background())
	if err != nil {
		return fmt.Errorf(stderror.GetServerStatusFailedErr, err)
	}
	if err := appctl.IsServerDaemonRunning(appStatus); err != nil {
		return fmt.Errorf(stderror.ServerNotRunningErr, err)
	}

	client, err := appctl.NewServerLifecycleRPCClient()
	if err != nil {
		return fmt.Errorf(stderror.CreateServerLifecycleRPCClientFailedErr, err)
	}
	timedctx, cancelFunc := context.WithTimeout(context.Background(), appctl.RPCTimeout)
	defer cancelFunc()
	client.StopCPUProfile(timedctx, &appctlpb.Empty{})
	return nil
}

// Update server unix domain socket permission to 770, belongs to root:mita.
func updateServerUDSPermission() error {
	rootUidStr, err := getUid("root")
	if err != nil {
		return fmt.Errorf("getUid(%q) failed: %w", "root", err)
	}
	rootUid, err := strconv.Atoi(rootUidStr)
	if err != nil {
		return fmt.Errorf("convert root UID with strconv.Atoi(%q) failed: %w", rootUidStr, err)
	}
	mitaGidStr, err := getGid("mita")
	if err != nil {
		return fmt.Errorf("getGid(%q) failed: %w", "mita", err)
	}
	mitaGid, err := strconv.Atoi(mitaGidStr)
	if err != nil {
		return fmt.Errorf("convert mieru UID with strconv.Atoi(%q) failed: %w", mitaGidStr, err)
	}
	if err = os.Chown(appctl.ServerUDS, rootUid, mitaGid); err != nil {
		return fmt.Errorf("os.Chown(%q) failed: %w", appctl.ServerUDS, err)
	}
	if err = os.Chmod(appctl.ServerUDS, 0770); err != nil {
		return fmt.Errorf("os.Chmod(%q) failed: %w", appctl.ServerUDS, err)
	}
	return nil
}

func getUid(username string) (string, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return "", err
	}
	return u.Uid, nil
}

func getGid(username string) (string, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return "", err
	}
	return u.Gid, nil
}
