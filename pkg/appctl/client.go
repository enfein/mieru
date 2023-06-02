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
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"

	pb "github.com/enfein/mieru/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/socks5"
	"github.com/enfein/mieru/pkg/stderror"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

var (
	// ClientRPCServerStarted is closed when client RPC server is started.
	ClientRPCServerStarted chan struct{} = make(chan struct{})

	// ClientSocks5ServerStarted is closed when client socks5 server is started.
	ClientSocks5ServerStarted chan struct{} = make(chan struct{})

	// clientIOLock is required to load client config and store client config.
	clientIOLock sync.Mutex

	// cachedClientConfigDir is the absolute path of client config directory.
	//
	// In Linux, it is typically /home/<user>/.config/mieru
	//
	// In Mac OS, it is typically /Users/<user>/Library/Application Support/mieru
	//
	// In Windows, it is typically C:\Users\<user>\AppData\Roaming\mieru
	cachedClientConfigDir string

	// cachedClientConfigFilePath is the absolute path of client config file.
	cachedClientConfigFilePath string

	// clientRPCServerRef holds a pointer to client RPC server.
	clientRPCServerRef atomic.Pointer[grpc.Server]

	// clientSocks5ServerRef holds a pointer to client socks5 server.
	clientSocks5ServerRef atomic.Pointer[socks5.Server]
)

func SetClientRPCServerRef(server *grpc.Server) {
	clientRPCServerRef.Store(server)
}

func SetClientSocks5ServerRef(server *socks5.Server) {
	clientSocks5ServerRef.Store(server)
}

// clientLifecycleService implements ClientLifecycleService defined in lifecycle.proto.
type clientLifecycleService struct {
	pb.UnimplementedClientLifecycleServiceServer
}

func (c *clientLifecycleService) GetStatus(ctx context.Context, req *pb.Empty) (*pb.AppStatusMsg, error) {
	status := GetAppStatus()
	log.Infof("return app status %s back to RPC caller", status.String())
	return &pb.AppStatusMsg{Status: &status}, nil
}

func (c *clientLifecycleService) Exit(ctx context.Context, req *pb.Empty) (*pb.Empty, error) {
	SetAppStatus(pb.AppStatus_STOPPING)
	log.Infof("received exit request from RPC caller")
	socks5Server := clientSocks5ServerRef.Load()
	if socks5Server != nil {
		log.Infof("stopping socks5 server")
		if err := socks5Server.Close(); err != nil {
			log.Infof("socks5 server Close() failed: %v", err)
		}
	} else {
		log.Infof("socks5 server reference not found")
	}
	grpcServer := clientRPCServerRef.Load()
	if grpcServer != nil {
		log.Infof("stopping RPC server")
		go grpcServer.GracefulStop()
	} else {
		log.Infof("RPC server reference not found")
	}
	log.Infof("completed exit request from RPC caller")
	return &pb.Empty{}, nil
}

func (c *clientLifecycleService) GetThreadDump(ctx context.Context, req *pb.Empty) (*pb.ThreadDump, error) {
	return &pb.ThreadDump{ThreadDump: proto.String(string(getThreadDump()))}, nil
}

func (c *clientLifecycleService) StartCPUProfile(ctx context.Context, req *pb.ProfileSavePath) (*pb.Empty, error) {
	err := startCPUProfile(req.GetFilePath())
	return &pb.Empty{}, err
}

func (c *clientLifecycleService) StopCPUProfile(ctx context.Context, req *pb.Empty) (*pb.Empty, error) {
	stopCPUProfile()
	return &pb.Empty{}, nil
}

func (c *clientLifecycleService) GetHeapProfile(ctx context.Context, req *pb.ProfileSavePath) (*pb.Empty, error) {
	err := getHeapProfile(req.GetFilePath())
	return &pb.Empty{}, err
}

// NewClientLifecycleService creates a new ClientLifecycleService RPC server.
func NewClientLifecycleService() *clientLifecycleService {
	return &clientLifecycleService{}
}

// NewClientLifecycleRPCClient creates a new ClientLifecycleService RPC client.
// It loads client config to find the server address.
func NewClientLifecycleRPCClient(ctx context.Context) (pb.ClientLifecycleServiceClient, error) {
	config, err := LoadClientConfig()
	if err != nil {
		return nil, fmt.Errorf("LoadClientConfig() failed: %w", err)
	}
	if proto.Equal(config, &pb.ClientConfig{}) {
		return nil, fmt.Errorf(stderror.ClientConfigIsEmpty)
	}
	if config.GetRpcPort() < 1 || config.GetRpcPort() > 65535 {
		return nil, fmt.Errorf("RPC port number %d is invalid", config.GetRpcPort())
	}
	rpcAddr := "localhost:" + strconv.Itoa(int(config.GetRpcPort()))
	return newClientLifecycleRPCClient(ctx, rpcAddr)
}

