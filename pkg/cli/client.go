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
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime/pprof"
	"strconv"
	"sync"
	"time"

	"github.com/enfein/mieru/pkg/appctl"
	"github.com/enfein/mieru/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/pkg/cipher"
	"github.com/enfein/mieru/pkg/http2socks"
	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/metrics"
	"github.com/enfein/mieru/pkg/socks5"
	"github.com/enfein/mieru/pkg/stderror"
	"github.com/enfein/mieru/pkg/tcpsession"
	"github.com/enfein/mieru/pkg/udpsession"
	"github.com/enfein/mieru/pkg/util"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// RegisterClientCommands registers all the client side CLI commands.
func RegisterClientCommands() {
	RegisterCallback(
		[]string{"", "help"},
		func(s []string) error {
			return unexpectedArgsError(s, 2)
		},
		clientHelpFunc,
	)
	RegisterCallback(
		[]string{"", "start"},
		func(s []string) error {
			return unexpectedArgsError(s, 2)
		},
		clientStartFunc,
	)
	RegisterCallback(
		[]string{"", "run"},
		func(s []string) error {
			return unexpectedArgsError(s, 2)
		},
		clientRunFunc,
	)
	RegisterCallback(
		[]string{"", "stop"},
		func(s []string) error {
			return unexpectedArgsError(s, 2)
		},
		clientStopFunc,
	)
	RegisterCallback(
		[]string{"", "status"},
		func(s []string) error {
			return unexpectedArgsError(s, 2)
		},
		clientStatusFunc,
	)
	RegisterCallback(
		[]string{"", "apply", "config"},
		func(s []string) error {
			if len(s) < 4 {
				return fmt.Errorf("usage: mieru apply config <FILE>. No config file is provided")
			} else if len(s) > 4 {
				return fmt.Errorf("usage: mieru apply config <FILE>. More than 1 config file is provided")
			}
			return nil
		},
		clientApplyConfigFunc,
	)
	RegisterCallback(
		[]string{"", "describe", "config"},
		func(s []string) error {
			return unexpectedArgsError(s, 3)
		},
		clientDescribeConfigFunc,
	)
	RegisterCallback(
		[]string{"", "import", "config"},
		func(s []string) error {
			if len(s) < 4 {
				return fmt.Errorf("usage: mieru import config <URL>. No URL is provided")
			} else if len(s) > 4 {
				return fmt.Errorf("usage: mieru import config <URL>. More than 1 URL is provided")
			}
			return nil
		},
		clientImportConfigFunc,
	)
	RegisterCallback(
		[]string{"", "export", "config"},
		func(s []string) error {
			return unexpectedArgsError(s, 3)
		},
		clientExportConfigFunc,
	)
	RegisterCallback(
		[]string{"", "delete", "profile"},
		func(s []string) error {
			if len(s) < 4 {
				return fmt.Errorf("usage: mieru delete profile <PROFILE_NAME>. no profile is provided")
			} else if len(s) > 4 {
				return fmt.Errorf("usage: mieru delete profile <PROFILE_NAME>. more than 1 profile is provided")
			}
			return nil
		},
		clientDeleteProfileFunc,
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
		clientGetThreadDumpFunc,
	)
	RegisterCallback(
		[]string{"", "get", "heap-profile"},
		func(s []string) error {
			if len(s) < 4 {
				return fmt.Errorf("usage: mieru get heap-profile <FILE>. no file save path is provided")
			} else if len(s) > 4 {
				return fmt.Errorf("usage: mieru get heap-profile <FILE>. more than 1 file save path is provided")
			}
			return nil
		},
		clientGetHeapProfileFunc,
	)
	RegisterCallback(
		[]string{"", "profile", "cpu", "start"},
		func(s []string) error {
			if len(s) < 5 {
				return fmt.Errorf("usage: mieru profile cpu start <FILE>. no file save path is provided")
			} else if len(s) > 5 {
				return fmt.Errorf("usage: mieru profile cpu start <FILE>. more than 1 file save path is provided")
			}
			return nil
		},
		clientStartCPUProfileFunc,
	)
	RegisterCallback(
		[]string{"", "profile", "cpu", "stop"},
		func(s []string) error {
			return unexpectedArgsError(s, 4)
		},
		clientStopCPUProfileFunc,
	)
}

