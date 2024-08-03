// Copyright (C) 2023  mieru authors
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

package protocolv2

import (
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/enfein/mieru/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/pkg/cipher"
	"github.com/enfein/mieru/pkg/congestion"
	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/mathext"
	"github.com/enfein/mieru/pkg/metrics"
	"github.com/enfein/mieru/pkg/stderror"
	"github.com/enfein/mieru/pkg/util"
)

const (
	segmentTreeCapacity = 4096
	segmentChanCapacity = 1024

	minWindowSize = 16
	maxWindowSize = segmentTreeCapacity

	serverRespTimeout        = 10 * time.Second
	sessionHeartbeatInterval = 5 * time.Second

	earlyRetransmission  = 3
	txTimeoutBackOff     = 1.25
	maxBackOffMultiplier = 20.0
)

type sessionState byte

const (
	sessionInit        sessionState = 0
	sessionAttached    sessionState = 1
	sessionEstablished sessionState = 2
	sessionClosed      sessionState = 3
)

func (ss sessionState) String() string {
	switch ss {
	case sessionInit:
		return "INITIALIZED"
	case sessionAttached:
		return "ATTACHED"
	case sessionEstablished:
		return "ESTABLISHED"
	case sessionClosed:
		return "CLOSED"
	default:
		return "UNKNOWN"
	}
}

type Session struct {
	conn  Underlay           // underlay connection
	block cipher.BlockCipher // cipher to encrypt and decrypt data

	id         uint32       // session ID number
	isClient   bool         // if this session is owned by client
	mtu        int          // L2 maxinum transmission unit
	remoteAddr net.Addr     // specify remote network address, used by UDP
	state      sessionState // session state
	status     statusCode   // session status
	users      map[string]*appctlpb.User

	ready         chan struct{} // indicate the session is ready to use
	done          chan struct{} // indicate the session is complete
	readDeadline  time.Time     // read deadline
	writeDeadline time.Time     // write deadline
	inputErr      chan error    // input error
	outputErr     chan error    // output error

	sendQueue *segmentTree  // segments waiting to send
	sendBuf   *segmentTree  // segments sent but not acknowledged
	recvBuf   *segmentTree  // segments received but acknowledge is not sent
	recvQueue *segmentTree  // segments waiting to be read by application
	recvChan  chan *segment // channel to receive segments from underlay

	nextSend      uint32      // next sequence number to send a segment
	nextRecv      uint32      // next sequence number to receive
	lastRXTime    time.Time   // last timestamp when a segment is received
	lastTXTime    time.Time   // last timestamp when a segment is sent
	ackOnDataRecv atomic.Bool // whether ack should be sent due to receive of new data
	unreadBuf     []byte      // payload removed from the recvQueue that haven't been read by application

	uploadBytes   metrics.Metric // number of bytes from client to server, only used by server
	downloadBytes metrics.Metric // number of bytes from server to client, only used by server

	rttStat             *congestion.RTTStats
	legacysendAlgorithm *congestion.CubicSendAlgorithm
	sendAlgorithm       *congestion.BBRSender
	remoteWindowSize    uint16

	wg    sync.WaitGroup
	rLock sync.Mutex
	wLock sync.Mutex
	cLock sync.Mutex
	sLock sync.Mutex
}

// Session must implement net.Conn interface.
var _ net.Conn = &Session{}

// NewSession creates a new session.
func NewSession(id uint32, isClient bool, mtu int) *Session {
	rttStat := congestion.NewRTTStats()
	rttStat.SetMaxAckDelay(outputLoopInterval)
	rttStat.SetRTOMultiplier(txTimeoutBackOff)
	return &Session{
		conn:                nil,
		block:               nil,
		id:                  id,
		isClient:            isClient,
		mtu:                 mtu,
		state:               sessionInit,
		status:              statusOK,
		ready:               make(chan struct{}),
		done:                make(chan struct{}),
		readDeadline:        time.Time{},
		writeDeadline:       time.Time{},
		inputErr:            make(chan error, 2), // allow nested
		outputErr:           make(chan error, 2), // allow nested
		sendQueue:           newSegmentTree(segmentTreeCapacity),
		sendBuf:             newSegmentTree(segmentTreeCapacity),
		recvBuf:             newSegmentTree(segmentTreeCapacity),
		recvQueue:           newSegmentTree(segmentTreeCapacity),
		recvChan:            make(chan *segment, segmentChanCapacity),
		lastRXTime:          time.Now(),
		lastTXTime:          time.Now(),
		rttStat:             rttStat,
		legacysendAlgorithm: congestion.NewCubicSendAlgorithm(minWindowSize, maxWindowSize),
		sendAlgorithm:       congestion.NewBBRSender(fmt.Sprintf("%d", id), rttStat),
		remoteWindowSize:    minWindowSize,
	}
}