// IsClientDaemonRunning detects if client daemon is running by using ClientLifecycleService.GetStatus() RPC.
func IsClientDaemonRunning(ctx context.Context) error {
	timedctx, cancelFunc := context.WithTimeout(ctx, RPCTimeout)
	defer cancelFunc()
	client, err := NewClientLifecycleRPCClient(timedctx)
	if err != nil {
		return fmt.Errorf("NewClientLifecycleRPCClient() failed: %w", err)
	}
	if _, err = client.GetStatus(timedctx, &pb.Empty{}); err != nil {
		return fmt.Errorf("ClientLifecycleService.GetStatus() failed: %w", err)
	}
	return nil
}

// GetJSONClientConfig returns the client config as JSON.
func GetJSONClientConfig() (string, error) {
	config, err := LoadClientConfig()
	if err != nil {
		return "", fmt.Errorf("LoadClientConfig() failed: %w", err)
	}
	b, err := jsonMarshalOption.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("protojson.Marshal() failed: %w", err)
	}
	return string(b), nil
}

// GetURLClientConfig returns the client config as a URL.
func GetURLClientConfig() (string, error) {
	config, err := LoadClientConfig()
	if err != nil {
		return "", fmt.Errorf("LoadClientConfig() failed: %w", err)
	}
	u, err := ClientConfigToURL(config)
	if err != nil {
		return "", fmt.Errorf("ClientConfigToURL() failed: %w", err)
	}
	return u, nil
}

// LoadClientConfig reads client config from disk.
func LoadClientConfig() (*pb.ClientConfig, error) {
	clientIOLock.Lock()
	defer clientIOLock.Unlock()

	fileName, fileType, err := clientConfigFilePath()
	if err != nil {
		return nil, fmt.Errorf("clientConfigFilePath() failed: %w", err)
	}
	if err := prepareClientConfigDir(); err != nil {
		return nil, fmt.Errorf("prepareClientConfigDir() failed: %w", err)
	}

	log.Debugf("loading client config from %q", fileName)
	f, err := os.Open(fileName)
	if err != nil && os.IsNotExist(err) {
		return nil, stderror.ErrFileNotExist
	} else if err != nil {
		return nil, fmt.Errorf("os.Open() failed: %w", err)
	}
	defer f.Close()

	b, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("io.ReadAll() failed: %w", err)
	}

	c := &pb.ClientConfig{}
	switch fileType {
	case PROTOBUF_CONFIG_FILE_TYPE:
		if err := proto.Unmarshal(b, c); err != nil {
			return nil, fmt.Errorf("proto.Unmarshal() failed: %w", err)
		}
	case JSON_CONFIG_FILE_TYPE:
		if err := Unmarshal(b, c); err != nil {
			return nil, fmt.Errorf("Unmarshal() failed: %w", err)
		}
	default:
		return nil, fmt.Errorf("config file type is invalid")
	}

	return c, nil
}

// StoreClientConfig writes client config to disk.
func StoreClientConfig(config *pb.ClientConfig) error {
	clientIOLock.Lock()
	defer clientIOLock.Unlock()

	fileName, fileType, err := clientConfigFilePath()
	if err != nil {
		return fmt.Errorf("clientConfigFilePath() failed: %w", err)
	}
	if err := prepareClientConfigDir(); err != nil {
		return fmt.Errorf("prepareClientConfigDir() failed: %w", err)
	}

	for _, profile := range config.GetProfiles() {
		profile.User = HashUserPassword(profile.GetUser(), true)
	}

	var b []byte
	switch fileType {
	case PROTOBUF_CONFIG_FILE_TYPE:
		if b, err = proto.Marshal(config); err != nil {
			return fmt.Errorf("proto.Marshal() failed: %w", err)
		}
	case JSON_CONFIG_FILE_TYPE:
		if b, err = Marshal(config); err != nil {
			return fmt.Errorf("Marshal() failed: %w", err)
		}
	default:
		return fmt.Errorf("config file type is invalid")
	}

	err = os.WriteFile(fileName, b, 0660)
	if err != nil {
		return fmt.Errorf("os.WriteFile(%q) failed: %w", fileName, err)
	}

	return nil
}

