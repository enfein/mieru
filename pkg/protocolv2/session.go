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
	segmentTreeCapacity = 4096
	segmentChanCapacity = 1024
	minWindowSize       = 16
	maxWindowSize       = 4096

	segmentRetryInterval = 10 * time.Millisecond
	segmentAckDelay      = 50 * time.Millisecond

	serverRespTimeout        = 10 * time.Second
	sessionHeartbeatInterval = 5 * time.Second
)

type sessionState byte

const (
	sessionInit        sessionState = 0
	sessionAttached    sessionState = 1
	sessionOpening     sessionState = 2
	sessionEstablished sessionState = 3
	sessionClosing     sessionState = 4
	sessionClosed      sessionState = 5
)

func (ss sessionState) String() string {
	switch ss {
	case sessionInit:
		return "sessionInit"
	case sessionAttached:
		return "sessionAttached"
	case sessionOpening:
		return "sessionOpening"
	case sessionEstablished:
		return "sessionEstablished"
	case sessionClosing:
		return "sessionClosing"
	case sessionClosed:
		return "sessionClosed"
	default:
		return "UNKNOWN"
	}
}

type Session struct {
	conn  Underlay           // underlay connection
	block cipher.BlockCipher // cipher to encrypt and decrypt data

	id            uint32        // session ID number
	isClient      bool          // if this session is owned by client
	mtu           int           // L2 maxinum transmission unit
	remoteAddr    net.Addr      // remote network address
	state         sessionState  // session state
	readDeadline  time.Time     // read deadline
	writeDeadline time.Time     // write deadline
	ready         chan struct{} // indicate the session is ready to use
	established   chan struct{} // indicate the session handshake is completed
	done          chan struct{} // indicate the session is complete
	inputErr      chan error    // input error
	outputErr     chan error    // output error

	sendQueue *segmentTree  // segments waiting to send
	sendBuf   *segmentTree  // segments sent but not acknowledged
	recvBuf   *segmentTree  // segments received but acknowledge is not sent
	recvQueue *segmentTree  // segments waiting to be read by application
	recvChan  chan *segment // channel to receive segment from underlay

	nextSeq    uint32    // next sequence number to send a segment
	nextRecv   uint32    // next sequence number to receive
	lastRXTime time.Time // last timestamp when a segment is received
	lastTXTime time.Time // last timestamp when a segment is sent
	unreadBuf  []byte    // payload removed from the recvQueue that haven't been read by application

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
	rttStat.SetMaxAckDelay(2 * segmentRetryInterval)
	rttStat.SetRTOMultiplier(1.5)
	return &Session{
		conn:             nil,
		block:            nil,
		id:               id,
		isClient:         isClient,
		mtu:              mtu,
		state:            sessionInit,
		readDeadline:     util.ZeroTime(),
		writeDeadline:    util.ZeroTime(),
		ready:            make(chan struct{}),
		established:      make(chan struct{}),
		done:             make(chan struct{}),
		inputErr:         make(chan error, 1),
		outputErr:        make(chan error, 1),
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
		return fmt.Sprintf("Session{%d}", s.id)
	}
	return fmt.Sprintf("Session{%d - %v - %v}", s.id, s.LocalAddr(), s.RemoteAddr())
}