func (s *Session) String() string {
	if s.conn == nil {
		return fmt.Sprintf("Session{id=%v}", s.id)
	}
	return fmt.Sprintf("Session{id=%v, local=%v, remote=%v}", s.id, s.LocalAddr(), s.RemoteAddr())
}

// Read lets a user to read data from receive queue.
func (s *Session) Read(b []byte) (n int, err error) {
	s.rLock.Lock()
	defer s.rLock.Unlock()
	if s.isStateBefore(sessionAttached, false) {
		return 0, fmt.Errorf("%v is not ready for Read()", s)
	}
	if s.isStateAfter(sessionClosed, true) {
		return 0, io.ErrClosedPipe
	}
	defer func() {
		s.readDeadline = time.Time{}
	}()
	if log.IsLevelEnabled(log.TraceLevel) {
		log.Tracef("%v trying to read %d bytes", s, len(b))
	}

	// Read remaining data that application failed to read last time.
	if len(s.unreadBuf) > 0 {
		n = copy(b, s.unreadBuf)
		if n == len(s.unreadBuf) {
			s.unreadBuf = nil
		} else {
			s.unreadBuf = s.unreadBuf[n:]
		}
		if log.IsLevelEnabled(log.TraceLevel) {
			log.Tracef("%v read %d bytes", s, n)
		}
		if !s.isClient && s.uploadBytes != nil {
			s.uploadBytes.Add(int64(n))
		}
		return n, nil
	}

	// Stop reading when deadline is reached.
	var timeC <-chan time.Time
	if !s.readDeadline.IsZero() {
		timeC = time.After(time.Until(s.readDeadline))
	}

	for {
		if s.recvQueue.Len() > 0 {
			// Some segments in segment tree are ready to read.
			for {
				seg, ok := s.recvQueue.DeleteMin()
				if !ok {
					break
				}
				if s.isClient && seg.metadata.Protocol() == openSessionResponse && s.isState(sessionAttached) {
					s.forwardStateTo(sessionEstablished)
				}
				if s.unreadBuf == nil {
					s.unreadBuf = make([]byte, 0)
				}
				s.unreadBuf = append(s.unreadBuf, seg.payload...)
			}
			if len(s.unreadBuf) > 0 {
				break
			}
		} else {
			// Wait for incoming segments.
			select {
			case <-s.done:
				return 0, io.EOF
			case <-s.inputErr:
				return 0, io.ErrUnexpectedEOF
			case <-timeC:
				return 0, stderror.ErrTimeout
			case <-s.recvQueue.chanNotEmptyEvent:
				// New segments are ready to read.
			}
		}
	}

	n = copy(b, s.unreadBuf)
	if n == len(s.unreadBuf) {
		s.unreadBuf = nil
	} else {
		s.unreadBuf = s.unreadBuf[n:]
	}
	if log.IsLevelEnabled(log.TraceLevel) {
		log.Tracef("%v read %d bytes", s, n)
	}
	if !s.isClient && s.uploadBytes != nil {
		s.uploadBytes.Add(int64(n))
	}
	return n, nil
}

