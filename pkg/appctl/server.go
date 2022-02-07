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
	"io/ioutil"
	"os"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"

	pb "github.com/enfein/mieru/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/metrics"
	"github.com/enfein/mieru/pkg/netutil"
	"github.com/enfein/mieru/pkg/session"
	"github.com/enfein/mieru/pkg/socks5"
	"github.com/enfein/mieru/pkg/stderror"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// ServerUDS is the UNIX domain socket that server is listening to RPC requests.
const ServerUDS = "/var/run/mita.sock"

var (
	// ServerRPCServerStarted is closed when server RPC server is started.
	ServerRPCServerStarted chan struct{} = make(chan struct{})

	// ServerSocks5ServerStarted is closed when server socks5 server is started.
	ServerSocks5ServerStarted chan struct{} = make(chan struct{})

	// serverIOLock is required to load server config and store server config.
	serverIOLock sync.Mutex

	cachedServerConfigDir      string = "/etc/mita"
	cachedServerConfigFilePath string = "/etc/mita/server.conf.pb"

	// serverRPCServerRef holds a pointer to server RPC server.
	serverRPCServerRef atomic.Value

	// serverSocks5ServerRef holds a pointer to server socks5 server.
	serverSocks5ServerRef atomic.Value

	// nilServerSocks5Server is a place holder to clear serverSocks5ServerRef (because atomic.Value can't be set to nil).
	nilServerSocks5Server = &socks5.Server{}
)

func GetServerRPCServerRef() *grpc.Server {
	s, ok := serverRPCServerRef.Load().(*grpc.Server)
	if !ok {
		return nil
	}
	return s
}

func SetServerRPCServerRef(server *grpc.Server) {
	serverRPCServerRef.Store(server)
}

func GetServerSocks5ServerRef() *socks5.Server {
	s, ok := serverSocks5ServerRef.Load().(*socks5.Server)
	if !ok || s == nilServerSocks5Server {
		return nil
	}
	return s
}

func SetServerSocks5ServerRef(server *socks5.Server) {
	serverSocks5ServerRef.Store(server)
}

// serverLifecycleService implements ServerLifecycleService defined in lifecycle.proto.
type serverLifecycleService struct {
	pb.UnimplementedServerLifecycleServiceServer
}

func (s *serverLifecycleService) GetStatus(ctx context.Context, req *pb.Empty) (*pb.AppStatusMsg, error) {
	status := GetAppStatus()
	log.Infof("return app status %s back to RPC caller", status.String())
	return &pb.AppStatusMsg{Status: status}, nil
}

func (s *serverLifecycleService) Start(ctx context.Context, req *pb.Empty) (*pb.Empty, error) {
	log.Infof("received start request from RPC caller")
	config, err := LoadServerConfig()
	if err != nil {
		return &pb.Empty{}, fmt.Errorf("LoadServerConfig() failed: %w", err)
	}
	loggingLevel := config.GetLoggingLevel().String()
	if loggingLevel != pb.LoggingLevel_DEFAULT.String() {
		log.SetLevel(loggingLevel)
	}
	if err = ValidateFullServerConfig(config); err != nil {
		return &pb.Empty{}, fmt.Errorf("ValidateFullServerConfig() failed: %w", err)
	}
	if GetServerSocks5ServerRef() != nil {
		log.Infof("socks5 server is already started")
		return &pb.Empty{}, nil
	}

	// Create the egress socks5 server.
	socks5Config := &socks5.Config{
		AllowLocalDestination: config.GetAdvancedSettings().GetAllowLocalDestination(),
	}
	socks5Server, err := socks5.New(socks5Config)
	if err != nil {
		return &pb.Empty{}, fmt.Errorf("failed to create socks5 server: %w", err)
	}
	SetServerSocks5ServerRef(socks5Server)
	ServerSocks5ServerStarted = make(chan struct{})
	SetAppStatus(pb.AppStatus_STARTING)

	// Run the egress socks5 server in the background.
	go func() {
		socks5Addr := netutil.MaybeDecorateIPv6(netutil.AllIPAddr()) + ":" + strconv.Itoa(int(config.GetPortBindings()[0].GetPort()))
		l, err := session.ListenWithOptions(socks5Addr, UserListToMap(config.GetUsers()))
		if err != nil {
			log.Fatalf("net.Listen(%q, %q) failed: %v", "tcp", socks5Addr, err)
		}
		close(ServerSocks5ServerStarted)
		log.Infof("mieru server daemon socks5 server is running")
		if err = socks5Server.Serve(l); err != nil {
			log.Fatalf("run socks5 server failed: %v", err)
		}
		log.Infof("mieru server daemon socks5 server is stopped")
	}()
	<-ServerSocks5ServerStarted
	metrics.EnableLogging()

	SetAppStatus(pb.AppStatus_RUNNING)
	log.Infof("completed start request from RPC caller")
	return &pb.Empty{}, nil
}