// Read lets a user to read data from receive queue.
// The data boundary is preserved, i.e. no fragment read.
func (s *Session) Read(b []byte) (n int, err error) {
	if s.isStateBefore(sessionAttached, false) {
		return 0, fmt.Errorf("%v is not ready for Write()", s)
	}
	if s.isStateAfter(sessionClosed, true) {
		return 0, io.ErrClosedPipe
	}
	s.rLock.Lock()
	defer s.rLock.Unlock()

	defer func() {
		s.readDeadline = util.ZeroTime()
	}()
	if log.IsLevelEnabled(log.TraceLevel) {
		log.Tracef("%v trying to read %d bytes", s, len(b))
	}

	// There are some remaining data that application
	// failed to read last time due to short buffer.
	if len(s.unreadBuf) > 0 {
		if len(b) < len(s.unreadBuf) {
			return 0, io.ErrShortBuffer
		}
		n = copy(b, s.unreadBuf)
		s.unreadBuf = nil
		metrics.InBytes.Add(int64(n))
		return n, nil
	}

	var timeC <-chan time.Time
	if !util.IsZeroTime(s.readDeadline) {
		timeC = time.After(time.Until(s.readDeadline))
	}

	for {
		select {
		case <-s.done:
			return 0, io.EOF
		case <-s.inputErr:
			return 0, io.ErrUnexpectedEOF
		case <-timeC:
			return 0, stderror.ErrTimeout
		default:
		}
		if !s.recvQueue.IsReadReady() {
			time.Sleep(segmentRetryInterval)
			continue
		}

		// Segments in segment tree are ready to read.
		for {
			seg, ok := s.recvQueue.DeleteMin()
			if !ok {
				return 0, stderror.ErrEmpty
			}

			if s.isClient && seg.metadata.Protocol() == openSessionResponse && (s.isState(sessionAttached) || s.isState(sessionOpening)) {
				s.forwardStateTo(sessionEstablished)
				close(s.established)
			}

			if len(s.unreadBuf) == 0 {
				s.unreadBuf = seg.payload
			} else {
				s.unreadBuf = append(s.unreadBuf, seg.payload...)
			}

			fragment := seg.Fragment()
			if fragment == 0 {
				break
			}
		}
		if len(s.unreadBuf) > 0 {
			break
		}
	}

	if len(b) < len(s.unreadBuf) {
		return 0, io.ErrShortBuffer
	}
	n = copy(b, s.unreadBuf)
	s.unreadBuf = nil
	metrics.InBytes.Add(int64(n))
	return n, nil
}

// Write stores the data to send queue.
func (s *Session) Write(b []byte) (n int, err error) {
	if len(b) > MaxPDU {
		return 0, io.ErrShortWrite
	}
	if s.isStateBefore(sessionAttached, false) {
		return 0, fmt.Errorf("%v is not ready for Write()", s)
	}
	if s.isStateAfter(sessionClosed, true) {
		return 0, io.ErrClosedPipe
	}
	s.wLock.Lock()
	defer s.wLock.Unlock()

	defer func() {
		s.writeDeadline = util.ZeroTime()
	}()
	if s.isState(sessionAttached) {
		if s.isClient {
			// Send open session request.
			seg := &segment{
				metadata: &sessionStruct{
					baseStruct: baseStruct{
						protocol: uint8(openSessionRequest),
					},
					sessionID: s.id,
					seq:       s.nextSeq,
				},
				transport: s.conn.TransportProtocol(),
			}
			s.nextSeq++
			if len(b) <= maxSessionOpenPayload {
				seg.metadata.(*sessionStruct).payloadLen = uint16(len(b))
				seg.payload = b
			}
			if log.IsLevelEnabled(log.TraceLevel) {
				log.Tracef("%v writing %d bytes with open session request", s, len(seg.payload))
			}
			s.sendQueue.InsertBlocking(seg)
			s.forwardStateTo(sessionOpening)
			if len(seg.payload) > 0 {
				return len(seg.payload), nil
			}
		} else {
			// Send open session response.
			seg := &segment{
				metadata: &sessionStruct{
					baseStruct: baseStruct{
						protocol: uint8(openSessionResponse),
					},
					sessionID: s.id,
					seq:       s.nextSeq,
				},
				transport: s.conn.TransportProtocol(),
			}
			s.nextSeq++
			if len(b) <= maxSessionOpenPayload {
				seg.metadata.(*sessionStruct).payloadLen = uint16(len(b))
				seg.payload = b
			}
			if log.IsLevelEnabled(log.TraceLevel) {
				log.Tracef("%v writing %d bytes with open session response", s, len(seg.payload))
			}
			s.sendQueue.InsertBlocking(seg)
			s.forwardStateTo(sessionEstablished)
			if len(seg.payload) > 0 {
				return len(seg.payload), nil
			}
		}
	}

	var timeC <-chan time.Time
	if !util.IsZeroTime(s.writeDeadline) {
		timeC = time.After(time.Until(s.writeDeadline))
	}

	nFragment := 1
	fragmentSize := MaxFragmentSize(s.mtu, s.conn.IPVersion(), s.conn.TransportProtocol())
	if len(b) > fragmentSize {
		nFragment = (len(b)-1)/fragmentSize + 1
	}
	if log.IsLevelEnabled(log.TraceLevel) {
		log.Tracef("%v writing %d bytes with %d fragments", s, len(b), nFragment)
	}

	ptr := b
	for i := nFragment - 1; i >= 0; i-- {
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
				seq:        s.nextSeq,
				unAckSeq:   s.nextRecv,
				windowSize: uint16(mathext.Max(0, int(s.sendAlgorithm.CongestionWindowSize())-s.recvBuf.Len())),
				fragment:   uint8(i),
				payloadLen: uint16(partLen),
			},
			payload:   part,
			transport: s.conn.TransportProtocol(),
		}
		s.nextSeq++
		for {
			select {
			case <-s.done:
				return 0, io.EOF
			case <-s.outputErr:
				return 0, io.ErrClosedPipe
			case <-timeC:
				return 0, stderror.ErrTimeout
			default:
			}
			ok := s.sendQueue.Insert(seg)
			if ok {
				break
			}
			time.Sleep(segmentRetryInterval)
		}
		ptr = ptr[partLen:]
	}

	if s.isClient {
		s.readDeadline = time.Now().Add(serverRespTimeout)
	}
	n = len(b)
	metrics.OutBytes.Add(int64(n))
	return n, nil
}