// ApplyJSONClientConfig applies user provided JSON client config from the given file.
func ApplyJSONClientConfig(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("os.ReadFile(%q) failed: %w", path, err)
	}
	c := &pb.ClientConfig{}
	if err = jsonUnmarshalOption.Unmarshal(b, c); err != nil {
		return fmt.Errorf("protojson.Unmarshal() failed: %w", err)
	}
	return applyClientConfig(c)
}

// ApplyJSONClientConfig applies user provided client config URL.
func ApplyURLClientConfig(u string) error {
	c, err := URLToClientConfig(u)
	if err != nil {
		return fmt.Errorf("URLToClientConfig() failed: %w", err)
	}
	return applyClientConfig(c)
}

// DeleteClientConfigProfile deletes a profile stored in client config.
// The profile to delete can't be the active profile.
func DeleteClientConfigProfile(profileName string) error {
	config, err := LoadClientConfig()
	if err != nil {
		return fmt.Errorf("LoadClientConfig() failed: %w", err)
	}
	if config.GetActiveProfile() == profileName {
		return fmt.Errorf("activeProfile %q can't be deleted", profileName)
	}
	profiles := config.GetProfiles()
	updated := make([]*pb.ClientProfile, 0)
	for _, profile := range profiles {
		if profile.GetProfileName() == profileName {
			continue
		}
		updated = append(updated, profile)
	}
	config.Profiles = updated
	if err = StoreClientConfig(config); err != nil {
		return fmt.Errorf("StoreClientConfig() failed: %w", err)
	}
	return nil
}

// ValidateClientConfigPatch validates a patch of client config.
//
// A client config patch must satisfy:
// 1. it has 0 or more profile
// 2. for each profile
// 2.1. profile name is not empty
// 2.2. user name is not empty
// 2.3. user has either a password or a hashed password
// 2.4. it has at least 1 server, and for each server
// 2.4.1. the server has either IP address or domain name
// 2.4.2. if set, server's IP address is parsable
// 2.4.3. the server has at least 1 port binding, and for each port binding
// 2.4.3.1. port number is valid
// 2.4.3.2. protocol is valid
// 2.5. if set, MTU is valid
func ValidateClientConfigPatch(patch *pb.ClientConfig) error {
	for _, profile := range patch.GetProfiles() {
		name := profile.GetProfileName()
		if name == "" {
			return fmt.Errorf("profile name is not set")
		}
		user := profile.GetUser()
		if user.GetName() == "" {
			return fmt.Errorf("user name is not set")
		}
		if user.GetPassword() == "" && user.GetHashedPassword() == "" {
			return fmt.Errorf("user password is not set")
		}
		servers := profile.GetServers()
		if len(servers) == 0 {
			return fmt.Errorf("servers are not set")
		}
		for _, server := range servers {
			if server.GetIpAddress() == "" && server.GetDomainName() == "" {
				return fmt.Errorf("neither server IP address nor domain name is set")
			}
			if server.GetIpAddress() != "" && net.ParseIP(server.GetIpAddress()) == nil {
				return fmt.Errorf("failed to parse IP address %q", server.GetIpAddress())
			}
			portBindings := server.GetPortBindings()
			if len(portBindings) == 0 {
				return fmt.Errorf("server port binding is not set")
			}
			for _, binding := range portBindings {
				if binding.GetPort() < 1 || binding.GetPort() > 65535 {
					return fmt.Errorf("server port number %d is invalid", binding.GetPort())
				}
				if binding.GetProtocol() == pb.TransportProtocol_UNKNOWN_TRANSPORT_PROTOCOL {
					return fmt.Errorf("server protocol is not set")
				}
			}
		}
		if profile.GetMtu() != 0 && (profile.GetMtu() < 1280 || profile.GetMtu() > 1500) {
			return fmt.Errorf("MTU value %d is out of range, valid range is [1280, 1500]", profile.GetMtu())
		}
	}
	return nil
}

