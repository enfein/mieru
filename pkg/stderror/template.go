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
	ClientConfigIsEmpty                     = "mieru client config is empty"
	ClientConfigNotExist                    = "mieru client config file doesn't exist"
	ClientGetActiveProfileFailedErr         = "mieru client get active profile failed: %w"
	ClientNotRunning                        = "mieru client is not running"
	ClientNotRunningErr                     = "mieru client is not running: %w"
	CreateClientLifecycleRPCClientFailedErr = "create mieru client lifecycle RPC client failed: %w"
	CreateEmptyServerConfigFailedErr        = "create empty mieru server config file failed: %w"
	CreateServerConfigRPCClientFailedErr    = "create mieru server config RPC client failed: %w"
	CreateServerLifecycleRPCClientFailedErr = "create mieru server lifecycle RPC client failed: %w"
	CreateSocks5ServerFailedErr             = "create socks5 server failed: %w"
	DecodeHashedPasswordFailedErr           = "decode hashed password failed: %w"
	ExitFailedErr                           = "process exit failed: %w"
	GetClientConfigFailedErr                = "get mieru client config failed: %w"
	GetHeapProfileFailedErr                 = "get heap profile failed: %w"
	GetServerConfigFailedErr                = "get mieru server config failed: %w"
	GetServerStatusFailedErr                = "get mieru server status failed: %w"
	GetThreadDumpFailedErr                  = "get thread dump failed: %w"
	InvalidTransportProtocol                = "invalid transport protocol"
	LoadClientConfigFailedErr               = "load mieru client config failed: %w"
	LoadServerConfigFailedErr               = "load mieru server config failed: %w"
	LookupIPFailedErr                       = "look up IP address failed: %w"
	ParseIPFailedErr                        = "parse IP address failed: %w"
	SegmentSizeTooBig                       = "segment size too big"
	ServerNotRunning                        = "mieru server daemon is not running"
	ServerNotRunningErr                     = "mieru server daemon is not running: %w"
	ServerProxyNotRunningErr                = "mieru server proxy is not running: %w"
	SetServerConfigFailedErr                = "set mieru server config failed: %w"
	StartClientFailedErr                    = "start mieru client failed: %w"
	StartCPUProfileFailedErr                = "start CPU profile failed: %w"
	StartServerProxyFailedErr               = "start mieru server proxy failed: %w"
	StopServerProxyFailedErr                = "stop mieru server proxy failed: %w"
	StoreClientConfigFailedErr              = "store mieru client config failed: %w"
	ValidateFullClientConfigFailedErr       = "validate full client config failed: %w"
	ValidateServerConfigPatchFailedErr      = "validate server config patch failed: %w"
)