// Close actively terminates the session. If the session is terminated by the
// other party, underlay is responsible to terminate the session at our end.
func (s *Session) Close() error {
	select {
	case <-s.done:
		s.forwardStateTo(sessionClosed)
		log.Debugf("%v is already closed", s)
		return nil
	default:
	}

	log.Debugf("Closing %v", s)
	s.wLock.Lock()
	defer s.wLock.Unlock()

	switch s.conn.TransportProtocol() {
	case util.TCPTransport:
		// Send closeSessionRequest, and wait for closeSessionResponse.
		s.forwardStateTo(sessionClosing)
		seg := &segment{
			metadata: &sessionStruct{
				baseStruct: baseStruct{
					protocol: uint8(closeSessionRequest),
				},
				sessionID: s.id,
				seq:       s.nextSeq,
			},
			transport: s.conn.TransportProtocol(),
		}
		s.nextSeq++
		s.sendQueue.InsertBlocking(seg)
		<-s.done
	case util.UDPTransport:
		if s.isState(sessionEstablished) {
			// Send a final ACK to the peer.
			s.forwardStateTo(sessionClosing)
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
					seq:        uint32(mathext.Max(0, int(s.nextSeq)-1)),
					unAckSeq:   s.nextRecv,
					windowSize: uint16(mathext.Max(0, int(s.sendAlgorithm.CongestionWindowSize())-s.recvBuf.Len())),
				},
				transport: s.conn.TransportProtocol(),
			}
			if err := s.output(ackSeg, s.RemoteAddr()); err != nil {
				log.Debugf("output() failed: %v", err)
			}
		}
	default:
		log.Debugf("unsupported transport protocol %v", s.conn.TransportProtocol())
	}
	s.forwardStateTo(sessionClosed)
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

func (s *Session) SetDeadline(t time.Time) error {
	s.readDeadline = t
	s.writeDeadline = t
	return nil
}

func (s *Session) SetReadDeadline(t time.Time) error {
	s.readDeadline = t
	return nil
}

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
	if log.IsLevelEnabled(log.TraceLevel) {
		log.Tracef("%v %v => %v", s, s.state, new)
	}
	s.state = new
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
				s.inputErr <- err
				return err
			}
		}
	}
}

