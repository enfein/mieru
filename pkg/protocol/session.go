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

package protocol

import (
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/congestion"
	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/mathext"
	"github.com/enfein/mieru/v3/pkg/metrics"
	"github.com/enfein/mieru/v3/pkg/stderror"
)

const (
	segmentTreeCapacity = 4096
	segmentChanCapacity = 1024

	minWindowSize = 16
	maxWindowSize = segmentTreeCapacity

	serverRespTimeout        = 10 * time.Second
	sessionHeartbeatInterval = 5 * time.Second

	earlyRetransmission      = 3 // number of ack to trigger early retransmission
	earlyRetransmissionLimit = 5 // maximum number of early retransmission attempt
	txTimeoutBackOff         = 1.25
	maxBackOffMultiplier     = 20.0
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
	conn  Underlay                           // underlay connection
	block atomic.Pointer[cipher.BlockCipher] // cipher to decrypt data

	id         uint32                    // session ID number
	isClient   bool                      // if this session is owned by client
	mtu        int                       // L2 maxinum transmission unit
	remoteAddr net.Addr                  // specify remote network address, used by UDP
	state      sessionState              // session state
	status     statusCode                // session status
	users      map[string]*appctlpb.User // all registered users

	ready          chan struct{} // indicate the session is ready to use
	closeRequested atomic.Bool   // the session is being closed or has been closed
	closedChan     chan struct{} // indicate the session is closed
	readDeadline   time.Time     // read deadline
	writeDeadline  time.Time     // write deadline
	inputErr       chan error    // input error
	outputErr      chan error    // output error

	sendQueue *segmentTree  // segments waiting to send
	sendBuf   *segmentTree  // segments sent but not acknowledged
	recvBuf   *segmentTree  // segments received but acknowledge is not sent
	recvQueue *segmentTree  // segments waiting to be read by application
	recvChan  chan *segment // channel to receive segments from underlay

	nextSend      uint32      // next sequence number to send a segment
	nextRecv      uint32      // next sequence number to receive
	lastSend      uint32      // last segment sequence number sent
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
	rLock sync.Mutex // serialize read from application
	oLock sync.Mutex // serialize the output sequence
	sLock sync.Mutex // serialize the state transition
}

// Session must implement net.Conn interface.
var _ net.Conn = &Session{}

// NewSession creates a new session.
func NewSession(id uint32, isClient bool, mtu int, users map[string]*appctlpb.User) *Session {
	rttStat := congestion.NewRTTStats()
	rttStat.SetMaxAckDelay(tickInterval)
	rttStat.SetRTOMultiplier(txTimeoutBackOff)
	return &Session{
		conn:                nil,
		block:               atomic.Pointer[cipher.BlockCipher]{},
		id:                  id,
		isClient:            isClient,
		mtu:                 mtu,
		state:               sessionInit,
		status:              statusOK,
		users:               users,
		ready:               make(chan struct{}),
		closedChan:          make(chan struct{}),
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
// Read is allowed even after the session has been closed.
func (s *Session) Read(b []byte) (n int, err error) {
	s.rLock.Lock()
	defer s.rLock.Unlock()
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
			case <-s.closedChan:
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
	if s.closeRequested.Load() {
		return 0, io.ErrClosedPipe
	}

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
		s.oLock.Lock()
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
		if !s.sendQueue.Insert(seg) {
			s.oLock.Unlock()
			return 0, fmt.Errorf("insert %v to send queue failed", seg)
		} else {
			s.oLock.Unlock()
		}
		if len(seg.payload) > 0 {
			return len(seg.payload), nil
		}
	}

	if log.IsLevelEnabled(log.TraceLevel) {
		log.Tracef("%v writing %d bytes", s, n)
	}
	for len(b) > 0 {
		sizeToSend := mathext.Min(len(b), maxPDU)
		if sent, err := s.writeChunk(b[:sizeToSend]); sent == 0 || err != nil {
			return n, err
		}
		b = b[sizeToSend:]
		n += sizeToSend
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
	return s.closeWithError(nil)
}

func (s *Session) LocalAddr() net.Addr {
	return s.conn.LocalAddr()
}

func (s *Session) RemoteAddr() net.Addr {
	if !common.IsNilNetAddr(s.remoteAddr) {
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
		LastSend:   fmt.Sprintf("%v (%d)", time.Since(s.lastTXTime).Truncate(time.Second), s.nextSend-1),
	}
	if _, ok := s.conn.(*StreamUnderlay); ok {
		info.Protocol = "TCP"
		info.LastRecv = fmt.Sprintf("%v", time.Since(s.lastRXTime).Truncate(time.Second)) // TCP nextRecv is not used
	} else if _, ok := s.conn.(*PacketUnderlay); ok {
		info.Protocol = "UDP"
		info.LastRecv = fmt.Sprintf("%v (%d)", time.Since(s.lastRXTime).Truncate(time.Second), s.nextRecv-1)
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
	// Return if there is no enough space in send queue.
	nFragment := 1
	fragmentSize := MaxFragmentSize(s.mtu, s.conn.TransportProtocol())
	if len(b) > fragmentSize {
		nFragment = (len(b)-1)/fragmentSize + 1
	}
	if s.sendQueue.Remaining() <= nFragment { // leave one slot in case it is needed
		time.Sleep(tickInterval) // add back pressure
		return 0, nil
	}

	s.oLock.Lock()
	ptr := b
	for i := nFragment - 1; i >= 0; i-- {
		select {
		case <-s.closedChan:
			s.oLock.Unlock()
			return 0, io.EOF
		case <-s.outputErr:
			s.oLock.Unlock()
			return 0, io.ErrClosedPipe
		case <-timeC:
			s.oLock.Unlock()
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
		if !s.sendQueue.Insert(seg) {
			s.oLock.Unlock()
			return 0, fmt.Errorf("insert %v to send queue failed", seg)
		}
		ptr = ptr[partLen:]
	}
	s.oLock.Unlock()

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
		case <-s.closedChan:
			return nil
		case seg := <-s.recvChan:
			if err := s.input(seg); err != nil {
				err = fmt.Errorf("input() failed: %w", err)
				log.Debugf("%v %v", s, err)
				s.inputErr <- err
				s.closeWithError(err)
				return err
			}
		}
	}
}

func (s *Session) runOutputLoop(ctx context.Context) error {
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-s.closedChan:
			return nil
		case <-ticker.C:
		case <-s.sendQueue.chanNotEmptyEvent:
		}

		switch s.conn.TransportProtocol() {
		case common.StreamTransport:
			s.runOutputOnceStream()
		case common.PacketTransport:
			s.runOutputOncePacket()
		default:
			err := fmt.Errorf("unsupported transport protocol %v", s.conn.TransportProtocol())
			log.Debugf("%v %v", s, err)
			s.outputErr <- err
			s.closeWithError(err)
		}
	}
}

