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
	"time"

	"github.com/enfein/mieru/v3/pkg/appctl"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/enfein/mieru/v3/pkg/egress"
	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/metrics"
	"github.com/enfein/mieru/v3/pkg/protocol"
	"github.com/enfein/mieru/v3/pkg/socks5"
	"github.com/enfein/mieru/v3/pkg/stderror"
	"github.com/enfein/mieru/v3/pkg/util"
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
		[]string{"", "reload"},
		func(s []string) error {
			return unexpectedArgsError(s, 2)
		},
		serverReloadFunc,
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
		[]string{"", "get", "metrics"},
		func(s []string) error {
			return unexpectedArgsError(s, 3)
		},
		serverGetMetricsFunc,
	)
	RegisterCallback(
		[]string{"", "get", "connections"},
		func(s []string) error {
			return unexpectedArgsError(s, 3)
		},
		serverGetConnectionsFunc,
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
		[]string{"", "get", "memory-statistics"},
		func(s []string) error {
			return unexpectedArgsError(s, 3)
		},
		serverGetMemoryStatisticsFunc,
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
	helpFmt := helpFormatter{
		appName: "mita",
		entries: []helpCmdEntry{
			{
				cmd:  "help",
				help: "Show mita server help.",
			},
			{
				cmd:  "start",
				help: "Start mita server proxy service.",
			},
			{
				cmd:  "stop",
				help: "Stop mita server proxy service.",
			},
			{
				cmd:  "reload",
				help: "Reload mita server configuration without stopping proxy service.",
			},
			{
				cmd:  "status",
				help: "Check mita server proxy service status.",
			},
			{
				cmd:  "apply config <FILE>",
				help: "Apply server configuration from JSON file.",
			},
			{
				cmd:  "describe config",
				help: "Show current server configuration.",
			},
			{
				cmd:  "delete user <USER_NAME>",
				help: "Delete a user from server configuration.",
			},
			{
				cmd:  "get metrics",
				help: "Get mita server metrics.",
			},
			{
				cmd:  "get connections",
				help: "Get mita server connections.",
			},
			{
				cmd:  "version",
				help: "Show mita server version.",
			},
			{
				cmd:  "check update",
				help: "Check mita server update.",
			},
		},
		advanced: []helpCmdEntry{
			{
				cmd:  "run",
				help: "Run mita server in foreground.",
			},
			{
				cmd:  "get thread-dump",
				help: "Get mita server thread dump.",
			},
			{
				cmd:  "get heap-profile <GZ_FILE>",
				help: "Get mita server heap profile and save results to the file.",
			},
			{
				cmd:  "get memory-statistics",
				help: "Get mita server memory statistics.",
			},
			{
				cmd:  "profile cpu start <GZ_FILE>",
				help: "Start mita server CPU profile and save results to the file.",
			},
			{
				cmd:  "profile cpu stop",
				help: "Stop mita server CPU profile.",
			},
		},
	}
	helpFmt.print()
	return nil
}