func (s *Session) runOutputLoop(ctx context.Context) error {
	var lastErr error
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-s.done:
			return nil
		default:
			if lastErr != nil {
				s.outputErr <- lastErr
				return lastErr
			}
			switch s.conn.TransportProtocol() {
			case util.TCPTransport:
				for {
					seg, ok := s.sendQueue.DeleteMin()
					if !ok {
						time.Sleep(segmentRetryInterval)
						break
					}
					if err := s.output(seg, nil); err != nil {
						err = fmt.Errorf("output() failed: %w", err)
						s.outputErr <- err
						return err
					}
				}
			case util.UDPTransport:
				needRetransmission := false
				hasLoss := false
				hasTimeout := false
				// Resend segments in sendBuf.
				s.sendBuf.Ascend(func(iter *segment) bool {
					if iter.txCount >= txCountLimit {
						lastErr = fmt.Errorf("too many retransmission of %v", iter)
						return false
					}
					if iter.fastAck >= fastAckLimit {
						needRetransmission = true
						hasLoss = true
						iter.txCount++
						iter.fastAck = 0
						iter.txTime = time.Now()
						iter.txTimeout = s.rttStat.RTO()
						if err := s.output(iter, s.RemoteAddr()); err != nil {
							lastErr = fmt.Errorf("output() failed: %w", err)
							return false
						}
						return true
					}
					if time.Since(iter.txTime) > iter.txTimeout {
						needRetransmission = true
						hasTimeout = true
						iter.txCount++
						iter.fastAck = 0
						iter.txTime = time.Now()
						iter.txTimeout = s.rttStat.RTO()
						if err := s.output(iter, s.RemoteAddr()); err != nil {
							lastErr = fmt.Errorf("output() failed: %w", err)
							return false
						}
						return true
					}
					return true
				})
				if hasTimeout {
					s.sendAlgorithm.OnTimeout()
				} else if hasLoss {
					s.sendAlgorithm.OnLoss()
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
						seg.fastAck = 0
						seg.txTime = time.Now()
						seg.txTimeout = s.rttStat.RTO()
						if err := s.output(seg, s.RemoteAddr()); err != nil {
							err = fmt.Errorf("output() failed: %w", err)
							s.outputErr <- err
							return err
						}
						s.sendBuf.InsertBlocking(seg)
					}
				}
				if !needRetransmission && segmentMoved == 0 {
					// Send ACK or heartbeat if needed.
					if (s.sendBuf.Len() > 0 && time.Since(s.lastTXTime) > segmentAckDelay) || time.Since(s.lastRXTime) > sessionHeartbeatInterval {
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
								seq:        uint32(mathext.Max(0, int(s.nextSeq)-1)),
								unAckSeq:   s.nextRecv,
								windowSize: uint16(mathext.Max(0, int(s.sendAlgorithm.CongestionWindowSize())-s.recvBuf.Len())),
							},
							transport: s.conn.TransportProtocol(),
						}
						if err := s.output(ackSeg, s.RemoteAddr()); err != nil {
							err = fmt.Errorf("output() failed: %w", err)
							s.outputErr <- err
							return err
						}
					}
				}
				time.Sleep(segmentRetryInterval)
			default:
				err := fmt.Errorf("unsupported transport protocol %v", s.conn.TransportProtocol())
				s.outputErr <- err
				return err
			}
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
		return nil
	case util.UDPTransport:
		// Delete all previous acknowledged segments from sendBuf.
		das, ok := seg.metadata.(*dataAckStruct)
		if ok {
			unAckSeq := das.unAckSeq
			for {
				seg2, deleted := s.sendBuf.DeleteMinIf(func(iter *segment) bool {
					seq, err := iter.Seq()
					if err != nil {
						panic(fmt.Sprintf("%v get segment sequence number failed: %v", s, err))
					}
					if seq < unAckSeq {
						return true
					}
					return false
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
				seq, err := iter.Seq()
				if err != nil {
					panic(fmt.Sprintf("%v get segment sequence number failed: %v", s, err))
				}
				if seq <= s.nextRecv {
					return true
				}
				return false
			})
			if seg3 == nil || !deleted {
				return nil
			}
			seq, err := seg3.Seq()
			if err != nil {
				panic(fmt.Sprintf("%v get segment sequence number failed: %v", s, err))
			}
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
				seq, err := iter.Seq()
				if err != nil {
					panic(fmt.Sprintf("%v get segment sequence number failed: %v", s, err))
				}
				if seq < unAckSeq {
					return true
				}
				return false
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
	if seg.metadata.Protocol() == closeSessionRequest {
		// Send close session response.
		seg2 := &segment{
			metadata: &sessionStruct{
				baseStruct: baseStruct{
					protocol: uint8(closeSessionResponse),
				},
				sessionID:  s.id,
				seq:        s.nextSeq,
				statusCode: 0,
				payloadLen: 0,
			},
			transport: s.conn.TransportProtocol(),
		}
		s.nextSeq++
		// The response will not retry if it is not delivered.
		if err := s.output(seg2, s.RemoteAddr()); err != nil {
			return fmt.Errorf("output() failed: %v", err)
		}
		// Immediately shutdown event loop.
		if log.IsLevelEnabled(log.TraceLevel) {
			log.Tracef("Shutdown %v", s)
		}
		close(s.done)
		s.forwardStateTo(sessionClosed)
	} else if seg.metadata.Protocol() == closeSessionResponse {
		// Immediately shutdown event loop.
		if log.IsLevelEnabled(log.TraceLevel) {
			log.Tracef("Shutdown %v", s)
		}
		close(s.done)
		s.forwardStateTo(sessionClosed)
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