func (s *Session) runOutputOnceStream() {
	s.oLock.Lock()
	defer s.oLock.Unlock()

	for {
		seg, ok := s.sendQueue.DeleteMin()
		if !ok {
			break
		}
		if err := s.output(seg, nil); err != nil {
			err = fmt.Errorf("output() failed: %w", err)
			log.Debugf("%v %v", s, err)
			s.outputErr <- err
			s.closeWithError(err)
			break
		}
	}
}

func (s *Session) runOutputOncePacket() {
	var closeSessionReason error
	hasLoss := false
	hasTimeout := false
	var bytesInFlight int64

	// Resend segments in sendBuf.
	// To avoid deadlock, session can't be closed inside Ascend().
	s.oLock.Lock()
	s.sendBuf.Ascend(func(iter *segment) bool {
		bytesInFlight += int64(packetOverhead + len(iter.payload))
		if iter.txCount >= txCountLimit {
			err := fmt.Errorf("too many retransmission of %v", iter)
			log.Debugf("%v is unhealthy: %v", s, err)
			s.outputErr <- err
			closeSessionReason = err
			return false
		}
		if (iter.ackCount >= earlyRetransmission && iter.txCount <= earlyRetransmissionLimit) || time.Since(iter.txTime) > iter.txTimeout {
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
				closeSessionReason = err
				return false
			}
			bytesInFlight += int64(packetOverhead + len(iter.payload))
			return true
		}
		return true
	})
	s.oLock.Unlock()
	if closeSessionReason != nil {
		s.closeWithError(closeSessionReason)
	}
	if hasLoss || hasTimeout {
		s.legacysendAlgorithm.OnLoss() // OnTimeout() is too aggressive.
	}

	// Send new segments in sendQueue.
	if s.sendQueue.Len() > 0 {
		s.oLock.Lock()
		for {
			seg, deleted := s.sendQueue.DeleteMinIf(func(iter *segment) bool {
				return s.sendAlgorithm.CanSend(bytesInFlight, int64(packetOverhead+len(iter.payload)))
			})
			if !deleted {
				s.oLock.Unlock()
				break
			}

			seg.txCount++
			seg.txTime = time.Now()
			seg.txTimeout = s.rttStat.RTO() * time.Duration(mathext.Min(math.Pow(txTimeoutBackOff, float64(seg.txCount)), maxBackOffMultiplier))
			if isDataAckProtocol(seg.metadata.Protocol()) {
				das, _ := toDataAckStruct(seg.metadata)
				das.unAckSeq = s.nextRecv
			}
			if !s.sendBuf.Insert(seg) {
				s.oLock.Unlock()
				err := fmt.Errorf("output() failed: insert %v to send buffer failed", seg)
				log.Debugf("%v %v", s, err)
				s.outputErr <- err
				s.closeWithError(err)
				break
			}
			if err := s.output(seg, s.RemoteAddr()); err != nil {
				s.oLock.Unlock()
				err = fmt.Errorf("output() failed: %w", err)
				log.Debugf("%v %v", s, err)
				s.outputErr <- err
				s.closeWithError(err)
				break
			} else {
				seq, err := seg.Seq()
				if err != nil {
					s.oLock.Unlock()
					err = fmt.Errorf("failed to get sequence number from %v: %w", seg, err)
					log.Debugf("%v %v", s, err)
					s.outputErr <- err
					s.closeWithError(err)
					break
				}
				newBytesInFlight := int64(packetOverhead + len(seg.payload))
				s.sendAlgorithm.OnPacketSent(time.Now(), bytesInFlight, int64(seq), newBytesInFlight, true)
				bytesInFlight += newBytesInFlight
			}
		}
	} else {
		s.sendAlgorithm.OnApplicationLimited(bytesInFlight)
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
		s.oLock.Lock()
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
			s.oLock.Unlock()
			err = fmt.Errorf("output() failed: %w", err)
			log.Debugf("%v %v", s, err)
			s.outputErr <- err
			s.closeWithError(err)
		} else {
			seq, err := ackSeg.Seq()
			if err != nil {
				s.oLock.Unlock()
				err = fmt.Errorf("failed to get sequence number from %v: %w", ackSeg, err)
				log.Debugf("%v %v", s, err)
				s.outputErr <- err
				s.closeWithError(err)
			} else {
				s.oLock.Unlock()
				newBytesInFlight := int64(packetOverhead + len(ackSeg.payload))
				s.sendAlgorithm.OnPacketSent(time.Now(), bytesInFlight, int64(seq), newBytesInFlight, true)
				bytesInFlight += newBytesInFlight
			}
		}
		s.ackOnDataRecv.Store(false)
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
		// Validate cipher block user name is consistent.
		if s.block.Load() != nil {
			prevUserName := (*s.block.Load()).BlockContext().UserName
			nextUserName := seg.block.BlockContext().UserName
			if prevUserName == "" {
				panic(fmt.Sprintf("%v cipher block user name is not set", s))
			}
			if nextUserName == "" {
				panic(fmt.Sprintf("%v cipher block user name is not set", seg))
			}
			if prevUserName != nextUserName {
				panic(fmt.Sprintf("%v cipher block user name %q is different from %v cipher block user name %q", s, prevUserName, seg, nextUserName))
			}
		}

		s.block.Store(&seg.block)

		// Register server per user metrics.
		if !s.isClient {
			if s.uploadBytes == nil {
				s.uploadBytes = metrics.RegisterMetric(fmt.Sprintf(metrics.UserMetricGroupFormat, (*s.block.Load()).BlockContext().UserName), metrics.UserMetricUploadBytes, metrics.COUNTER_TIME_SERIES)
			}
			if s.downloadBytes == nil {
				s.downloadBytes = metrics.RegisterMetric(fmt.Sprintf(metrics.UserMetricGroupFormat, (*s.block.Load()).BlockContext().UserName), metrics.UserMetricDownloadBytes, metrics.COUNTER_TIME_SERIES)
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
	case common.StreamTransport:
		// Deliver the segment directly to recvQueue.
		if !s.recvQueue.Insert(seg) {
			return fmt.Errorf("insert %v to receive queue failed", seg)
		}
	case common.PacketTransport:
		// Delete all previous acknowledged segments from sendBuf.
		var priorInFlight int64
		s.sendBuf.Ascend(func(iter *segment) bool {
			priorInFlight += int64(packetOverhead + len(iter.payload))
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
					BytesAcked:       int64(packetOverhead + len(seg2.payload)),
					ReceiveTimestamp: time.Now(),
				})
			}
			if len(ackedPackets) > 0 {
				s.sendAlgorithm.OnCongestionEvent(priorInFlight, time.Now(), ackedPackets, nil)
			}
			s.remoteWindowSize = das.windowSize
		}

		// Deliver the segment to recvBuf.
		if !s.recvBuf.Insert(seg) {
			return fmt.Errorf("insert %v to receive buffer failed", seg)
		}

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
				if !s.recvQueue.Insert(seg3) {
					return fmt.Errorf("insert %v to receive queue failed", seg3)
				}
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
		if s.isState(sessionAttached) {
			// Server needs to send open session response.
			// Check user quota if we can identify the user.
			s.oLock.Lock()
			var userName string
			if seg.block != nil && seg.block.BlockContext().UserName != "" {
				userName = seg.block.BlockContext().UserName
			} else if s.block.Load() != nil && (*s.block.Load()).BlockContext().UserName != "" {
				userName = (*s.block.Load()).BlockContext().UserName
			}
			if userName != "" {
				quotaOK, err := s.checkQuota(userName)
				if err != nil {
					log.Debugf("%v checkQuota() failed: %v", s, err)
				}
				if !quotaOK {
					s.status = statusQuotaExhausted
					log.Debugf("Closing %v because user %s used all the quota", s, userName)
					s.oLock.Unlock()
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
			if !s.sendQueue.Insert(seg4) {
				s.oLock.Unlock()
				return fmt.Errorf("insert %v to send queue failed", seg4)
			} else {
				s.oLock.Unlock()
				s.forwardStateTo(sessionEstablished)
			}
		}
	}
	return nil
}