var serverStartFunc = func(s []string) error {
	appStatus, err := appctl.GetServerStatusWithRPC(context.Background())
	if err != nil {
		if stderror.IsConnRefused(err) {
			return fmt.Errorf(stderror.ServerNotRunningWithCommand)
		}
		return fmt.Errorf(stderror.GetServerStatusFailedErr, err)
	}
	if err := appctl.IsServerDaemonRunning(appStatus); err != nil {
		return fmt.Errorf(stderror.ServerNotRunningErr, err)
	}
	if err := appctl.IsServerProxyRunning(appStatus); err == nil {
		log.Infof("mita server proxy is running")
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
	log.Infof("mita server proxy is started")
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
		rpcAddr := appctl.ServerUDS()
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
		log.Infof("mita server daemon RPC server is running")
		if err = grpcServer.Serve(rpcListener); err != nil {
			log.Fatalf("run gRPC server failed: %v", err)
		}
		log.Infof("mita server daemon RPC server is stopped")
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
			return fmt.Errorf(stderror.GetServerConfigFailedErr, err)
		}
	}

	// Set logging level based on server config.
	loggingLevel := config.GetLoggingLevel().String()
	if loggingLevel != appctlpb.LoggingLevel_DEFAULT.String() {
		log.SetLevel(loggingLevel)
	}

	// Load previous metrics if possible.
	if err := os.MkdirAll("/var/lib/mita", 0775); err == nil {
		const metricsDumpPath = "/var/lib/mita/metrics.pb"
		metrics.SetMetricsDumpFilePath(metricsDumpPath)
		if err := metrics.LoadMetricsFromDump(); err == nil {
			log.Infof("Loaded previous metrics from %s", metricsDumpPath)
		} else {
			log.Infof("Failed to load previous metrics: %v", err)
		}
		if err := metrics.EnableMetricsDump(); err != nil {
			log.Warnf("Failed to enable metrics dump: %v", err)
		}
	}

	// Disable client side metrics.
	if clientDecryptionMetricGroup := metrics.GetMetricGroupByName(cipher.ClientDecryptionMetricGroupName); clientDecryptionMetricGroup != nil {
		clientDecryptionMetricGroup.DisableLogging()
	}
	if httpMetricGroup := metrics.GetMetricGroupByName(socks5.HTTPMetricGroupName); httpMetricGroup != nil {
		httpMetricGroup.DisableLogging()
	}

	// Detect and log TCP congestion control algorithm.
	if algo := util.TCPCongestionControlAlgorithm(); algo != "" {
		log.Infof("TCP congestion control algorithm is %q", algo)
	}

	// Start proxy if server config is valid.
	if err = appctl.ValidateFullServerConfig(config); err == nil {
		appctl.SetAppStatus(appctlpb.AppStatus_STARTING)

		mux := protocol.NewMux(false).SetServerUsers(appctl.UserListToMap(config.GetUsers()))
		appctl.SetServerMuxRef(mux)
		mtu := util.DefaultMTU
		if config.GetMtu() != 0 {
			mtu = int(config.GetMtu())
		}
		endpoints, err := appctl.PortBindingsToUnderlayProperties(config.GetPortBindings(), mtu)
		if err != nil {
			return err
		}
		mux.SetEndpoints(endpoints)

		// Create the egress socks5 server.
		socks5Config := &socks5.Config{
			AllowLocalDestination: config.GetAdvancedSettings().GetAllowLocalDestination(),
			AuthOpts: socks5.Auth{
				ClientSideAuthentication: true,
			},
			EgressController: egress.NewSocks5Controller(config.GetEgress()),
			HandshakeTimeout: 10 * time.Second,
		}
		socks5Server, err := socks5.New(socks5Config)
		if err != nil {
			return fmt.Errorf(stderror.CreateSocks5ServerFailedErr, err)
		}
		appctl.SetSocks5Server(socks5Server)

		// Run the egress socks5 server in the background.
		var proxyTasks sync.WaitGroup
		var initProxyTasks sync.WaitGroup
		proxyTasks.Add(1)
		initProxyTasks.Add(1)
		go func() {
			if err = mux.Start(); err != nil {
				log.Fatalf("socks5 server listening failed: %v", err)
			}
			initProxyTasks.Done()

			log.Infof("mita server daemon socks5 server is running")
			if err = socks5Server.Serve(mux); err != nil {
				log.Fatalf("run socks5 server failed: %v", err)
			}
			log.Infof("mita server daemon socks5 server is stopped")
			proxyTasks.Done()
		}()

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

	log.Infof("mita server daemon exit now")
	return nil
}

