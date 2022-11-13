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

package metrics

import (
	"github.com/enfein/mieru/pkg/log"
)

var (
	// Max number of connections ever reached.
	MaxConn *Metric = RegisterMetric("connections", "MaxConn")

	// Accumulated active open connections.
	ActiveOpens *Metric = RegisterMetric("connections", "ActiveOpens")

	// Accumulated passive open connections.
	PassiveOpens *Metric = RegisterMetric("connections", "PassiveOpens")

	// Current number of established connections.
	CurrEstablished *Metric = RegisterMetric("connections", "CurrEstablished")
)

var (
	// UDP bytes
	UDPInBytes  uint64 // UDP bytes received
	UDPOutBytes uint64 // UDP bytes sent

	// TCP bytes
	TCPInBytes     uint64 // TCP bytes received
	TCPOutBytes    uint64 // TCP bytes sent
	TCPPaddingSent uint64 // TCP bytes sent for padding purpose

	// UDP Errors
	UDPInErrors      uint64 // UDP read errors reported from net.PacketConn
	KCPInErrors      uint64 // packet input errors reported from KCP
	KCPSendErrors    uint64 // packet send errors reported from KCP
	KCPReceiveErrors uint64 // packet receive errors reported from KCP

	// TCP Errors
	TCPSendErrors    uint64 // TCP send errors
	TCPReceiveErrors uint64 // TCP receive errors
)

func LogUDPBytes() {
	log.WithFields(log.Fields{
		"InBytes":  UDPInBytes,
		"OutBytes": UDPOutBytes,
	}).Infof("[metrics - UDP bytes]")
}

func LogTCPBytes() {
	log.WithFields(log.Fields{
		"InBytes":     TCPInBytes,
		"OutBytes":    TCPOutBytes,
		"PaddingSent": TCPPaddingSent,
	}).Infof("[metrics - TCP bytes]")
}

func LogUDPErrors() {
	log.WithFields(log.Fields{
		"UDPInErrors":      UDPInErrors,
		"KCPInErrors":      KCPInErrors,
		"KCPSendErrors":    KCPSendErrors,
		"KCPReceiveErrors": KCPReceiveErrors,
	}).Infof("[metrics - UDP errors]")
}

func LogTCPErrors() {
	log.WithFields(log.Fields{
		"TCPSendErrors":    TCPSendErrors,
		"TCPReceiveErrors": TCPReceiveErrors,
	}).Infof("[metrics - TCP errors]")
}
