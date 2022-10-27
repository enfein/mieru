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
	"fmt"
	"sync"
	"time"

	"github.com/enfein/mieru/pkg/log"
)

var (
	// connections
	MaxConn         uint64 // max number of connections ever reached
	ActiveOpens     uint64 // accumulated active open connections
	PassiveOpens    uint64 // accumulated passive open connections
	CurrEstablished uint64 // current number of established connections

	// server decryption
	ServerDirectDecrypt        uint64 // number of decryption using the cipher block associated with the connection
	ServerFailedDirectDecrypt  uint64 // number of decryption using the stored cipher block but failed
	ServerFailedIterateDecrypt uint64 // number of decryption that failed after iterating all possible cipher blocks

	// client decryption
	ClientDirectDecrypt       uint64 // number of decryption using the cipher block associated with the connection
	ClientFailedDirectDecrypt uint64 // number of decryption using the stored cipher block but failed

	// UDP packets
	InPkts  uint64 // incoming packets count
	OutPkts uint64 // outgoing packets count

	// KCP segments
	InSegs           uint64 // incoming KCP segments
	OutSegs          uint64 // outgoing KCP segments
	RepeatSegs       uint64 // repeated KCP segments
	LostSegs         uint64 // lost KCP segments
	OutOfWindowSegs  uint64 // KCP segments that have sequence number out of receiving window
	FastRetransSegs  uint64 // fast retransmission KCP segments
	EarlyRetransSegs uint64 // early retransmission KCP segments
	RetransSegs      uint64 // retransmission KCP segments

	// UDP bytes
	UDPInBytes  uint64 // UDP bytes received
	UDPOutBytes uint64 // UDP bytes sent

	// KCP bytes
	KCPBytesSent     uint64 // KCP bytes sent from upper level
	KCPBytesReceived uint64 // KCP bytes delivered to upper level
	KCPPaddingSent   uint64 // KCP bytes sent for padding purpose

	// TCP bytes
	TCPInBytes     uint64 // TCP bytes received
	TCPOutBytes    uint64 // TCP bytes sent
	TCPPaddingSent uint64 // TCP bytes sent for padding purpose

	// Replay protection
	ReplayKnownSession uint64 // replay packets sent from a known session
	ReplayNewSession   uint64 // replay packets sent from a new session

	// Socks5 UDP association
	UDPAssociateInBytes  uint64 // incoming UDP association bytes
	UDPAssociateOutBytes uint64 // outgoing UDP association bytes
	UDPAssociateInPkts   uint64 // incoming UDP association packets count
	UDPAssociateOutPkts  uint64 // outgoing UDP association packets count

	// UDP Errors
	UDPInErrors      uint64 // UDP read errors reported from net.PacketConn
	KCPInErrors      uint64 // packet input errors reported from KCP
	KCPSendErrors    uint64 // packet send errors reported from KCP
	KCPReceiveErrors uint64 // packet receive errors reported from KCP

	// TCP Errors
	TCPSendErrors    uint64 // TCP send errors
	TCPReceiveErrors uint64 // TCP receive errors

	// Socks5 Errors
	Socks5HandshakeErrors          uint64 // Socks5 handshake errors
	Socks5DNSResolveErrors         uint64 // Socks5 can't resolve DNS address
	Socks5UnsupportedCommandErrors uint64 // Socks5 command is not supported
	Socks5NetworkUnreachableErrors uint64 // Destination network is unreachable
	Socks5HostUnreachableErrors    uint64 // Destination Host is unreachable
	Socks5ConnectionRefusedErrors  uint64 // Connection is refused
	Socks5UDPAssociateErrors       uint64 // UDP associate errors
)

var ticker *time.Ticker
var logDuration time.Duration
var done chan struct{}
var mutex sync.Mutex

func init() {
	logDuration = time.Minute
	done = make(chan struct{})
}

// Enable metrics logging with the given time duration.
func EnableLogging() {
	mutex.Lock()
	defer mutex.Unlock()
	if ticker == nil {
		ticker = time.NewTicker(logDuration)
		go logMetrics()
		log.Infof("enabled metrics logging with duration %v", logDuration)
	}
}

// Disable metrics logging.
func DisableLogging() {
	mutex.Lock()
	defer mutex.Unlock()
	done <- struct{}{}
	if ticker != nil {
		ticker.Stop()
		ticker = nil
		log.Infof("disabled metrics logging")
	}
}