var serverStopFunc = func(s []string) error {
	appStatus, err := appctl.GetServerStatusWithRPC(context.Background())
	if err != nil {
		if stderror.IsConnRefused(err) {
			return fmt.Errorf(stderror.ServerNotRunningWithCommand)
		}
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
	log.Infof("mita server proxy is stopped")
	return nil
}

var serverReloadFunc = func(s []string) error {
	appStatus, err := appctl.GetServerStatusWithRPC(context.Background())
	if err != nil {
		if stderror.IsConnRefused(err) {
			return fmt.Errorf(stderror.ServerNotRunningWithCommand)
		}
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
	if _, err = client.Reload(timedctx, &appctlpb.Empty{}); err != nil {
		return fmt.Errorf(stderror.ReloadServerFailedErr, err)
	}
	log.Infof("mita server is reloaded")
	return nil
}

var serverStatusFunc = func(s []string) error {
	appStatus, err := appctl.GetServerStatusWithRPC(context.Background())
	if err != nil {
		if stderror.IsConnRefused(err) {
			return fmt.Errorf(stderror.ServerNotRunningWithCommand)
		} else if stderror.IsPermissionDenied(err) {
			currentUser, err := user.Current()
			if err != nil {
				cmd := strings.Join(s, " ")
				return fmt.Errorf("unable to determine the OS user which executed command %q", cmd)
			}
			return fmt.Errorf("unable to connect to mita server daemon via %q; please retry after running \"sudo usermod -a -G mita %s\" command and logout the system, then login again", appctl.ServerUDS(), currentUser.Username)
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
		log.Infof("mita server status is %q", appctlpb.AppStatus_RUNNING.String())
	}
	return nil
}

var serverApplyConfigFunc = func(s []string) error {
	appStatus, err := appctl.GetServerStatusWithRPC(context.Background())
	if err != nil {
		if stderror.IsConnRefused(err) {
			return fmt.Errorf(stderror.ServerNotRunningWithCommand)
		}
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
	if err = util.UnmarshalJSON(b, patch); err != nil {
		return fmt.Errorf("util.UnmarshalJSON() failed: %w", err)
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
		if stderror.IsConnRefused(err) {
			return fmt.Errorf(stderror.ServerNotRunningWithCommand)
		}
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
	jsonBytes, err := util.MarshalJSON(config)
	if err != nil {
		return fmt.Errorf("util.MarshalJSON() failed: %w", err)
	}
	log.Infof("%s", string(jsonBytes))
	return nil
}

var serverDeleteUserFunc = func(s []string) error {
	appStatus, err := appctl.GetServerStatusWithRPC(context.Background())
	if err != nil {
		if stderror.IsConnRefused(err) {
			return fmt.Errorf(stderror.ServerNotRunningWithCommand)
		}
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

var serverGetMetricsFunc = func(s []string) error {
	appStatus, err := appctl.GetServerStatusWithRPC(context.Background())
	if err != nil {
		if stderror.IsConnRefused(err) {
			return fmt.Errorf(stderror.ServerNotRunningWithCommand)
		}
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
	metrics, err := client.GetMetrics(timedctx, &appctlpb.Empty{})
	if err != nil {
		return fmt.Errorf(stderror.GetMetricsFailedErr, err)
	}
	log.Infof("%s", metrics.GetJson())
	return nil
}

var serverGetConnectionsFunc = func(s []string) error {
	appStatus, err := appctl.GetServerStatusWithRPC(context.Background())
	if err != nil {
		if stderror.IsConnRefused(err) {
			return fmt.Errorf(stderror.ServerNotRunningWithCommand)
		}
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
	info, err := client.GetSessionInfo(timedctx, &appctlpb.Empty{})
	if err != nil {
		return fmt.Errorf(stderror.GetConnectionsFailedErr, err)
	}
	for _, line := range info.GetTable() {
		log.Infof("%s", line)
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

var serverGetMemoryStatisticsFunc = func(s []string) error {
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
	memStats, err := client.GetMemoryStatistics(timedctx, &appctlpb.Empty{})
	if err != nil {
		return fmt.Errorf(stderror.GetMemoryStatisticsFailedErr, err)
	}
	log.Infof("%s", memStats.GetJson())
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
		return fmt.Errorf("convert mita UID with strconv.Atoi(%q) failed: %w", mitaGidStr, err)
	}
	if err = os.Chown(appctl.ServerUDS(), rootUid, mitaGid); err != nil {
		return fmt.Errorf("os.Chown(%q) failed: %w", appctl.ServerUDS(), err)
	}
	if err = os.Chmod(appctl.ServerUDS(), 0770); err != nil {
		return fmt.Errorf("os.Chmod(%q) failed: %w", appctl.ServerUDS(), err)
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