func (s *serverLifecycleService) Stop(ctx context.Context, req *pb.Empty) (*pb.Empty, error) {
	SetAppStatus(pb.AppStatus_STOPPING)
	log.Infof("received stop request from RPC caller")
	socks5Server := GetServerSocks5ServerRef()
	if socks5Server != nil {
		log.Infof("stopping socks5 server")
		if err := socks5Server.Close(); err != nil {
			if log.IsLevelEnabled(log.DebugLevel) {
				log.Debugf("socks5 server Close() failed: %v", err)
			}
		}
	} else {
		log.Infof("socks5 server reference not found")
	}
	SetServerSocks5ServerRef(nilServerSocks5Server)
	SetAppStatus(pb.AppStatus_IDLE)
	log.Infof("completed stop request from RPC caller")
	return &pb.Empty{}, nil
}

func (s *serverLifecycleService) Exit(ctx context.Context, req *pb.Empty) (*pb.Empty, error) {
	SetAppStatus(pb.AppStatus_STOPPING)
	log.Infof("received exit request from RPC caller")
	socks5Server := GetServerSocks5ServerRef()
	if socks5Server != nil {
		log.Infof("stopping socks5 server")
		if err := socks5Server.Close(); err != nil {
			if log.IsLevelEnabled(log.DebugLevel) {
				log.Debugf("socks5 server Close() failed: %v", err)
			}
		}
	} else {
		log.Infof("socks5 server reference not found")
	}
	SetServerSocks5ServerRef(nilServerSocks5Server)
	grpcServer := GetServerRPCServerRef()
	if grpcServer != nil {
		log.Infof("stopping RPC server")
		go grpcServer.GracefulStop()
	} else {
		log.Infof("RPC server reference not found")
	}
	log.Infof("completed exit request from RPC caller")
	return &pb.Empty{}, nil
}

func (s *serverLifecycleService) GetThreadDump(ctx context.Context, req *pb.Empty) (*pb.ThreadDump, error) {
	buf := make([]byte, 4096)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	return &pb.ThreadDump{ThreadDump: string(buf)}, nil
}

// NewServerLifecycleService creates a new ServerLifecycleService RPC server.
func NewServerLifecycleService() *serverLifecycleService {
	return &serverLifecycleService{}
}

// NewServerLifecycleRPCClient creates a new ServerLifecycleService RPC client.
func NewServerLifecycleRPCClient() (pb.ServerLifecycleServiceClient, error) {
	rpcAddr := "unix://" + ServerUDS
	timedctx, cancelFunc := context.WithTimeout(context.Background(), RPCTimeout())
	defer cancelFunc()
	conn, err := grpc.DialContext(timedctx, rpcAddr, grpc.WithInsecure())
	if err != nil {
		return nil, fmt.Errorf("grpc.DialContext() failed: %w", err)
	}
	return pb.NewServerLifecycleServiceClient(conn), nil
}

