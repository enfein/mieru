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
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlcommon"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlgrpc"
	pb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/metrics"
	"github.com/enfein/mieru/v3/pkg/protocol"
	"github.com/enfein/mieru/v3/pkg/socks5"
	"github.com/enfein/mieru/v3/pkg/stderror"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
)

const (
	EnvMieruConfigFile     = "MIERU_CONFIG_FILE"
	EnvMieruConfigJSONFile = "MIERU_CONFIG_JSON_FILE"
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

	// clientMuxRef holds a pointer to client multiplexier.
	clientMuxRef atomic.Pointer[protocol.Mux]
)

func SetClientRPCServerRef(server *grpc.Server) {
	clientRPCServerRef.Store(server)
}

func SetClientSocks5ServerRef(server *socks5.Server) {
	clientSocks5ServerRef.Store(server)
}

func SetClientMuxRef(mux *protocol.Mux) {
	clientMuxRef.Store(mux)
}

// clientManagementService implements ClientManagementService defined in rpc.proto.
type clientManagementService struct {
	appctlgrpc.UnimplementedClientManagementServiceServer
}

func (c *clientManagementService) GetStatus(ctx context.Context, req *pb.Empty) (*pb.AppStatusMsg, error) {
	status := GetAppStatus()
	log.Infof("return app status %s back to RPC caller", status.String())
	return &pb.AppStatusMsg{Status: &status}, nil
}

func (c *clientManagementService) Exit(ctx context.Context, req *pb.Empty) (*pb.Empty, error) {
	SetAppStatus(pb.AppStatus_STOPPING)
	log.Infof("received Exit request from RPC caller")
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
	log.Infof("completed Exit request from RPC caller")
	return &pb.Empty{}, nil
}

func (c *clientManagementService) GetMetrics(ctx context.Context, req *pb.Empty) (*pb.Metrics, error) {
	b, err := metrics.GetMetricsAsJSON()
	if err != nil {
		return &pb.Metrics{}, err
	}
	return &pb.Metrics{Json: proto.String(string(b))}, nil
}

func (c *clientManagementService) GetSessionInfoList(context.Context, *pb.Empty) (*pb.SessionInfoList, error) {
	mux := clientMuxRef.Load()
	if mux == nil {
		return &pb.SessionInfoList{}, fmt.Errorf("client multiplexier is unavailable")
	}
	return mux.ExportSessionInfoList(), nil
}

func (c *clientManagementService) GetThreadDump(ctx context.Context, req *pb.Empty) (*pb.ThreadDump, error) {
	return &pb.ThreadDump{ThreadDump: proto.String(common.GetAllStackTrace())}, nil
}

func (c *clientManagementService) StartCPUProfile(ctx context.Context, req *pb.ProfileSavePath) (*pb.Empty, error) {
	err := common.StartCPUProfile(req.GetFilePath())
	return &pb.Empty{}, err
}

func (c *clientManagementService) StopCPUProfile(ctx context.Context, req *pb.Empty) (*pb.Empty, error) {
	common.StopCPUProfile()
	return &pb.Empty{}, nil
}

func (c *clientManagementService) GetHeapProfile(ctx context.Context, req *pb.ProfileSavePath) (*pb.Empty, error) {
	err := common.GetHeapProfile(req.GetFilePath())
	return &pb.Empty{}, err
}

func (c *clientManagementService) GetMemoryStatistics(ctx context.Context, req *pb.Empty) (*pb.MemoryStatistics, error) {
	return getMemoryStatistics()
}

// NewClientManagementService creates a new ClientManagementService RPC server.
func NewClientManagementService() *clientManagementService {
	return &clientManagementService{}
}

// NewClientManagementRPCClient creates a new ClientManagementService RPC client.
// It loads client config to find the server address.
func NewClientManagementRPCClient() (appctlgrpc.ClientManagementServiceClient, error) {
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
	return newClientManagementRPCClient(rpcAddr)
}

// IsClientDaemonRunning detects if client daemon is running by using
// ClientManagementService.GetStatus() RPC.
func IsClientDaemonRunning(ctx context.Context) error {
	client, err := NewClientManagementRPCClient()
	if err != nil {
		return fmt.Errorf("NewClientManagementRPCClient() failed: %w", err)
	}
	timedctx, cancelFunc := context.WithTimeout(ctx, RPCTimeout)
	defer cancelFunc()
	if _, err = client.GetStatus(timedctx, &pb.Empty{}); err != nil {
		return fmt.Errorf("ClientManagementService.GetStatus() failed: %w", err)
	}
	return nil
}

