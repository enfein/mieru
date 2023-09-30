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
	"time"

	"github.com/enfein/mieru/pkg/cipher"
	"github.com/enfein/mieru/pkg/congestion"
	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/mathext"
	"github.com/enfein/mieru/pkg/metrics"
	"github.com/enfein/mieru/pkg/stderror"
	"github.com/enfein/mieru/pkg/util"
)

const (
	segmentTreeCapacity = 64 * 1024
	segmentChanCapacity = 1024

	minWindowSize = 16
	maxWindowSize = 64 * 1024

	segmentAckDelay = 50 * time.Millisecond

	serverRespTimeout        = 10 * time.Second
	sessionHeartbeatInterval = 5 * time.Second

	txTimeoutBackOff = 1.2
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
		return "sessionInit"
	case sessionAttached:
		return "sessionAttached"
	case sessionEstablished:
		return "sessionEstablished"
	case sessionClosed:
		return "sessionClosed"
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

	nextSend   uint32    // next sequence number to send a segment
	nextRecv   uint32    // next sequence number to receive
	lastRXTime time.Time // last timestamp when a segment is received
	lastTXTime time.Time // last timestamp when a segment is sent
	unreadBuf  []byte    // payload removed from the recvQueue that haven't been read by application

	readBytes  *metrics.Metric // number of bytes delivered to the application
	writeBytes *metrics.Metric // number of bytes sent from the application

	rttStat          *congestion.RTTStats
	sendAlgorithm    *congestion.CubicSendAlgorithm
	remoteWindowSize uint16

	wg    sync.WaitGroup
	rLock sync.Mutex
	wLock sync.Mutex
	sLock sync.Mutex
}

// Session must implement net.Conn interface.
var _ net.Conn = &Session{}