// serverConfigService implements ServerConfigService defined in servercfg.proto.
type serverConfigService struct {
	pb.UnimplementedServerConfigServiceServer
}

func (s *serverConfigService) GetConfig(ctx context.Context, req *pb.Empty) (*pb.ServerConfig, error) {
	config, err := LoadServerConfig()
	if err != nil {
		return &pb.ServerConfig{}, fmt.Errorf("LoadServerConfig() failed: %w", err)
	}
	return config, nil
}

func (s *serverConfigService) SetConfig(ctx context.Context, req *pb.ServerConfig) (*pb.ServerConfig, error) {
	if err := StoreServerConfig(req); err != nil {
		return &pb.ServerConfig{}, fmt.Errorf("StoreServerConfig() failed: %w", err)
	}
	config, err := LoadServerConfig()
	if err != nil {
		return &pb.ServerConfig{}, fmt.Errorf("LoadServerConfig() failed: %w", err)
	}
	return config, nil
}

// NewServerConfigService creates a new ServerConfigService RPC server.
func NewServerConfigService() *serverConfigService {
	return &serverConfigService{}
}

// NewServerConfigRPCClient creates a new ServerConfigService RPC client.
func NewServerConfigRPCClient() (pb.ServerConfigServiceClient, error) {
	rpcAddr := "unix://" + ServerUDS
	timedctx, cancelFunc := context.WithTimeout(context.Background(), RPCTimeout())
	defer cancelFunc()
	conn, err := grpc.DialContext(timedctx, rpcAddr, grpc.WithInsecure())
	if err != nil {
		return nil, fmt.Errorf("grpc.DialContext() failed: %w", err)
	}
	return pb.NewServerConfigServiceClient(conn), nil
}

// GetServerStatusWithRPC gets server application status via ServerLifecycleService.GetStatus() RPC.
func GetServerStatusWithRPC(ctx context.Context) (*pb.AppStatusMsg, error) {
	client, err := NewServerLifecycleRPCClient()
	if err != nil {
		return nil, fmt.Errorf("NewServerLifecycleRPCClient() failed: %w", err)
	}
	timedctx, cancelFunc := context.WithTimeout(ctx, RPCTimeout())
	defer cancelFunc()
	status, err := client.GetStatus(timedctx, &pb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("ServerLifecycleService.GetStatus() failed: %w", err)
	}
	return status, nil
}

// IsServerDaemonRunning returns nil if app status shows server daemon is running.
func IsServerDaemonRunning(appStatus *pb.AppStatusMsg) error {
	if appStatus == nil {
		return fmt.Errorf("AppStatusMsg is nil")
	}
	if appStatus.GetStatus() == pb.AppStatus_UNKNOWN {
		return fmt.Errorf("mieru server status is %q", appStatus.GetStatus().String())
	}
	return nil
}

// IsServerProxyRunning returns nil if app status shows proxy function is running.
func IsServerProxyRunning(appStatus *pb.AppStatusMsg) error {
	if err := IsServerDaemonRunning(appStatus); err != nil {
		return err
	}
	if appStatus.GetStatus() != pb.AppStatus_RUNNING {
		return fmt.Errorf("mieru server status is %q", appStatus.GetStatus().String())
	}
	return nil
}

// GetJSONServerConfig returns the server config as JSON.
func GetJSONServerConfig() (string, error) {
	config, err := LoadServerConfig()
	if err != nil {
		return "", fmt.Errorf("LoadServerConfig() failed: %w", err)
	}
	b, err := jsonMarshalOption.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("protojson.Marshal() failed: %w", err)
	}
	return string(b), nil
}