// Set the metrics logging time duration.
func SetLoggingDuration(duration time.Duration) error {
	if duration.Seconds() <= 0 {
		return fmt.Errorf("duration must be a positive number")
	}
	mutex.Lock()
	defer mutex.Unlock()
	logDuration = duration
	return nil
}

func logMetrics() {
	for {
		select {
		case <-ticker.C:
			log.Infof("[metrics]")
			LogConnections()
			LogServerDecryption()
			LogClientDecryption()
			LogUDPPackets()
			LogKCPSegments()
			LogUDPBytes()
			LogKCPBytes()
			LogTCPBytes()
			LogUDPAssociation()
			LogReplay()
			LogUDPErrors()
			LogTCPErrors()
			LogSocks5Errors()
		case <-done:
			return
		}
	}
}

func LogConnections() {
	log.WithFields(log.Fields{
		"MaxConn":         MaxConn,
		"ActiveOpens":     ActiveOpens,
		"PassiveOpens":    PassiveOpens,
		"CurrEstablished": CurrEstablished,
	}).Infof("[metrics - connections]")
}

func LogServerDecryption() {
	log.WithFields(log.Fields{
		"ServerDirectDecrypt":        ServerDirectDecrypt,
		"ServerFailedDirectDecrypt":  ServerFailedDirectDecrypt,
		"ServerFailedIterateDecrypt": ServerFailedIterateDecrypt,
	}).Infof("[metrics - server decryption]")
}

func LogClientDecryption() {
	log.WithFields(log.Fields{
		"ClientDirectDecrypt":       ClientDirectDecrypt,
		"ClientFailedDirectDecrypt": ClientFailedDirectDecrypt,
	}).Infof("[metrics - client decryption]")
}

func LogUDPPackets() {
	log.WithFields(log.Fields{
		"InPkts":  InPkts,
		"OutPkts": OutPkts,
	}).Infof("[metrics - UDP packets]")
}

func LogKCPSegments() {
	log.WithFields(log.Fields{
		"InSegs":           InSegs,
		"OutSegs":          OutSegs,
		"RepeatSegs":       RepeatSegs,
		"LostSegs":         LostSegs,
		"OutOfWindowSegs":  OutOfWindowSegs,
		"FastRetransSegs":  FastRetransSegs,
		"EarlyRetransSegs": EarlyRetransSegs,
		"RetransSegs":      RetransSegs,
	}).Infof("[metrics - KCP segments]")
}

func LogUDPBytes() {
	log.WithFields(log.Fields{
		"InBytes":  UDPInBytes,
		"OutBytes": UDPOutBytes,
	}).Infof("[metrics - UDP bytes]")
}

func LogKCPBytes() {
	log.WithFields(log.Fields{
		"BytesSent":     KCPBytesSent,
		"BytesReceived": KCPBytesReceived,
		"PaddingSent":   KCPPaddingSent,
	}).Infof("[metrics - KCP bytes]")
}

func LogTCPBytes() {
	log.WithFields(log.Fields{
		"InBytes":     TCPInBytes,
		"OutBytes":    TCPOutBytes,
		"PaddingSent": TCPPaddingSent,
	}).Infof("[metrics - TCP bytes]")
}

func LogUDPAssociation() {
	log.WithFields(log.Fields{
		"UDPAssociateInBytes":  UDPAssociateInBytes,
		"UDPAssociateOutBytes": UDPAssociateOutBytes,
		"UDPAssociateInPkts":   UDPAssociateInPkts,
		"UDPAssociateOutPkts":  UDPAssociateOutPkts,
	}).Infof("[metrics - socks5 UDP association]")
}

func LogReplay() {
	log.WithFields(log.Fields{
		"ReplayKnownSession": ReplayKnownSession,
		"ReplayNewSession":   ReplayNewSession,
	}).Infof("[metrics - replay protection]")
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

func LogSocks5Errors() {
	log.WithFields(log.Fields{
		"HandshakeErrors":    Socks5HandshakeErrors,
		"DNSResolveErrors":   Socks5DNSResolveErrors,
		"UnsupportedCommand": Socks5UnsupportedCommandErrors,
		"NetworkUnreachable": Socks5NetworkUnreachableErrors,
		"HostUnreachable":    Socks5HostUnreachableErrors,
		"ConnectionRefused":  Socks5ConnectionRefusedErrors,
		"UDPAssociateErrors": Socks5UDPAssociateErrors,
	}).Infof("[metrics - socks5 errors]")
}