var clientHelpFunc = func(s []string) error {
	format := "  %-32v%-46v"
	helpCmd := fmt.Sprintf(format, "help", "Show mieru client help")
	startCmd := fmt.Sprintf(format, "start", "Start mieru client")
	stopCmd := fmt.Sprintf(format, "stop", "Stop mieru client")
	statusCmd := fmt.Sprintf(format, "status", "Check mieru client status")
	applyConfigCmd := fmt.Sprintf(format, "apply config <FILE>", "Apply client configuration from JSON file")
	describeConfigCmd := fmt.Sprintf(format, "describe config", "Show current client configuration")
	importConfigCmd := fmt.Sprintf(format, "import config <URL>", "Import client configuration from URL")
	exportConfigCmd := fmt.Sprintf(format, "export config", "Export client configuration as URL")
	deleteProfileCmd := fmt.Sprintf(format, "delete profile <PROFILE_NAME>", "Delete a client configuration profile")
	versionCmd := fmt.Sprintf(format, "version", "Show mieru client version")
	checkUpdateCmd := fmt.Sprintf(format, "check update", "Check mieru client update")
	log.Infof("Usage: %s <COMMAND> [<ARGS>]", binaryName)
	log.Infof("")
	log.Infof("Commands:")
	log.Infof("%s", helpCmd)
	log.Infof("%s", startCmd)
	log.Infof("%s", stopCmd)
	log.Infof("%s", statusCmd)
	log.Infof("%s", applyConfigCmd)
	log.Infof("%s", describeConfigCmd)
	log.Infof("%s", importConfigCmd)
	log.Infof("%s", exportConfigCmd)
	log.Infof("%s", deleteProfileCmd)
	log.Infof("%s", versionCmd)
	log.Infof("%s", checkUpdateCmd)
	return nil
}