// Write stores the data to send queue.
func (s *Session) Write(b []byte) (n int, err error) {
	s.wLock.Lock()
	defer s.wLock.Unlock()

	if s.isStateBefore(sessionAttached, false) {
		return 0, fmt.Errorf("%v is not ready for Write()", s)
	}
	if s.isStateAfter(sessionClosed, true) {
		return 0, io.ErrClosedPipe
	}
	defer func() {
		s.writeDeadline = time.Time{}
	}()

	if s.isClient && s.isState(sessionAttached) {
		// Before the first write, client needs to send open session request.
		seg := &segment{
			metadata: &sessionStruct{
				baseStruct: baseStruct{
					protocol: uint8(openSessionRequest),
				},
				sessionID: s.id,
				seq:       s.nextSend,
			},
			transport: s.conn.TransportProtocol(),
		}
		s.nextSend++
		if len(b) <= MaxSessionOpenPayload {
			seg.metadata.(*sessionStruct).payloadLen = uint16(len(b))
			seg.payload = make([]byte, len(b))
			copy(seg.payload, b)
		}
		if log.IsLevelEnabled(log.TraceLevel) {
			log.Tracef("%v writing %d bytes with open session request", s, len(seg.payload))
		}
		s.sendQueue.InsertBlocking(seg)
		if len(seg.payload) > 0 {
			return len(seg.payload), nil
		}
	}

	n = len(b)
	if log.IsLevelEnabled(log.TraceLevel) {
		log.Tracef("%v writing %d bytes", s, n)
	}
	for len(b) > 0 {
		sizeToSend := mathext.Min(len(b), maxPDU)
		if _, err = s.writeChunk(b[:sizeToSend]); err != nil {
			return 0, err
		}
		b = b[sizeToSend:]
	}
	if log.IsLevelEnabled(log.TraceLevel) {
		log.Tracef("%v wrote %d bytes", s, n)
	}
	if !s.isClient && s.downloadBytes != nil {
		s.downloadBytes.Add(int64(n))
	}
	return n, nil
}

// Close terminates the session.
func (s *Session) Close() error {
	s.cLock.Lock()
	defer s.cLock.Unlock()
	select {
	case <-s.done:
		s.forwardStateTo(sessionClosed)
		return nil
	default:
	}

	log.Debugf("Closing %v", s)
	s.sendQueue.DeleteAll()
	s.sendBuf.DeleteAll()
	s.recvBuf.DeleteAll()
	s.recvQueue.DeleteAll()
	if s.isState(sessionAttached) || s.isState(sessionEstablished) {
		// Send closeSessionRequest, but don't wait for closeSessionResponse,
		// because the underlay connection may be already broken.
		// The closeSessionRequest won't be sent again.
		seg := &segment{
			metadata: &sessionStruct{
				baseStruct: baseStruct{
					protocol: uint8(closeSessionRequest),
				},
				sessionID:  s.id,
				seq:        s.nextSend,
				statusCode: uint8(s.status),
			},
			transport: s.conn.TransportProtocol(),
		}
		s.nextSend++
		switch s.conn.TransportProtocol() {
		case util.TCPTransport:
			s.sendQueue.InsertBlocking(seg)
		case util.UDPTransport:
			if err := s.output(seg, s.RemoteAddr()); err != nil {
				log.Debugf("output() failed: %v", err)
			}
		default:
			log.Debugf("Unsupported transport protocol %v", s.conn.TransportProtocol())
		}
	}

	// Wait for sendQueue to flush.
	timeC := time.After(time.Second)
	select {
	case <-timeC:
	case <-s.sendQueue.chanEmptyEvent:
	}

	s.forwardStateTo(sessionClosed)
	close(s.done)
	log.Debugf("Closed %v", s)
	metrics.CurrEstablished.Add(-1)
	return nil
}

func (s *Session) LocalAddr() net.Addr {
	return s.conn.LocalAddr()
}

func (s *Session) RemoteAddr() net.Addr {
	if !util.IsNilNetAddr(s.remoteAddr) {
		return s.remoteAddr
	}
	return s.conn.RemoteAddr()
}

// SetDeadline implements net.Conn.
func (s *Session) SetDeadline(t time.Time) error {
	s.readDeadline = t
	s.writeDeadline = t
	return nil
}

// SetReadDeadline implements net.Conn.
func (s *Session) SetReadDeadline(t time.Time) error {
	s.readDeadline = t
	return nil
}

// SetWriteDeadline implements net.Conn.
func (s *Session) SetWriteDeadline(t time.Time) error {
	s.writeDeadline = t
	return nil
}

// ToSessionInfo creates related SessionInfo structure.
func (s *Session) ToSessionInfo() SessionInfo {
	info := SessionInfo{
		ID:         fmt.Sprintf("%d", s.id),
		LocalAddr:  s.LocalAddr().String(),
		RemoteAddr: s.RemoteAddr().String(),
		State:      s.state.String(),
		RecvQBuf:   fmt.Sprintf("%d+%d", s.recvQueue.Len(), s.recvBuf.Len()),
		SendQBuf:   fmt.Sprintf("%d+%d", s.sendQueue.Len(), s.sendBuf.Len()),
		LastRecv:   fmt.Sprintf("%v", time.Since(s.lastRXTime).Truncate(time.Second)),
		LastSend:   fmt.Sprintf("%v", time.Since(s.lastTXTime).Truncate(time.Second)),
	}
	if _, ok := s.conn.(*TCPUnderlay); ok {
		info.Protocol = "TCP"
	} else if _, ok := s.conn.(*UDPUnderlay); ok {
		info.Protocol = "UDP"
	} else {
		info.Protocol = "UNKNOWN"
	}
	return info
}