// ValidateFullClientConfig validates the full client config.
//
// In addition to ValidateClientConfigPatch, it also validates:
// 1. there is at least 1 profile
// 2. the active profile is available
// 3. RPC port is valid
// 4. socks5 port is valid
// 5. RPC port, socks5 port, http proxy port are different
func ValidateFullClientConfig(config *pb.ClientConfig) error {
	if err := ValidateClientConfigPatch(config); err != nil {
		return err
	}
	if len(config.GetProfiles()) == 0 {
		return fmt.Errorf("profiles are not set")
	}
	if config.GetActiveProfile() == "" {
		return fmt.Errorf("active profile is not set")
	}
	foundActiveProfile := false
	for _, profile := range config.GetProfiles() {
		if profile.GetProfileName() == config.GetActiveProfile() {
			foundActiveProfile = true
			break
		}
	}
	if !foundActiveProfile {
		return fmt.Errorf("active profile is not found in the profile list")
	}
	// RPC port is allowed to be 0, which means disable RPC.
	if config.GetRpcPort() < 0 || config.GetRpcPort() > 65535 {
		return fmt.Errorf("RPC port number %d is invalid", config.GetRpcPort())
	}
	if config.GetSocks5Port() < 1 || config.GetSocks5Port() > 65535 {
		return fmt.Errorf("socks5 port number %d is invalid", config.GetSocks5Port())
	}
	if config.GetRpcPort() == config.GetSocks5Port() {
		return fmt.Errorf("RPC port number %d is the same as socks5 port number", config.GetRpcPort())
	}
	if config.HttpProxyPort != nil {
		if config.GetHttpProxyPort() < 1 || config.GetHttpProxyPort() > 65535 {
			return fmt.Errorf("HTTP proxy port number %d is invalid", config.GetHttpProxyPort())
		}
		if config.GetHttpProxyPort() == config.GetRpcPort() {
			return fmt.Errorf("HTTP proxy port number %d is the same as RPC port number", config.GetHttpProxyPort())
		}
		if config.GetHttpProxyPort() == config.GetSocks5Port() {
			return fmt.Errorf("HTTP proxy port number %d is the same as socks5 port number", config.GetHttpProxyPort())
		}
	}
	return nil
}

// GetActiveProfileFromConfig returns the active client profile from client config.
func GetActiveProfileFromConfig(config *pb.ClientConfig, name string) (*pb.ClientProfile, error) {
	if config == nil {
		return nil, fmt.Errorf("client config is nil")
	}
	for _, profile := range config.GetProfiles() {
		if profile.GetProfileName() == name {
			return profile, nil
		}
	}
	return nil, fmt.Errorf("profile %q is not found", name)
}

// newClientLifecycleRPCClient creates a new ClientLifecycleService RPC client
// and connects to the given server address.
func newClientLifecycleRPCClient(ctx context.Context, serverAddr string) (pb.ClientLifecycleServiceClient, error) {
	conn, err := grpc.DialContext(ctx, serverAddr, grpc.WithInsecure())
	if err != nil {
		return nil, fmt.Errorf("grpc.DialContext() failed: %w", err)
	}
	return pb.NewClientLifecycleServiceClient(conn), nil
}