var clientStartFunc = func(s []string) error {
	// Load and verify client config.
	config, err := appctl.LoadClientConfig()
	if err != nil {
		if err == stderror.ErrFileNotExist {
			return fmt.Errorf(stderror.ClientConfigNotExist)
		} else {
			return fmt.Errorf(stderror.LoadClientConfigFailedErr, err)
		}
	}
	if err = appctl.ValidateFullClientConfig(config); err != nil {
		return fmt.Errorf(stderror.ValidateFullClientConfigFailedErr, err)
	}

	if err = appctl.IsClientDaemonRunning(context.Background()); err == nil {
		if config.GetSocks5ListenLAN() {
			log.Infof("mieru client is running, listening to 0.0.0.0:%d", config.GetSocks5Port())
		} else {
			log.Infof("mieru client is running, listening to 127.0.0.1:%d", config.GetSocks5Port())
		}
		return nil
	}

	cmd := exec.Command(s[0], "run")
	if errors.Is(cmd.Err, exec.ErrDot) {
		cmd.Err = nil
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf(stderror.StartClientFailedErr, err)
	}

	// Wait until client daemon is running.
	// The maximum waiting time is 10 seconds.
	var lastErr error
	for i := 0; i < 100; i++ {
		lastErr = appctl.IsClientDaemonRunning(context.Background())
		if lastErr == nil {
			if config.GetSocks5ListenLAN() {
				log.Infof("mieru client is started, listening to 0.0.0.0:%d", config.GetSocks5Port())
			} else {
				log.Infof("mieru client is started, listening to 127.0.0.1:%d", config.GetSocks5Port())
			}
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf(stderror.ClientNotRunningErr, lastErr)
}

var clientRunFunc = func(s []string) error {
	log.SetFormatter(&log.DaemonFormatter{})
	appctl.SetAppStatus(appctlpb.AppStatus_STARTING)

	logFile, err := log.NewClientLogFile()
	if err == nil {
		log.SetOutput(logFile)
		if err = log.RemoveOldClientLogFiles(); err != nil {
			log.Errorf("remove old client log files failed: %v", err)
		}
	} else {
		log.Infof("log to stdout due to the following reason: %v", err)
	}

	// Load and verify client config.
	config, err := appctl.LoadClientConfig()
	if err != nil {
		if err == stderror.ErrFileNotExist {
			return fmt.Errorf(stderror.ClientConfigNotExist)
		} else {
			return fmt.Errorf(stderror.LoadClientConfigFailedErr, err)
		}
	}
	if proto.Equal(config, &appctlpb.ClientConfig{}) {
		return fmt.Errorf(stderror.ClientConfigIsEmpty)
	}
	if err = appctl.ValidateFullClientConfig(config); err != nil {
		return fmt.Errorf(stderror.ValidateFullClientConfigFailedErr, err)
	}

	// Set logging level based on client config.
	loggingLevel := config.GetLoggingLevel().String()
	if loggingLevel != appctlpb.LoggingLevel_DEFAULT.String() {
		log.SetLevel(loggingLevel)
	}

	// Disable server side metrics.
	if serverDecryptionMetricGroup := metrics.GetMetricGroupByName(cipher.ServerDecryptionMetricGroupName); serverDecryptionMetricGroup != nil {
		serverDecryptionMetricGroup.DisableLogging()
	}

	var wg sync.WaitGroup

	// RPC port is allowed to set to 0. In that case, don't run RPC server.
	// When RPC server is not running, mieru commands can't be used to control the proxy client.
	// This mode is typically used by a mobile app, where the app controls the lifecycle of the proxy client.
	if config.GetRpcPort() != 0 {
		wg.Add(1)
		go func() {
			rpcAddr := "localhost:" + strconv.Itoa(int(config.GetRpcPort()))
			rpcListener, err := net.Listen("tcp", rpcAddr)
			if err != nil {
				log.Fatalf("listen on RPC address tcp %q failed: %v", rpcAddr, err)
			}
			grpcServer := grpc.NewServer()
			appctl.SetClientRPCServerRef(grpcServer)
			appctlpb.RegisterClientLifecycleServiceServer(grpcServer, appctl.NewClientLifecycleService())
			close(appctl.ClientRPCServerStarted)
			log.Infof("mieru client RPC server is running")
			if err = grpcServer.Serve(rpcListener); err != nil {
				log.Fatalf("run gRPC server failed: %v", err)
			}
			log.Infof("mieru client RPC server is stopped")
			wg.Done()
		}()
		<-appctl.ClientRPCServerStarted
	}

	// Collect remote proxy addresses and password.
	proxyConfigs := make([]socks5.ProxyConfig, 0)
	var hashedPassword []byte
	activeProfile, err := appctl.GetActiveProfileFromConfig(config, config.GetActiveProfile())
	if err != nil {
		return fmt.Errorf(stderror.ClientGetActiveProfileFailedErr, err)
	}
	user := activeProfile.GetUser()
	if user.GetHashedPassword() != "" {
		hashedPassword, err = hex.DecodeString(user.GetHashedPassword())
		if err != nil {
			return fmt.Errorf(stderror.DecodeHashedPasswordFailedErr, err)
		}
	} else {
		hashedPassword = cipher.HashPassword([]byte(user.GetPassword()), []byte(user.GetName()))
	}
	for _, serverInfo := range activeProfile.GetServers() {
		var proxyHost string
		if serverInfo.GetDomainName() != "" {
			proxyHost = serverInfo.GetDomainName()
		} else {
			proxyHost = serverInfo.GetIpAddress()
		}
		for _, bindingInfo := range serverInfo.GetPortBindings() {
			proxyPort := bindingInfo.GetPort()
			if bindingInfo.GetProtocol() == appctlpb.TransportProtocol_TCP {
				proxyConfigs = append(proxyConfigs, socks5.ProxyConfig{
					NetworkType: "tcp",
					Address:     util.MaybeDecorateIPv6(proxyHost) + ":" + strconv.Itoa(int(proxyPort)),
					Password:    hashedPassword,
					Dial:        tcpsession.DialWithOptionsReturnConn,
				})
			} else if bindingInfo.GetProtocol() == appctlpb.TransportProtocol_UDP {
				proxyConfigs = append(proxyConfigs, socks5.ProxyConfig{
					NetworkType: "udp",
					Address:     util.MaybeDecorateIPv6(proxyHost) + ":" + strconv.Itoa(int(proxyPort)),
					Password:    hashedPassword,
					Dial:        udpsession.DialWithOptionsReturnConn,
				})
			}
		}
	}

	// Set MTU for UDP sessions.
	if activeProfile.GetMtu() != 0 {
		udpsession.SetGlobalMTU(activeProfile.GetMtu())
	}

	// Create the local socks5 server.
	socks5Config := &socks5.Config{
		UseProxy:                 true,
		ClientSideAuthentication: true,
		ProxyConf:                proxyConfigs,
	}
	socks5Server, err := socks5.New(socks5Config)
	if err != nil {
		return fmt.Errorf(stderror.CreateSocks5ServerFailedErr, err)
	}
	appctl.SetClientSocks5ServerRef(socks5Server)

	// Run the local socks5 server in the background.
	var socks5Addr string
	if config.GetSocks5ListenLAN() {
		socks5Addr = util.MaybeDecorateIPv6(util.AllIPAddr()) + ":" + strconv.Itoa(int(config.GetSocks5Port()))
	} else {
		socks5Addr = util.MaybeDecorateIPv6(util.LocalIPAddr()) + ":" + strconv.Itoa(int(config.GetSocks5Port()))
	}
	wg.Add(1)
	go func(socks5Addr string) {
		l, err := net.Listen("tcp", socks5Addr)
		if err != nil {
			log.Fatalf("listen on socks5 address tcp %q failed: %v", socks5Addr, err)
		}
		close(appctl.ClientSocks5ServerStarted)
		log.Infof("mieru client socks5 server is running")
		if err = socks5Server.Serve(l); err != nil {
			log.Fatalf("run socks5 server failed: %v", err)
		}
		log.Infof("mieru client socks5 server is stopped")
		wg.Done()
	}(socks5Addr)

	// If HTTP proxy is enabled, run the local HTTP server in the background.
	if config.GetHttpProxyPort() != 0 {
		wg.Add(1)
		go func(socks5Addr string) {
			var httpServerAddr string
			if config.GetHttpProxyListenLAN() {
				httpServerAddr = util.MaybeDecorateIPv6(util.AllIPAddr()) + ":" + strconv.Itoa(int(config.GetHttpProxyPort()))
			} else {
				httpServerAddr = util.MaybeDecorateIPv6(util.LocalIPAddr()) + ":" + strconv.Itoa(int(config.GetHttpProxyPort()))
			}
			httpServer := http2socks.NewHTTPServer(httpServerAddr, &http2socks.Proxy{
				ProxyURI: "socks5://" + socks5Addr + "?timeout=10s",
			})
			log.Infof("mieru client HTTP proxy server is running")
			wg.Done()
			if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Fatalf("run HTTP proxy server failed: %v", err)
			}
		}(socks5Addr)
	}

	<-appctl.ClientSocks5ServerStarted
	metrics.EnableLogging()

	appctl.SetAppStatus(appctlpb.AppStatus_RUNNING)
	wg.Wait()

	// Stop CPU profiling, if previously started.
	pprof.StopCPUProfile()

	log.Infof("mieru client exit now")
	return nil
}

var clientStopFunc = func(s []string) error {
	if err := appctl.IsClientDaemonRunning(context.Background()); err != nil {
		log.Infof(stderror.ClientNotRunning)
		return nil
	}

	timedctx, cancelFunc := context.WithTimeout(context.Background(), appctl.RPCTimeout)
	defer cancelFunc()
	client, err := appctl.NewClientLifecycleRPCClient(timedctx)
	if err != nil {
		return fmt.Errorf(stderror.CreateClientLifecycleRPCClientFailedErr, err)
	}
	if _, err = client.Exit(timedctx, &appctlpb.Empty{}); err != nil {
		return fmt.Errorf(stderror.ExitFailedErr, err)
	}
	log.Infof("mieru client is stopped")
	return nil
}

var clientStatusFunc = func(s []string) error {
	if err := appctl.IsClientDaemonRunning(context.Background()); err != nil {
		if stderror.IsConnRefused(err) {
			// This is the most common reason, no need to show more details.
			return fmt.Errorf(stderror.ClientNotRunning)
		} else if errors.Is(err, stderror.ErrFileNotExist) {
			// Ask the user to create a client config.
			return fmt.Errorf(stderror.ClientConfigNotExist + ", please create one with \"mieru apply config <FILE>\" command")
		} else {
			return fmt.Errorf(stderror.ClientNotRunningErr, err)
		}
	}
	log.Infof("mieru client is running")
	return nil
}

var clientApplyConfigFunc = func(s []string) error {
	_, err := appctl.LoadClientConfig()
	if err == stderror.ErrFileNotExist {
		if err = appctl.StoreClientConfig(&appctlpb.ClientConfig{}); err != nil {
			return fmt.Errorf(stderror.StoreClientConfigFailedErr, err)
		}
	}
	return appctl.ApplyJSONClientConfig(s[3])
}

var clientDescribeConfigFunc = func(s []string) error {
	_, err := appctl.LoadClientConfig()
	if err == stderror.ErrFileNotExist {
		if err = appctl.StoreClientConfig(&appctlpb.ClientConfig{}); err != nil {
			return fmt.Errorf(stderror.StoreClientConfigFailedErr, err)
		}
	}
	out, err := appctl.GetJSONClientConfig()
	if err != nil {
		return fmt.Errorf(stderror.GetClientConfigFailedErr, err)
	}
	log.Infof("%s", out)
	return nil
}

var clientImportConfigFunc = func(s []string) error {
	_, err := appctl.LoadClientConfig()
	if err == stderror.ErrFileNotExist {
		if err = appctl.StoreClientConfig(&appctlpb.ClientConfig{}); err != nil {
			return fmt.Errorf(stderror.StoreClientConfigFailedErr, err)
		}
	}
	return appctl.ApplyURLClientConfig(s[3])
}

var clientExportConfigFunc = func(s []string) error {
	_, err := appctl.LoadClientConfig()
	if err == stderror.ErrFileNotExist {
		if err = appctl.StoreClientConfig(&appctlpb.ClientConfig{}); err != nil {
			return fmt.Errorf(stderror.StoreClientConfigFailedErr, err)
		}
	}
	out, err := appctl.GetURLClientConfig()
	if err != nil {
		return fmt.Errorf(stderror.GetClientConfigFailedErr, err)
	}
	log.Infof("%s", out)
	return nil
}

var clientDeleteProfileFunc = func(s []string) error {
	_, err := appctl.LoadClientConfig()
	if err != nil {
		return fmt.Errorf(stderror.LoadClientConfigFailedErr, err)
	}
	return appctl.DeleteClientConfigProfile(s[3])
}

var clientGetThreadDumpFunc = func(s []string) error {
	if err := appctl.IsClientDaemonRunning(context.Background()); err != nil {
		log.Infof(stderror.ClientNotRunning)
		return nil
	}

	timedctx, cancelFunc := context.WithTimeout(context.Background(), appctl.RPCTimeout)
	defer cancelFunc()
	client, err := appctl.NewClientLifecycleRPCClient(timedctx)
	if err != nil {
		return fmt.Errorf(stderror.CreateClientLifecycleRPCClientFailedErr, err)
	}
	dump, err := client.GetThreadDump(timedctx, &appctlpb.Empty{})
	if err != nil {
		return fmt.Errorf(stderror.GetThreadDumpFailedErr, err)
	}
	log.Infof("%s", dump.GetThreadDump())
	return nil
}

var clientGetHeapProfileFunc = func(s []string) error {
	if err := appctl.IsClientDaemonRunning(context.Background()); err != nil {
		log.Infof(stderror.ClientNotRunning)
		return nil
	}

	timedctx, cancelFunc := context.WithTimeout(context.Background(), appctl.RPCTimeout)
	defer cancelFunc()
	client, err := appctl.NewClientLifecycleRPCClient(timedctx)
	if err != nil {
		return fmt.Errorf(stderror.CreateClientLifecycleRPCClientFailedErr, err)
	}
	if _, err := client.GetHeapProfile(timedctx, &appctlpb.ProfileSavePath{FilePath: proto.String(s[3])}); err != nil {
		return fmt.Errorf(stderror.GetHeapProfileFailedErr, err)
	}
	log.Infof("heap profile is saved to %q", s[3])
	return nil
}

var clientStartCPUProfileFunc = func(s []string) error {
	if err := appctl.IsClientDaemonRunning(context.Background()); err != nil {
		log.Infof(stderror.ClientNotRunning)
		return nil
	}

	timedctx, cancelFunc := context.WithTimeout(context.Background(), appctl.RPCTimeout)
	defer cancelFunc()
	client, err := appctl.NewClientLifecycleRPCClient(timedctx)
	if err != nil {
		return fmt.Errorf(stderror.CreateClientLifecycleRPCClientFailedErr, err)
	}
	if _, err := client.StartCPUProfile(timedctx, &appctlpb.ProfileSavePath{FilePath: proto.String(s[4])}); err != nil {
		return fmt.Errorf(stderror.StartCPUProfileFailedErr, err)
	}
	log.Infof("CPU profile will be saved to %q", s[4])
	return nil
}

var clientStopCPUProfileFunc = func(s []string) error {
	if err := appctl.IsClientDaemonRunning(context.Background()); err != nil {
		log.Infof(stderror.ClientNotRunning)
		return nil
	}

	timedctx, cancelFunc := context.WithTimeout(context.Background(), appctl.RPCTimeout)
	defer cancelFunc()
	client, err := appctl.NewClientLifecycleRPCClient(timedctx)
	if err != nil {
		return fmt.Errorf(stderror.CreateClientLifecycleRPCClientFailedErr, err)
	}
	client.StopCPUProfile(timedctx, &appctlpb.Empty{})
	return nil
}