func (s *Session) isState(target sessionState) bool {
	s.sLock.Lock()
	defer s.sLock.Unlock()
	return s.state == target
}

func (s *Session) isStateBefore(target sessionState, include bool) bool {
	s.sLock.Lock()
	defer s.sLock.Unlock()
	if include {
		return s.state <= target
	} else {
		return s.state < target
	}
}

func (s *Session) isStateAfter(target sessionState, include bool) bool {
	s.sLock.Lock()
	defer s.sLock.Unlock()
	if include {
		return s.state >= target
	} else {
		return s.state > target
	}
}

func (s *Session) forwardStateTo(new sessionState) {
	s.sLock.Lock()
	defer s.sLock.Unlock()
	if new < s.state {
		panic(fmt.Sprintf("Can't move state back from %v to %v", s.state, new))
	}
	if log.IsLevelEnabled(log.TraceLevel) && s.state != new {
		log.Tracef("%v %v => %v", s, s.state, new)
	}
	s.state = new
}

func (s *Session) writeChunk(b []byte) (n int, err error) {
	if len(b) > maxPDU {
		return 0, io.ErrShortWrite
	}

	// Stop writing when deadline is reached.
	var timeC <-chan time.Time
	if !s.writeDeadline.IsZero() {
		timeC = time.After(time.Until(s.writeDeadline))
	}

	seqBeforeWrite, _ := s.sendQueue.MinSeq()

	// Determine number of fragments to write.
	nFragment := 1
	fragmentSize := MaxFragmentSize(s.mtu, s.conn.IPVersion(), s.conn.TransportProtocol())
	if len(b) > fragmentSize {
		nFragment = (len(b)-1)/fragmentSize + 1
	}

	ptr := b
	for i := nFragment - 1; i >= 0; i-- {
		select {
		case <-s.done:
			return 0, io.EOF
		case <-s.outputErr:
			return 0, io.ErrClosedPipe
		case <-timeC:
			return 0, stderror.ErrTimeout
		default:
		}
		var protocol uint8
		if s.isClient {
			protocol = uint8(dataClientToServer)
		} else {
			protocol = uint8(dataServerToClient)
		}
		partLen := mathext.Min(fragmentSize, len(ptr))
		part := ptr[:partLen]
		seg := &segment{
			metadata: &dataAckStruct{
				baseStruct: baseStruct{
					protocol: protocol,
				},
				sessionID:  s.id,
				seq:        s.nextSend,
				unAckSeq:   s.nextRecv,
				windowSize: uint16(mathext.Max(0, int(s.legacysendAlgorithm.CongestionWindowSize())-s.recvBuf.Len())),
				fragment:   uint8(i),
				payloadLen: uint16(partLen),
			},
			payload:   make([]byte, partLen),
			transport: s.conn.TransportProtocol(),
		}
		copy(seg.payload, part)
		s.nextSend++
		s.sendQueue.InsertBlocking(seg)
		ptr = ptr[partLen:]
	}

	// To create back pressure, wait until sendQueue is moving.
	for {
		shouldReturn := false
		select {
		case <-s.sendQueue.chanEmptyEvent:
			shouldReturn = true
		case <-s.sendQueue.chanNotEmptyEvent:
			seqAfterWrite, err := s.sendQueue.MinSeq()
			if err != nil || seqAfterWrite > seqBeforeWrite {
				shouldReturn = true
			}
		}
		if shouldReturn {
			break
		}
	}

	if s.isClient {
		s.readDeadline = time.Now().Add(serverRespTimeout)
	}
	return len(b), nil
}

func (s *Session) runInputLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-s.done:
			return nil
		case seg := <-s.recvChan:
			if err := s.input(seg); err != nil {
				err = fmt.Errorf("input() failed: %w", err)
				log.Debugf("%v %v", s, err)
				s.inputErr <- err
				s.Close()
				return err
			}
		}
	}
}

