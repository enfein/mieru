// Copyright (C) 2026  mieru authors
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

package socks5

import "github.com/enfein/mieru/v3/pkg/metrics"

const (
	HTTPMetricGroupName = "HTTP proxy"
)

var (
	HandshakeErrors          = metrics.RegisterMetric("socks5", "HandshakeErrors", metrics.COUNTER)
	DNSResolveErrors         = metrics.RegisterMetric("socks5", "DNSResolveErrors", metrics.COUNTER)
	UnsupportedCommandErrors = metrics.RegisterMetric("socks5", "UnsupportedCommandErrors", metrics.COUNTER)
	NetworkUnreachableErrors = metrics.RegisterMetric("socks5", "NetworkUnreachableErrors", metrics.COUNTER)
	HostUnreachableErrors    = metrics.RegisterMetric("socks5", "HostUnreachableErrors", metrics.COUNTER)
	ConnectionRefusedErrors  = metrics.RegisterMetric("socks5", "ConnectionRefusedErrors", metrics.COUNTER)
	UDPAssociateErrors       = metrics.RegisterMetric("socks5", "UDPAssociateErrors", metrics.COUNTER)
	RejectByRules            = metrics.RegisterMetric("socks5", "RejectByRules", metrics.COUNTER)

	UDPAssociateUploadBytes     = metrics.RegisterMetric("socks5 UDP associate", "UploadBytes", metrics.COUNTER)
	UDPAssociateDownloadBytes   = metrics.RegisterMetric("socks5 UDP associate", "DownloadBytes", metrics.COUNTER)
	UDPAssociateUploadPackets   = metrics.RegisterMetric("socks5 UDP associate", "UploadPackets", metrics.COUNTER)
	UDPAssociateDownloadPackets = metrics.RegisterMetric("socks5 UDP associate", "DownloadPackets", metrics.COUNTER)

	HTTPRequests     = metrics.RegisterMetric(HTTPMetricGroupName, "Requests", metrics.COUNTER)
	HTTPConnErrors   = metrics.RegisterMetric(HTTPMetricGroupName, "ConnErrors", metrics.COUNTER)
	HTTPSchemeErrors = metrics.RegisterMetric(HTTPMetricGroupName, "SchemeErrors", metrics.COUNTER)
)