// NewSession creates a new session.
func NewSession(id uint32, isClient bool, mtu int) *Session {
	rttStat := congestion.NewRTTStats()
	rttStat.SetMaxAckDelay(segmentAckDelay)
	rttStat.SetRTOMultiplier(1.5)
	return &Session{
		conn:             nil,
		block:            nil,
		id:               id,
		isClient:         isClient,
		mtu:              mtu,
		state:            sessionInit,
		ready:            make(chan struct{}),
		done:             make(chan struct{}),
		readDeadline:     util.ZeroTime(),
		writeDeadline:    util.ZeroTime(),
		inputErr:         make(chan error, 2), // allow nested
		outputErr:        make(chan error, 2), // allow nested
		sendQueue:        newSegmentTree(segmentTreeCapacity),
		sendBuf:          newSegmentTree(segmentTreeCapacity),
		recvBuf:          newSegmentTree(segmentTreeCapacity),
		recvQueue:        newSegmentTree(segmentTreeCapacity),
		recvChan:         make(chan *segment, segmentChanCapacity),
		lastRXTime:       time.Now(),
		lastTXTime:       time.Now(),
		rttStat:          rttStat,
		sendAlgorithm:    congestion.NewCubicSendAlgorithm(minWindowSize, maxWindowSize),
		remoteWindowSize: minWindowSize,
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
		s.readDeadline = util.ZeroTime()
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
		if s.readBytes != nil {
			s.readBytes.Add(int64(n))
		}
		return n, nil
	}

	// Stop reading when deadline is reached.
	var timeC <-chan time.Time
	if !util.IsZeroTime(s.readDeadline) {
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
	if s.readBytes != nil {
		s.readBytes.Add(int64(n))
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
		s.writeDeadline = util.ZeroTime()
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
	if s.writeBytes != nil {
		s.writeBytes.Add(int64(n))
	}
	return n, nil
}

// Close terminates the session.
func (s *Session) Close() error {
	s.wLock.Lock()
	defer s.wLock.Unlock()
	select {
	case <-s.done:
		s.forwardStateTo(sessionClosed)
		return nil
	default:
	}

	log.Debugf("Closing %v", s)
	if s.isState(sessionEstablished) {
		// Send closeSessionRequest, but don't wait for closeSessionResponse,
		// because the underlay connection may be already broken.
		// The closeSessionRequest won't be sent again.
		seg := &segment{
			metadata: &sessionStruct{
				baseStruct: baseStruct{
					protocol: uint8(closeSessionRequest),
				},
				sessionID: s.id,
				seq:       s.nextSend,
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
	s.forwardStateTo(sessionClosed)
	close(s.done)
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
	if !util.IsZeroTime(s.writeDeadline) {
		timeC = time.After(time.Until(s.writeDeadline))
	}

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
				windowSize: uint16(mathext.Max(0, int(s.sendAlgorithm.CongestionWindowSize())-s.recvBuf.Len())),
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
	ticker := time.NewTicker(10 * time.Millisecond)
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
			hasTimeout := false

			// Resend segments in sendBuf.
			s.sendBuf.Ascend(func(iter *segment) bool {
				if iter.txCount >= txCountLimit {
					err := fmt.Errorf("too many retransmission of %v", iter)
					log.Debugf("%v is unhealthy: %v", s, err)
					s.outputErr <- err
					s.Close()
					return false
				}
				if time.Since(iter.txTime) > iter.txTimeout {
					hasTimeout = true
					iter.txCount++
					iter.txTime = time.Now()
					iter.txTimeout = s.rttStat.RTO() * time.Duration(math.Pow(txTimeoutBackOff, float64(iter.txCount)))
					if err := s.output(iter, s.RemoteAddr()); err != nil {
						err = fmt.Errorf("output() failed: %w", err)
						log.Debugf("%v %v", s, err)
						s.outputErr <- err
						s.Close()
						return false
					}
					return true
				}
				return true
			})
			if hasTimeout {
				s.sendAlgorithm.OnTimeout()
			}

			// Send new segments in sendQueue.
			segmentMoved := 0
			if s.sendQueue.Len() > 0 {
				maxSegmentToMove := mathext.Min(s.sendQueue.Len(), s.sendBuf.Remaining())
				maxSegmentToMove = mathext.Min(maxSegmentToMove, int(s.sendAlgorithm.CongestionWindowSize()))
				maxSegmentToMove = mathext.Min(maxSegmentToMove, int(s.remoteWindowSize))
				for {
					seg, deleted := s.sendQueue.DeleteMinIf(func(iter *segment) bool {
						if segmentMoved >= maxSegmentToMove {
							return false
						}
						segmentMoved++
						return true
					})
					if !deleted {
						break
					}
					seg.txCount++
					seg.txTime = time.Now()
					seg.txTimeout = s.rttStat.RTO() * time.Duration(math.Pow(txTimeoutBackOff, float64(seg.txCount)))
					s.sendBuf.InsertBlocking(seg)
					if err := s.output(seg, s.RemoteAddr()); err != nil {
						err = fmt.Errorf("output() failed: %w", err)
						log.Debugf("%v %v", s, err)
						s.outputErr <- err
						s.Close()
						break
					}
				}
			}

			// Send ACK or heartbeat if needed.
			if !hasTimeout && segmentMoved == 0 {
				if (s.recvBuf.Len() > 0 && time.Since(s.lastTXTime) > segmentAckDelay) || time.Since(s.lastTXTime) > sessionHeartbeatInterval {
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
							windowSize: uint16(mathext.Max(0, int(s.sendAlgorithm.CongestionWindowSize())-s.recvBuf.Len())),
						},
						transport: s.conn.TransportProtocol(),
					}
					if err := s.output(ackSeg, s.RemoteAddr()); err != nil {
						err = fmt.Errorf("output() failed: %w", err)
						log.Debugf("%v %v", s, err)
						s.outputErr <- err
						s.Close()
					}
				}
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
		if s.readBytes == nil && s.block.BlockContext().UserName != "" {
			s.readBytes = metrics.RegisterMetric(fmt.Sprintf(metrics.UserMetricGroupFormat, s.block.BlockContext().UserName), metrics.UserMetricReadBytes)
		}
		if s.writeBytes == nil && s.block.BlockContext().UserName != "" {
			s.writeBytes = metrics.RegisterMetric(fmt.Sprintf(metrics.UserMetricGroupFormat, s.block.BlockContext().UserName), metrics.UserMetricWriteBytes)
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
		das, ok := seg.metadata.(*dataAckStruct)
		if ok {
			unAckSeq := das.unAckSeq
			for {
				seg2, deleted := s.sendBuf.DeleteMinIf(func(iter *segment) bool {
					seq, _ := iter.Seq()
					return seq < unAckSeq
				})
				if !deleted {
					break
				}
				s.rttStat.UpdateRTT(time.Since(seg2.txTime))
				s.sendAlgorithm.OnAck()
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
	default:
		return fmt.Errorf("unsupported transport protocol %v", s.conn.TransportProtocol())
	}

	if !s.isClient && seg.metadata.Protocol() == openSessionRequest {
		s.wLock.Lock()
		defer s.wLock.Unlock()
		if s.isState(sessionAttached) {
			// Server needs to send open session response.
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
		das := seg.metadata.(*dataAckStruct)
		unAckSeq := das.unAckSeq
		for {
			seg2, deleted := s.sendBuf.DeleteMinIf(func(iter *segment) bool {
				seq, _ := iter.Seq()
				return seq < unAckSeq
			})
			if !deleted {
				break
			}
			s.rttStat.UpdateRTT(time.Since(seg2.txTime))
			s.sendAlgorithm.OnAck()
		}
		s.remoteWindowSize = das.windowSize
		return nil
	default:
		return fmt.Errorf("unsupported transport protocol %v", s.conn.TransportProtocol())
	}
}

func (s *Session) inputClose(seg *segment) error {
	s.wLock.Lock()
	defer s.wLock.Unlock()
	if seg.metadata.Protocol() == closeSessionRequest {
		// Send close session response.
		seg2 := &segment{
			metadata: &sessionStruct{
				baseStruct: baseStruct{
					protocol: uint8(closeSessionResponse),
				},
				sessionID:  s.id,
				seq:        s.nextSend,
				statusCode: 0,
				payloadLen: 0,
			},
			transport: s.conn.TransportProtocol(),
		}
		s.nextSend++
		// The response will not retry if it is not delivered.
		if err := s.output(seg2, s.RemoteAddr()); err != nil {
			return fmt.Errorf("output() failed: %v", err)
		}
		// Immediately shutdown event loop.
		log.Debugf("Remote requested to shut down %v", s)
		s.forwardStateTo(sessionClosed)
		select {
		case <-s.done:
		default:
			close(s.done)
			metrics.CurrEstablished.Add(-1)
		}
	} else if seg.metadata.Protocol() == closeSessionResponse {
		// Immediately shutdown event loop.
		log.Debugf("Remote received the request from %v to shut down", s)
		s.forwardStateTo(sessionClosed)
		select {
		case <-s.done:
		default:
			close(s.done)
			metrics.CurrEstablished.Add(-1)
		}
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
			if !stderror.ShouldRetry(err) {
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