func (s *Session) runOutputLoop(ctx context.Context) error {
	ticker := time.NewTicker(outputLoopInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-s.done:
			return nil
		case <-ticker.C:
		case <-s.sendQueue.chanNotEmptyEvent:
		}

		switch s.conn.TransportProtocol() {
		case util.TCPTransport:
			for {
				seg, ok := s.sendQueue.DeleteMin()
				if !ok {
					break
				}
				if err := s.output(seg, nil); err != nil {
					err = fmt.Errorf("output() failed: %w", err)
					log.Debugf("%v %v", s, err)
					s.outputErr <- err
					s.Close()
					break
				}
			}
		case util.UDPTransport:
			closeSession := false
			hasLoss := false
			hasTimeout := false

			// Resend segments in sendBuf.
			// To avoid deadlock, session can't be closed inside Ascend().
			var bytesInFlight int64
			s.sendBuf.Ascend(func(iter *segment) bool {
				bytesInFlight += int64(udpOverhead + len(iter.payload))
				if iter.txCount >= txCountLimit {
					err := fmt.Errorf("too many retransmission of %v", iter)
					log.Debugf("%v is unhealthy: %v", s, err)
					s.outputErr <- err
					closeSession = true
					return false
				}
				if iter.ackCount >= earlyRetransmission || time.Since(iter.txTime) > iter.txTimeout {
					if iter.ackCount >= earlyRetransmission {
						hasLoss = true
					} else {
						hasTimeout = true
					}
					iter.ackCount = 0
					iter.txCount++
					iter.txTime = time.Now()
					iter.txTimeout = s.rttStat.RTO() * time.Duration(mathext.Min(math.Pow(txTimeoutBackOff, float64(iter.txCount)), maxBackOffMultiplier))
					if isDataAckProtocol(iter.metadata.Protocol()) {
						das, _ := toDataAckStruct(iter.metadata)
						das.unAckSeq = s.nextRecv
					}
					if err := s.output(iter, s.RemoteAddr()); err != nil {
						err = fmt.Errorf("output() failed: %w", err)
						log.Debugf("%v %v", s, err)
						s.outputErr <- err
						closeSession = true
						return false
					}
					return true
				}
				return true
			})
			if closeSession {
				s.Close()
			}
			if hasLoss || hasTimeout {
				s.legacysendAlgorithm.OnLoss() // OnTimeout() is too aggressive.
			}

			// Send new segments in sendQueue.
			if s.sendQueue.Len() > 0 {
				for {
					seg, deleted := s.sendQueue.DeleteMinIf(func(iter *segment) bool {
						return s.sendAlgorithm.CanSend(bytesInFlight)
					})
					if !deleted {
						break
					}
					seg.txCount++
					seg.txTime = time.Now()
					seg.txTimeout = s.rttStat.RTO() * time.Duration(mathext.Min(math.Pow(txTimeoutBackOff, float64(seg.txCount)), maxBackOffMultiplier))
					if isDataAckProtocol(seg.metadata.Protocol()) {
						das, _ := toDataAckStruct(seg.metadata)
						das.unAckSeq = s.nextRecv
					}
					s.sendBuf.InsertBlocking(seg)
					if err := s.output(seg, s.RemoteAddr()); err != nil {
						err = fmt.Errorf("output() failed: %w", err)
						log.Debugf("%v %v", s, err)
						s.outputErr <- err
						s.Close()
						break
					} else {
						seq, err := seg.Seq()
						if err != nil {
							err = fmt.Errorf("failed to get sequence number from %v: %w", seg, err)
							log.Debugf("%v %v", s, err)
							s.outputErr <- err
							s.Close()
							break
						}
						newBytesInFlight := int64(udpOverhead + len(seg.payload))
						s.sendAlgorithm.OnPacketSent(time.Now(), bytesInFlight, int64(seq), newBytesInFlight, true)
						bytesInFlight += newBytesInFlight
					}
				}
			}

			// Send ACK or heartbeat if needed.
			exceedHeartbeatInterval := time.Since(s.lastTXTime) > sessionHeartbeatInterval
			if s.ackOnDataRecv.Load() || exceedHeartbeatInterval {
				baseStruct := baseStruct{}
				if s.isClient {
					baseStruct.protocol = uint8(ackClientToServer)
				} else {
					baseStruct.protocol = uint8(ackServerToClient)
				}
				ackSeg := &segment{
					metadata: &dataAckStruct{
						baseStruct: baseStruct,
						sessionID:  s.id,
						seq:        uint32(mathext.Max(0, int(s.nextSend)-1)),
						unAckSeq:   s.nextRecv,
						windowSize: uint16(mathext.Max(0, int(s.legacysendAlgorithm.CongestionWindowSize())-s.recvBuf.Len())),
					},
					transport: s.conn.TransportProtocol(),
				}
				if err := s.output(ackSeg, s.RemoteAddr()); err != nil {
					err = fmt.Errorf("output() failed: %w", err)
					log.Debugf("%v %v", s, err)
					s.outputErr <- err
					s.Close()
				} else {
					seq, err := ackSeg.Seq()
					if err != nil {
						err = fmt.Errorf("failed to get sequence number from %v: %w", ackSeg, err)
						log.Debugf("%v %v", s, err)
						s.outputErr <- err
						s.Close()
					}
					newBytesInFlight := int64(udpOverhead + len(ackSeg.payload))
					s.sendAlgorithm.OnPacketSent(time.Now(), bytesInFlight, int64(seq), newBytesInFlight, true)
					bytesInFlight += newBytesInFlight
				}
				s.ackOnDataRecv.Store(false)
			}
		default:
			err := fmt.Errorf("unsupported transport protocol %v", s.conn.TransportProtocol())
			log.Debugf("%v %v", s, err)
			s.outputErr <- err
			s.Close()
		}
	}
}