// LoadServerConfig reads server config from disk.
func LoadServerConfig() (*pb.ServerConfig, error) {
	serverIOLock.Lock()
	defer serverIOLock.Unlock()

	err := checkServerConfigDir()
	if err != nil {
		return nil, fmt.Errorf("checkServerConfigDir() failed: %w", err)
	}
	fileName, err := serverConfigFilePath()
	if err != nil {
		return nil, fmt.Errorf("serverConfigFilePath() failed: %w", err)
	}

	if log.IsLevelEnabled(log.DebugLevel) {
		log.Debugf("loading server config from %q", fileName)
	}
	f, err := os.Open(fileName)
	if err != nil && os.IsNotExist(err) {
		return nil, stderror.ErrFileNotExist
	} else if err != nil {
		return nil, fmt.Errorf("os.Open() failed: %w", err)
	}
	defer f.Close()

	b, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("ReadFile(%q) failed: %w", fileName, err)
	}

	s := &pb.ServerConfig{}
	err = proto.Unmarshal(b, s)
	if err != nil {
		return nil, fmt.Errorf("proto.Unmarshal() failed: %w", err)
	}

	return s, nil
}

// StoreServerConfig writes server config to disk.
func StoreServerConfig(config *pb.ServerConfig) error {
	serverIOLock.Lock()
	defer serverIOLock.Unlock()

	if config == nil {
		return fmt.Errorf("ServerConfig is nil")
	}
	config.Users = HashUserPasswords(config.GetUsers(), false)

	err := checkServerConfigDir()
	if err != nil {
		return fmt.Errorf("checkServerConfigDir() failed: %w", err)
	}
	fileName, err := serverConfigFilePath()
	if err != nil {
		return fmt.Errorf("serverConfigFilePath() failed: %w", err)
	}

	b, err := proto.Marshal(config)
	if err != nil {
		return fmt.Errorf("proto.Marshal() failed: %w", err)
	}

	err = ioutil.WriteFile(fileName, b, 0660)
	if err != nil {
		return fmt.Errorf("WriteFile(%q) failed: %w", fileName, err)
	}
	return nil
}

// ApplyJSONServerConfig applies user provided JSON server config from path.
func ApplyJSONServerConfig(path string) error {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return fmt.Errorf("ReadFile(%q) failed: %w", path, err)
	}
	s := &pb.ServerConfig{}
	if err = jsonUnmarshalOption.Unmarshal(b, s); err != nil {
		return fmt.Errorf("protojson.Unmarshal() failed: %w", err)
	}
	if err := ValidateServerConfigPatch(s); err != nil {
		return fmt.Errorf("ValidateServerConfigPatch() failed: %w", err)
	}
	config, err := LoadServerConfig()
	if err != nil {
		return fmt.Errorf("LoadServerConfig() failed: %w", err)
	}
	if err = mergeServerConfig(config, s); err != nil {
		return fmt.Errorf("mergeServerConfig() failed: %w", err)
	}
	if err = ValidateFullServerConfig(config); err != nil {
		return fmt.Errorf("ValidateFullServerConfig() failed: %w", err)
	}
	if err = StoreServerConfig(config); err != nil {
		return fmt.Errorf("StoreServerConfig() failed: %w", err)
	}
	return nil
}