func (s *Session) inputAck(seg *segment) error {
	switch s.conn.TransportProtocol() {
	case common.StreamTransport:
		// Do nothing when receive ACK from TCP protocol.
		return nil
	case common.PacketTransport:
		// Delete all previous acknowledged segments from sendBuf.
		var priorInFlight int64
		s.sendBuf.Ascend(func(iter *segment) bool {
			priorInFlight += int64(packetOverhead + len(iter.payload))
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
				BytesAcked:       int64(packetOverhead + len(seg2.payload)),
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
	s.oLock.Lock()
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
			s.oLock.Unlock()
			return fmt.Errorf("output() failed: %v", err)
		}
		// Immediately shutdown event loop.
		if seg.metadata.(*sessionStruct).statusCode == uint8(statusQuotaExhausted) {
			log.Infof("Remote requested to shut down the session because user has exhausted quota")
		} else {
			log.Debugf("Remote requested to shut down %v", s)
		}
		s.oLock.Unlock()
		s.Close()
	} else if seg.metadata.Protocol() == closeSessionResponse {
		// Immediately shutdown event loop.
		log.Debugf("Remote received the request from %v to shut down", s)
		s.oLock.Unlock()
		s.Close()
	}
	return nil
}

func (s *Session) output(seg *segment, remoteAddr net.Addr) error {
	switch s.conn.TransportProtocol() {
	case common.StreamTransport:
		if err := s.conn.(*StreamUnderlay).writeOneSegment(seg); err != nil {
			return fmt.Errorf("TCPUnderlay.writeOneSegment() failed: %v", err)
		}
	case common.PacketTransport:
		err := s.conn.(*PacketUnderlay).writeOneSegment(seg, remoteAddr)
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
	s.lastSend, _ = seg.Seq()
	s.lastTXTime = time.Now()
	return nil
}

func (s *Session) closeWithError(err error) error {
	if !s.closeRequested.CompareAndSwap(false, true) {
		// This function has been called before.
		return nil
	}

	var gracefulClose bool
	if err == nil {
		log.Debugf("Closing %v", s)
		gracefulClose = true
	} else {
		log.Debugf("Closing %v with error %v", s, err)
	}
	if s.isState(sessionAttached) || s.isState(sessionEstablished) {
		// Send closeSessionRequest, but don't wait for closeSessionResponse,
		// because the underlay connection may be already broken.
		s.oLock.Lock()
		closeRequestSeq := s.nextSend
		seg := &segment{
			metadata: &sessionStruct{
				baseStruct: baseStruct{
					protocol: uint8(closeSessionRequest),
				},
				sessionID:  s.id,
				seq:        closeRequestSeq,
				statusCode: uint8(s.status),
			},
			transport: s.conn.TransportProtocol(),
		}
		s.nextSend++

		var gracefulCloseSuccess bool
		if gracefulClose {
			if !s.sendQueue.Insert(seg) {
				s.oLock.Unlock()
			} else {
				s.oLock.Unlock()
				for i := 0; i < 1000; i++ {
					time.Sleep(time.Millisecond)
					if s.lastSend >= closeRequestSeq {
						gracefulCloseSuccess = true
						break
					}
				}
			}
		} else {
			s.oLock.Unlock()
		}
		if !gracefulCloseSuccess {
			s.oLock.Lock()
			if err := s.output(seg, s.RemoteAddr()); err != nil {
				log.Debugf("output() failed: %v", err)
			}
			s.oLock.Unlock()
		}
	}

	// Don't clear receive buf and queue, because read is allowed after
	// the session is closed.
	s.sendQueue.DeleteAll()
	s.sendBuf.DeleteAll()
	s.forwardStateTo(sessionClosed)
	close(s.closedChan)
	log.Debugf("Closed %v", s)
	metrics.CurrEstablished.Add(-1)
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