// input reads incoming packets from network and assemble
// them in the receive buffer and receive queue.
func (s *Session) input(seg *segment) error {
	protocol := seg.Protocol()
	if s.isClient {
		if protocol != openSessionResponse && protocol != dataServerToClient && protocol != ackServerToClient && protocol != closeSessionRequest && protocol != closeSessionResponse {
			return stderror.ErrInvalidArgument
		}
	} else {
		if protocol != openSessionRequest && protocol != dataClientToServer && protocol != ackClientToServer && protocol != closeSessionRequest && protocol != closeSessionResponse {
			return stderror.ErrInvalidArgument
		}
	}
	if seg.block != nil {
		s.block = seg.block
		if !s.isClient {
			if s.uploadBytes == nil && s.block.BlockContext().UserName != "" {
				s.uploadBytes = metrics.RegisterMetric(fmt.Sprintf(metrics.UserMetricGroupFormat, s.block.BlockContext().UserName), metrics.UserMetricUploadBytes, metrics.COUNTER_TIME_SERIES)
			}
			if s.downloadBytes == nil && s.block.BlockContext().UserName != "" {
				s.downloadBytes = metrics.RegisterMetric(fmt.Sprintf(metrics.UserMetricGroupFormat, s.block.BlockContext().UserName), metrics.UserMetricDownloadBytes, metrics.COUNTER_TIME_SERIES)
			}
		}
	}
	s.lastRXTime = time.Now()
	if protocol == openSessionRequest || protocol == openSessionResponse || protocol == dataServerToClient || protocol == dataClientToServer {
		return s.inputData(seg)
	} else if protocol == ackServerToClient || protocol == ackClientToServer {
		return s.inputAck(seg)
	} else if protocol == closeSessionRequest || protocol == closeSessionResponse {
		return s.inputClose(seg)
	}
	return nil
}