// DeleteServerUsers deletes the list of users from server config.
func DeleteServerUsers(names []string) error {
	config, err := LoadServerConfig()
	if err != nil {
		return fmt.Errorf("LoadServerConfig() failed: %w", err)
	}
	users := config.GetUsers()
	remaining := make([]*pb.User, 0)
	// The complexity of the following algorithm is O(total_users * users_to_delete).
	// This seems to be high, however in reality the number of users to delete is typically 1,
	// so it is faster than using a set to find the difference, then sort the users by name.
	for _, user := range users {
		shouldDelete := false
		for _, toDelete := range names {
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
	if err := StoreServerConfig(config); err != nil {
		return fmt.Errorf("StoreServerConfig() failed: %w", err)
	}
	return nil
}

// ValidateServerConfigPatch validates a patch of server config.
//
// A server config patch must satisfy:
// 1. it has 0 or more port binding
// 2. for each port binding
// 2.1. port number is valid
// 2.2. protocol is valid
// 3. it has 0 or more user
// 4. for each user
// 4.1. user name is not empty
// 4.2. user has either a password or a hashed password
func ValidateServerConfigPatch(patch *pb.ServerConfig) error {
	for _, portBinding := range patch.GetPortBindings() {
		port := portBinding.GetPort()
		protocol := portBinding.GetProtocol()
		if port < 1 || port > 65535 {
			return fmt.Errorf("port number %d is invalid", port)
		}
		if protocol == pb.TransportProtocol_UNKNOWN_TRANSPORT_PROTOCOL {
			return fmt.Errorf("protocol is not set")
		}
	}
	for _, user := range patch.GetUsers() {
		if user.GetName() == "" {
			return fmt.Errorf("user name is not set")
		}
		if user.GetPassword() == "" && user.GetHashedPassword() == "" {
			return fmt.Errorf("user password is not set")
		}
	}
	return nil
}

// ValidateFullServerConfig validates the full server config.
//
// In addition to ValidateServerConfigPatch, it also validates:
// 1. there is exactly 1 port binding
func ValidateFullServerConfig(config *pb.ServerConfig) error {
	if err := ValidateServerConfigPatch(config); err != nil {
		return err
	}
	if proto.Equal(config, &pb.ServerConfig{}) {
		return fmt.Errorf("server config is empty")
	}
	if len(config.GetPortBindings()) == 0 {
		return fmt.Errorf("server port binding is not set")
	}
	if len(config.GetPortBindings()) > 1 {
		return fmt.Errorf("want exactly 1 port binding, got %d", len(config.GetPortBindings()))
	}
	return nil
}

// checkServerConfigDir validates if server config directory exists.
func checkServerConfigDir() error {
	_, err := os.Stat(cachedServerConfigDir)
	return err
}

// serverConfigFilePath returns the server config file path.
func serverConfigFilePath() (string, error) {
	return cachedServerConfigFilePath, nil
}

// mergeServerConfig merges the source client config into destination.
// If a user is specified in source, it is added to destination, or replacing existing user in destination.
func mergeServerConfig(dst, src *pb.ServerConfig) error {
	// Port bindings: if src is not empty, replace dst with src.
	var mergedPortBindings []*pb.PortBinding
	if len(src.GetPortBindings()) != 0 {
		mergedPortBindings = src.GetPortBindings()
	} else {
		mergedPortBindings = dst.GetPortBindings()
	}

	// Users: merge src into dst.
	mergedUserMapping := map[string]*pb.User{}
	for _, user := range dst.GetUsers() {
		mergedUserMapping[user.GetName()] = user
	}
	for _, user := range src.GetUsers() {
		mergedUserMapping[user.GetName()] = user
	}
	names := make([]string, 0, len(mergedUserMapping))
	for name := range mergedUserMapping {
		names = append(names, name)
	}
	sort.Strings(names)
	mergedUsers := make([]*pb.User, 0, len(mergedUserMapping))
	for _, name := range names {
		mergedUsers = append(mergedUsers, mergedUserMapping[name])
	}

	var advancedSettings *pb.ServerAdvancedSettings
	if src.GetAdvancedSettings() != nil {
		advancedSettings = src.GetAdvancedSettings()
	} else {
		advancedSettings = dst.GetAdvancedSettings()
	}
	var loggingLevel pb.LoggingLevel
	if src.GetLoggingLevel() != pb.LoggingLevel_DEFAULT {
		loggingLevel = src.GetLoggingLevel()
	} else {
		loggingLevel = dst.GetLoggingLevel()
	}

	proto.Reset(dst)
	dst.PortBindings = mergedPortBindings
	dst.Users = mergedUsers
	dst.AdvancedSettings = advancedSettings
	dst.LoggingLevel = loggingLevel
	return nil
}

// deleteServerConfigFile deletes the server config file.
func deleteServerConfigFile() error {
	path, err := serverConfigFilePath()
	if err != nil {
		return fmt.Errorf("serverConfigFilePath() failed: %w", err)
	}
	err = os.Remove(path)
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return err
}