// GetJSONClientConfig returns the client config as JSON.
func GetJSONClientConfig() (string, error) {
	config, err := LoadClientConfig()
	if err != nil {
		return "", fmt.Errorf("LoadClientConfig() failed: %w", err)
	}
	b, err := common.MarshalJSON(config)
	if err != nil {
		return "", fmt.Errorf("common.MarshalJSON() failed: %w", err)
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
		if err := common.UnmarshalJSON(b, c); err != nil {
			return nil, fmt.Errorf("common.UnmarshalJSON() failed: %w", err)
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
		if b, err = common.MarshalJSON(config); err != nil {
			return fmt.Errorf("common.MarshalJSON() failed: %w", err)
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
	if err = common.UnmarshalJSON(b, c); err != nil {
		return fmt.Errorf("common.UnmarshalJSON() failed: %w", err)
	}
	return applyClientConfig(c)
}

// ApplyURLClientConfig applies user provided client config URL.
func ApplyURLClientConfig(u string) error {
	if strings.HasPrefix(u, "mieru://") {
		c, err := URLToClientConfig(u)
		if err != nil {
			return fmt.Errorf("URLToClientConfig() failed: %w", err)
		}
		return applyClientConfig(c)
	} else if strings.HasPrefix(u, "mierus://") {
		p, err := URLToClientProfile(u)
		if err != nil {
			return fmt.Errorf("URLToClientProfile() failed: %w", err)
		}
		c := &pb.ClientConfig{
			Profiles: []*pb.ClientProfile{p},
		}
		return applyClientConfig(c)
	} else {
		return fmt.Errorf("unrecognized URL scheme. URL must begin with mieru:// or mierus://")
	}
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
// 2. validate each profile
// 3. for each socks5 authentication, the user and password are not empty
// 4. metrics logging interval is valid, and it is not less than 1 second
func ValidateClientConfigPatch(patch *pb.ClientConfig) error {
	for _, profile := range patch.GetProfiles() {
		if err := appctlcommon.ValidateClientConfigSingleProfile(profile); err != nil {
			return err
		}
	}
	for _, auth := range patch.GetSocks5Authentication() {
		if auth.GetUser() == "" {
			return fmt.Errorf("socks5 authentication user is not set")
		}
		if auth.GetPassword() == "" {
			return fmt.Errorf("socks5 authentication password is not set")
		}
	}
	if patch.GetAdvancedSettings().GetMetricsLoggingInterval() != "" {
		d, err := time.ParseDuration(patch.GetAdvancedSettings().GetMetricsLoggingInterval())
		if err != nil {
			return fmt.Errorf("metrics logging interval %q is invalid: %w", patch.GetAdvancedSettings().GetMetricsLoggingInterval(), err)
		}
		if d < time.Second {
			return fmt.Errorf("metrics logging interval %q is less than 1 second", patch.GetAdvancedSettings().GetMetricsLoggingInterval())
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
// 6. if set, metrics logging interval is valid, and it is not less than 1 second
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

// ClientUpdaterHistoryPath returns the file path to retrieve
// client updater history.
func ClientUpdaterHistoryPath() (string, error) {
	if err := prepareClientConfigDir(); err != nil {
		return "", err
	}
	return filepath.Join(cachedClientConfigDir, "client.updater.pb"), nil
}

// newClientManagementRPCClient creates a new ClientManagementService RPC client
// and connects to the given server address.
func newClientManagementRPCClient(serverAddr string) (appctlgrpc.ClientManagementServiceClient, error) {
	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(MaxRecvMsgSize)))
	if err != nil {
		return nil, fmt.Errorf("grpc.NewClient() failed: %w", err)
	}
	return appctlgrpc.NewClientManagementServiceClient(conn), nil
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
	if v, found := os.LookupEnv(EnvMieruConfigFile); found {
		cachedClientConfigFilePath = v
		cachedClientConfigDir = filepath.Dir(v)
		return cachedClientConfigFilePath, PROTOBUF_CONFIG_FILE_TYPE, nil
	}
	if v, found := os.LookupEnv(EnvMieruConfigJSONFile); found {
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
	var socks5Authentication []*pb.Auth = dst.Socks5Authentication
	if src.Socks5Authentication != nil {
		socks5Authentication = src.Socks5Authentication
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
	dst.Socks5Authentication = socks5Authentication
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