func (s *Session) inputData(seg *segment) error {
	switch s.conn.TransportProtocol() {
	case util.TCPTransport:
		// Deliver the segment directly to recvQueue.
		s.recvQueue.InsertBlocking(seg)
	case util.UDPTransport:
		// Delete all previous acknowledged segments from sendBuf.
		var priorInFlight int64
		s.sendBuf.Ascend(func(iter *segment) bool {
			priorInFlight += int64(udpOverhead + len(iter.payload))
			return true
		})
		das, ok := seg.metadata.(*dataAckStruct)
		if ok {
			unAckSeq := das.unAckSeq
			ackedPackets := make([]congestion.AckedPacketInfo, 0)
			for {
				seg2, deleted := s.sendBuf.DeleteMinIf(func(iter *segment) bool {
					seq, _ := iter.Seq()
					return seq < unAckSeq
				})
				if !deleted {
					break
				}
				s.rttStat.UpdateRTT(time.Since(seg2.txTime))
				s.legacysendAlgorithm.OnAck()
				seq, _ := seg2.Seq()
				ackedPackets = append(ackedPackets, congestion.AckedPacketInfo{
					PacketNumber:     int64(seq),
					BytesAcked:       int64(udpOverhead + len(seg2.payload)),
					ReceiveTimestamp: time.Now(),
				})
			}
			if len(ackedPackets) > 0 {
				s.sendAlgorithm.OnCongestionEvent(priorInFlight, time.Now(), ackedPackets, nil)
			}
			s.remoteWindowSize = das.windowSize
		}

		// Deliver the segment to recvBuf.
		s.recvBuf.InsertBlocking(seg)

		// Move recvBuf to recvQueue.
		for {
			seg3, deleted := s.recvBuf.DeleteMinIf(func(iter *segment) bool {
				seq, _ := iter.Seq()
				return seq <= s.nextRecv
			})
			if seg3 == nil || !deleted {
				break
			}
			seq, _ := seg3.Seq()
			if seq == s.nextRecv {
				s.recvQueue.InsertBlocking(seg3)
				s.nextRecv++
				das, ok := seg3.metadata.(*dataAckStruct)
				if ok {
					s.remoteWindowSize = das.windowSize
				}
			}
		}
		s.ackOnDataRecv.Store(true)
	default:
		return fmt.Errorf("unsupported transport protocol %v", s.conn.TransportProtocol())
	}

	if !s.isClient && seg.metadata.Protocol() == openSessionRequest {
		s.wLock.Lock()
		if s.isState(sessionAttached) {
			// Server needs to send open session response.
			// Check user quota if we can identify the user.
			var userName string
			if seg.block != nil && seg.block.BlockContext().UserName != "" {
				userName = seg.block.BlockContext().UserName
			} else if s.block != nil && s.block.BlockContext().UserName != "" {
				userName = s.block.BlockContext().UserName
			}
			if userName != "" {
				quotaOK, err := s.checkQuota(userName)
				if err != nil {
					log.Debugf("%v checkQuota() failed: %v", s, err)
				}
				if !quotaOK {
					s.status = statusQuotaExhausted
					log.Debugf("Closing %v because user %s used all the quota", s, userName)
					s.wLock.Unlock()
					s.Close()
					return nil
				}
			}
			seg4 := &segment{
				metadata: &sessionStruct{
					baseStruct: baseStruct{
						protocol: uint8(openSessionResponse),
					},
					sessionID: s.id,
					seq:       s.nextSend,
				},
				transport: s.conn.TransportProtocol(),
			}
			s.nextSend++
			if log.IsLevelEnabled(log.TraceLevel) {
				log.Tracef("%v writing open session response", s)
			}
			s.sendQueue.InsertBlocking(seg4)
			s.forwardStateTo(sessionEstablished)
		}
		s.wLock.Unlock()
	}
	return nil
}

func (s *Session) inputAck(seg *segment) error {
	switch s.conn.TransportProtocol() {
	case util.TCPTransport:
		// Do nothing when receive ACK from TCP protocol.
		return nil
	case util.UDPTransport:
		// Delete all previous acknowledged segments from sendBuf.
		var priorInFlight int64
		s.sendBuf.Ascend(func(iter *segment) bool {
			priorInFlight += int64(udpOverhead + len(iter.payload))
			return true
		})
		das := seg.metadata.(*dataAckStruct)
		unAckSeq := das.unAckSeq
		ackedPackets := make([]congestion.AckedPacketInfo, 0)
		for {
			seg2, deleted := s.sendBuf.DeleteMinIf(func(iter *segment) bool {
				seq, _ := iter.Seq()
				return seq < unAckSeq
			})
			if !deleted {
				break
			}
			s.rttStat.UpdateRTT(time.Since(seg2.txTime))
			s.legacysendAlgorithm.OnAck()
			seq, _ := seg2.Seq()
			ackedPackets = append(ackedPackets, congestion.AckedPacketInfo{
				PacketNumber:     int64(seq),
				BytesAcked:       int64(udpOverhead + len(seg2.payload)),
				ReceiveTimestamp: time.Now(),
			})
		}
		if len(ackedPackets) > 0 {
			s.sendAlgorithm.OnCongestionEvent(priorInFlight, time.Now(), ackedPackets, nil)
		}
		s.remoteWindowSize = das.windowSize

		// Update acknowledge count.
		s.sendBuf.Ascend(func(iter *segment) bool {
			seq, _ := iter.Seq()
			if seq > unAckSeq {
				return false
			}
			if seq == unAckSeq {
				iter.ackCount++
			}
			return true
		})
		return nil
	default:
		return fmt.Errorf("unsupported transport protocol %v", s.conn.TransportProtocol())
	}
}

