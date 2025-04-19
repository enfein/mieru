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

package stderror

const (
	ClientConfigIsEmpty                      = "mieru client config is empty"
	ClientConfigNotExist                     = "mieru client config file doesn't exist"
	ClientGetActiveProfileFailedErr          = "mieru client get active profile failed: %w"
	ClientNotRunning                         = "mieru client is not running"
	ClientNotRunningErr                      = "mieru client is not running: %w"
	CreateClientManagementRPCClientFailedErr = "create mieru client management RPC client failed: %w"
	CreateEmptyServerConfigFailedErr         = "create empty mita server config file failed: %w"
	CreateServerManagementRPCClientFailedErr = "create mita server management RPC client failed: %w"
	CreateSocks5ServerFailedErr              = "create socks5 server failed: %w"
	DecodeHashedPasswordFailedErr            = "decode hashed password failed: %w"
	ExitFailedErr                            = "process exit failed: %w"
	GetClientConfigFailedErr                 = "get mieru client config failed: %w"
	GetConnectionsFailedErr                  = "get connections failed: %w"
	GetHeapProfileFailedErr                  = "get heap profile failed: %w"
	GetMemoryStatisticsFailedErr             = "get memory statistics failed: %w"
	GetMetricsFailedErr                      = "get metrics failed: %w"
	GetServerConfigFailedErr                 = "get mita server config failed: %w"
	GetServerStatusFailedErr                 = "get mita server status failed: %w"
	GetThreadDumpFailedErr                   = "get thread dump failed: %w"
	GetUsersFailedErr                        = "get users failed: %w"
	InvalidPortBindingsErr                   = "invalid port bindings: %w"
	InvalidTransportProtocol                 = "invalid transport protocol"
	IPAddressNotFound                        = "IP address not found from domain name %q"
	LookupIPFailedErr                        = "look up IP address failed: %w"
	ParseIPFailed                            = "parse IP address failed"
	ReloadServerFailedErr                    = "reload mita server failed: %w"
	SegmentSizeTooBig                        = "segment size too big"
	ServerNotRunningErr                      = "mita server daemon is not running: %w"
	ServerNotRunningWithCommand              = "mita server daemon is not running; please run command \"sudo systemctl restart mita\" to start server daemon; run command \"sudo journalctl -e -u mita --no-pager\" to check log if unable to start"
	ServerProxyNotRunningErr                 = "mita server proxy is not running: %w"
	SetServerConfigFailedErr                 = "set mita server config failed: %w"
	StartClientFailedErr                     = "start mieru client failed: %w"
	StartCPUProfileFailedErr                 = "start CPU profile failed: %w"
	StartServerProxyFailedErr                = "start mita server proxy failed: %w"
	StopServerProxyFailedErr                 = "stop mita server proxy failed: %w"
	StoreClientConfigFailedErr               = "store mieru client config failed: %w"
	ValidateFullClientConfigFailedErr        = "validate full client config failed: %w"
	ValidateServerConfigPatchFailedErr       = "validate server config patch failed: %w"
)