// prepareClientConfigDir creates the client config directory if needed.
func prepareClientConfigDir() error {
	if cachedClientConfigDir != "" {
		return os.MkdirAll(cachedClientConfigDir, 0755)
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	cachedClientConfigDir = configDir + string(os.PathSeparator) + "mieru"
	return os.MkdirAll(cachedClientConfigDir, 0755)
}

// clientConfigFilePath returns the client config file path and file type.
// If environment variable MIERU_CONFIG_FILE or MIERU_CONFIG_JSON_FILE is specified,
// those values are returned.
func clientConfigFilePath() (string, ConfigFileType, error) {
	if v, found := os.LookupEnv("MIERU_CONFIG_FILE"); found {
		cachedClientConfigFilePath = v
		cachedClientConfigDir = filepath.Dir(v)
		return cachedClientConfigFilePath, PROTOBUF_CONFIG_FILE_TYPE, nil
	}
	if v, found := os.LookupEnv("MIERU_CONFIG_JSON_FILE"); found {
		cachedClientConfigFilePath = v
		cachedClientConfigDir = filepath.Dir(v)
		return cachedClientConfigFilePath, JSON_CONFIG_FILE_TYPE, nil
	}

	if cachedClientConfigFilePath != "" {
		return cachedClientConfigFilePath, FindConfigFileType(cachedClientConfigFilePath), nil
	}

	if err := prepareClientConfigDir(); err != nil {
		return "", INVALID_CONFIG_FILE_TYPE, fmt.Errorf("prepareClientConfigDir() failed: %w", err)
	}
	cachedClientConfigFilePath = cachedClientConfigDir + string(os.PathSeparator) + "client.conf.pb"
	return cachedClientConfigFilePath, FindConfigFileType(cachedClientConfigFilePath), nil
}

func applyClientConfig(c *pb.ClientConfig) error {
	if err := ValidateClientConfigPatch(c); err != nil {
		return fmt.Errorf("ValidateClientConfigPatch() failed: %w", err)
	}
	config, err := LoadClientConfig()
	if err != nil {
		return fmt.Errorf("LoadClientConfig() failed: %w", err)
	}
	mergeClientConfigByProfile(config, c)
	if err = ValidateFullClientConfig(config); err != nil {
		return fmt.Errorf("ValidateFullClientConfig() failed: %w", err)
	}
	if err = StoreClientConfig(config); err != nil {
		return fmt.Errorf("StoreClientConfig() failed: %w", err)
	}
	return nil
}

// mergeClientConfigByProfile merges the source client config into destination.
// If a profile is specified in source, it is added to destination,
// or replacing existing profile in destination.
func mergeClientConfigByProfile(dst, src *pb.ClientConfig) {
	// Merge src profiles into dst profiles.
	dstMapping := map[string]*pb.ClientProfile{}
	srcMapping := map[string]*pb.ClientProfile{}
	for _, profile := range dst.GetProfiles() {
		dstMapping[profile.GetProfileName()] = profile
	}
	for _, profile := range src.GetProfiles() {
		srcMapping[profile.GetProfileName()] = profile
	}
	mergedProfileMapping := map[string]*pb.ClientProfile{}
	for name, profile := range dstMapping {
		mergedProfileMapping[name] = profile
	}
	for name, profile := range srcMapping {
		mergedProfileMapping[name] = profile
	}
	names := make([]string, 0, len(mergedProfileMapping))
	for name := range mergedProfileMapping {
		names = append(names, name)
	}
	sort.Strings(names)
	mergedProfiles := make([]*pb.ClientProfile, 0, len(mergedProfileMapping))
	for _, name := range names {
		mergedProfiles = append(mergedProfiles, mergedProfileMapping[name])
	}

	// Required fields.
	var activeProfile string
	if src.ActiveProfile != nil {
		activeProfile = src.GetActiveProfile()
	} else {
		activeProfile = dst.GetActiveProfile()
	}
	var sock5Port int32
	if src.Socks5Port != nil {
		sock5Port = src.GetSocks5Port()
	} else {
		sock5Port = dst.GetSocks5Port()
	}
	var loggingLevel pb.LoggingLevel
	if src.LoggingLevel != nil {
		loggingLevel = src.GetLoggingLevel()
	} else {
		loggingLevel = dst.GetLoggingLevel()
	}

	// Optional fields.
	var rpcPort *int32 = dst.RpcPort
	if src.RpcPort != nil {
		rpcPort = src.RpcPort
	}
	var advancedSettings *pb.ClientAdvancedSettings = dst.AdvancedSettings
	if src.AdvancedSettings != nil {
		advancedSettings = src.AdvancedSettings
	}
	var socks5ListenLAN *bool = dst.Socks5ListenLAN
	if src.Socks5ListenLAN != nil {
		socks5ListenLAN = src.Socks5ListenLAN
	}
	var httpProxyPort *int32 = dst.HttpProxyPort
	if src.HttpProxyPort != nil {
		httpProxyPort = src.HttpProxyPort
	}
	var httpProxyListenLAN *bool = dst.HttpProxyListenLAN
	if src.HttpProxyListenLAN != nil {
		httpProxyListenLAN = src.HttpProxyListenLAN
	}

	proto.Reset(dst)

	dst.ActiveProfile = proto.String(activeProfile)
	dst.Profiles = mergedProfiles
	dst.Socks5Port = proto.Int32(sock5Port)
	dst.LoggingLevel = &loggingLevel

	dst.RpcPort = rpcPort
	dst.AdvancedSettings = advancedSettings
	dst.Socks5ListenLAN = socks5ListenLAN
	dst.HttpProxyPort = httpProxyPort
	dst.HttpProxyListenLAN = httpProxyListenLAN
}

// deleteClientConfigFile deletes the client config file.
func deleteClientConfigFile() error {
	path, _, err := clientConfigFilePath()
	if err != nil {
		return fmt.Errorf("clientConfigFilePath() failed: %w", err)
	}
	err = os.Remove(path)
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return err
}