func (s *Session) inputClose(seg *segment) error {
	s.wLock.Lock()
	if seg.metadata.Protocol() == closeSessionRequest {
		// Send close session response.
		seg2 := &segment{
			metadata: &sessionStruct{
				baseStruct: baseStruct{
					protocol: uint8(closeSessionResponse),
				},
				sessionID:  s.id,
				seq:        s.nextSend,
				statusCode: uint8(statusOK),
				payloadLen: 0,
			},
			transport: s.conn.TransportProtocol(),
		}
		s.nextSend++
		// The response will not retry if it is not delivered.
		if err := s.output(seg2, s.RemoteAddr()); err != nil {
			s.wLock.Unlock()
			return fmt.Errorf("output() failed: %v", err)
		}
		// Immediately shutdown event loop.
		if seg.metadata.(*sessionStruct).statusCode == uint8(statusQuotaExhausted) {
			log.Infof("Remote requested to shut down the session because user has exhausted quota")
		} else {
			log.Debugf("Remote requested to shut down %v", s)
		}
		s.wLock.Unlock()
		s.Close()
	} else if seg.metadata.Protocol() == closeSessionResponse {
		// Immediately shutdown event loop.
		log.Debugf("Remote received the request from %v to shut down", s)
		s.wLock.Unlock()
		s.Close()
	}
	return nil
}

func (s *Session) output(seg *segment, remoteAddr net.Addr) error {
	switch s.conn.TransportProtocol() {
	case util.TCPTransport:
		if err := s.conn.(*TCPUnderlay).writeOneSegment(seg); err != nil {
			return fmt.Errorf("TCPUnderlay.writeOneSegment() failed: %v", err)
		}
	case util.UDPTransport:
		err := s.conn.(*UDPUnderlay).writeOneSegment(seg, remoteAddr.(*net.UDPAddr))
		if err != nil {
			if !stderror.IsNotReady(err) {
				return fmt.Errorf("UDPUnderlay.writeOneSegment() failed: %v", err)
			}
			if log.IsLevelEnabled(log.TraceLevel) {
				log.Tracef("UDPUnderlay.writeOneSegment() failed: %v. Will retry later.", err)
			}
			return nil
		}
	default:
		return fmt.Errorf("unsupported transport protocol %v", s.conn.TransportProtocol())
	}
	s.lastTXTime = time.Now()
	return nil
}

func (s *Session) checkQuota(userName string) (ok bool, err error) {
	if len(s.users) == 0 {
		return true, fmt.Errorf("no registered user")
	}
	user, found := s.users[userName]
	if !found {
		return true, fmt.Errorf("user %s is not found", userName)
	}
	if len(user.GetQuotas()) == 0 {
		return true, nil
	}

	metricGroupName := fmt.Sprintf(metrics.UserMetricGroupFormat, userName)
	metricGroup := metrics.GetMetricGroupByName(metricGroupName)
	if metricGroup == nil {
		return true, fmt.Errorf("metric group %s is not found", metricGroupName)
	}
	uploadBytes, found := metricGroup.GetMetric(metrics.UserMetricUploadBytes)
	if !found {
		return true, fmt.Errorf("metric %s in group %s is not found", metrics.UserMetricUploadBytes, metricGroupName)
	}
	downloadBytes, found := metricGroup.GetMetric(metrics.UserMetricDownloadBytes)
	if !found {
		return true, fmt.Errorf("metric %s in group %s is not found", metrics.UserMetricDownloadBytes, metricGroupName)
	}
	for _, quota := range user.GetQuotas() {
		now := time.Now()
		then := now.Add(-time.Duration(quota.GetDays()) * 24 * time.Hour)
		totalBytes := uploadBytes.(*metrics.Counter).DeltaBetween(then, now)
		totalBytes += downloadBytes.(*metrics.Counter).DeltaBetween(then, now)
		if totalBytes/1048576 > int64(quota.GetMegabytes()) {
			return false, nil
		}
	}
	return true, nil
}

// SessionInfo provides a string representation of a Session.
type SessionInfo struct {
	ID         string
	Protocol   string
	LocalAddr  string
	RemoteAddr string
	State      string
	RecvQBuf   string
	SendQBuf   string
	LastRecv   string
	LastSend   string
}
