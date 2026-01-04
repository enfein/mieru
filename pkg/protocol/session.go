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

	apicommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/congestion"
	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/mathext"
	"github.com/enfein/mieru/v3/pkg/metrics"
	"github.com/enfein/mieru/v3/pkg/stderror"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// Buffer some segments before blocking the underlay input loop.
	segmentChanCapacity = 1024

	// Maximum number of segments in a queue or buffer.
	segmentTreeCapacity = 4096

	// Use a very large value for receive queue, because application may be lazy
	// and only read received segments on demand.
	segmentTreeRecvQueueCapacity = 1024 * 1024

	minWindowSize = 16
	maxWindowSize = segmentTreeCapacity

	serverRespTimeout        = 10 * time.Second
	sessionHeartbeatInterval = 5 * time.Second

	// periodicOutputInterval triggers periodic output of packet transport,
	// even if there is no new data to send.
	periodicOutputInterval = 1 * time.Millisecond

	// backPressureDelay is a short sleep to add back pressure
	// to the writer.
	backPressureDelay = 100 * time.Microsecond

	// Number of ack to trigger early retransmission.
	earlyRetransmission = 3
	// Maximum number of early retransmission attempt.
	earlyRetransmissionLimit = 1
	// Send timeout back off multiplier.
	txTimeoutBackOff = 1.5
	// Maximum back off multiplier.
	maxBackOffDuration = 10 * time.Second
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

	id                uint32                   // session ID number
	isClient          bool                     // if this session is owned by client
	mtu               int                      // L2 maxinum transmission unit
	transportProtocol common.TransportProtocol // transport protocol of underlay connection
	remoteAddr        net.Addr                 // specify remote network address, used by UDP
	state             sessionState             // session state
	status            statusCode               // session status

	users    map[string]*appctlpb.User // all registered users, only used by server
	userName atomic.Pointer[string]    // user that owns this session, only used by server

	ready                  chan struct{} // indicate the session is ready to use
	openSessionRequestSent atomic.Bool   // whether open session request has been sent, only used by client
	closeRequested         atomic.Bool   // the session is being closed or has been closed
	closedChan             chan struct{} // indicate the session is closed
	readDeadline           atomic.Int64  // read deadline, in microseconds since Unix epoch
	writeDeadline          atomic.Int64  // write deadline, in microseconds since Unix epoch
	inputHasErr            atomic.Bool   // input has error
	inputErr               chan error    // this channel is closed when input has error
	outputHasErr           atomic.Bool   // output has error
	outputErr              chan error    // this channel is closed when output has error

	sendQueue *segmentTree  // segments waiting to send
	sendBuf   *segmentTree  // segments sent but not acknowledged
	recvBuf   *segmentTree  // segments received but acknowledge is not sent
	recvQueue *segmentTree  // segments waiting to be read by application
	recvChan  chan *segment // channel to receive segments from underlay

	nextSend               atomic.Uint32 // next sequence number to send a segment
	nextRecv               atomic.Uint32 // next sequence number to receive
	lastSend               atomic.Uint32 // last segment sequence number sent
	lastRXTime             atomic.Int64  // last timestamp when a segment is received, in microseconds since Unix epoch
	lastTXTime             atomic.Int64  // last timestamp when a segment is sent, in microseconds since Unix epoch
	nextRetransmissionTime atomic.Int64  // time that need to retransmit a segment in sendBuf, in microseconds since Unix epoch
	ackOnDataRecv          atomic.Bool   // whether ack should be sent due to receive of new data
	unreadBuf              []byte        // payload removed from the recvQueue that haven't been read by application

	uploadBytes   metrics.Metric // number of bytes from client to server, only used by server
	downloadBytes metrics.Metric // number of bytes from server to client, only used by server

	rttStat            *congestion.RTTStats
	cubicSendAlgorithm *congestion.CubicSendAlgorithm
	bbrSendAlgorithm   *congestion.BBRSender
	remoteWindowSize   atomic.Uint32

	wg    sync.WaitGroup
	rLock sync.Mutex // serialize application read
	wLock sync.Mutex // serialize application write
	oLock sync.Mutex // serialize the output sequence
	sLock sync.Mutex // serialize the state transition
}

var (
	// Session implements net.Conn interface.
	_ net.Conn = (*Session)(nil)

	// Session implements UserContext interface.
	_ apicommon.UserContext = (*Session)(nil)
)

// NewSession creates a new session.
func NewSession(id uint32, isClient bool, mtu int, users map[string]*appctlpb.User) *Session {
	rttStat := congestion.NewRTTStats()
	rttStat.SetMaxAckDelay(periodicOutputInterval)
	rttStat.SetRTOMultiplier(txTimeoutBackOff)
	s := &Session{
		conn:               nil,
		block:              atomic.Pointer[cipher.BlockCipher]{},
		id:                 id,
		isClient:           isClient,
		mtu:                mtu,
		state:              sessionInit,
		status:             statusOK,
		users:              users,
		ready:              make(chan struct{}),
		closedChan:         make(chan struct{}),
		inputErr:           make(chan error),
		outputErr:          make(chan error),
		sendQueue:          newSegmentTree(segmentTreeCapacity),
		sendBuf:            newSegmentTree(segmentTreeCapacity),
		recvBuf:            newSegmentTree(segmentTreeCapacity),
		recvQueue:          newSegmentTree(segmentTreeRecvQueueCapacity),
		recvChan:           make(chan *segment, segmentChanCapacity),
		rttStat:            rttStat,
		cubicSendAlgorithm: congestion.NewCubicSendAlgorithm(minWindowSize, maxWindowSize),
		bbrSendAlgorithm:   congestion.NewBBRSender(fmt.Sprintf("%d", id), rttStat),
	}
	now := time.Now().UnixMicro()
	s.lastRXTime.Store(now)
	s.lastTXTime.Store(now)
	s.remoteWindowSize.Store(minWindowSize)
	return s
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
		s.readDeadline.Store(0)
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
	if readDeadline := s.readDeadline.Load(); readDeadline != 0 {
		timeC = time.After(time.Until(time.UnixMicro(readDeadline)))
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
	s.wLock.Lock()
	defer s.wLock.Unlock()

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
		s.writeDeadline.Store(0)
	}()

	// Before the first write, client needs to send open session request.
	// Open session request is sent only once. Underlay may retry if the packet is lost.
	if s.isClient && s.isState(sessionAttached) && !s.openSessionRequestSent.Swap(true) {
		s.oLock.Lock()
		seg := &segment{
			metadata: &sessionStruct{
				baseStruct: baseStruct{
					protocol: uint8(openSessionRequest),
				},
				sessionID: s.id,
				seq:       s.nextSend.Load(),
			},
			transport: s.transportProtocol,
		}
		s.nextSend.Add(1)
		// Allow open session request to carry payload.
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
	if s.conn != nil {
		return s.conn.LocalAddr()
	}
	return common.NilNetAddr()
}

func (s *Session) RemoteAddr() net.Addr {
	if !common.IsNilNetAddr(s.remoteAddr) {
		return s.remoteAddr
	}
	if s.conn != nil {
		return s.conn.RemoteAddr()
	}
	return common.NilNetAddr()
}

// SetDeadline implements net.Conn.
func (s *Session) SetDeadline(t time.Time) error {
	micros := t.UnixMicro()
	if t.IsZero() {
		micros = 0
	}
	s.readDeadline.Store(micros)
	s.writeDeadline.Store(micros)
	return nil
}

// SetReadDeadline implements net.Conn.
func (s *Session) SetReadDeadline(t time.Time) error {
	micros := t.UnixMicro()
	if t.IsZero() {
		micros = 0
	}
	s.readDeadline.Store(micros)
	return nil
}

// SetWriteDeadline implements net.Conn.
func (s *Session) SetWriteDeadline(t time.Time) error {
	micros := t.UnixMicro()
	if t.IsZero() {
		micros = 0
	}
	s.writeDeadline.Store(micros)
	return nil
}

// UserName returns the user that owns this session,
// only if this is a server session.
func (s *Session) UserName() string {
	if p := s.userName.Load(); p != nil {
		return *p
	}
	return ""
}

// ToSessionInfo creates related SessionInfo protobuf object.
func (s *Session) ToSessionInfo() *appctlpb.SessionInfo {
	info := &appctlpb.SessionInfo{
		Id:         proto.String(fmt.Sprintf("%d", s.id)),
		LocalAddr:  proto.String("-"),
		RemoteAddr: proto.String("-"),
		State:      proto.String(s.state.String()),
		RecvQ:      proto.Uint32(uint32(s.recvQueue.Len())),
		RecvBuf:    proto.Uint32(uint32(s.recvBuf.Len())),
		SendQ:      proto.Uint32(uint32(s.sendQueue.Len())),
		SendBuf:    proto.Uint32(uint32(s.sendBuf.Len())),
	}
	if s.conn != nil {
		info.LocalAddr = proto.String(s.conn.LocalAddr().String())
		info.RemoteAddr = proto.String(s.conn.RemoteAddr().String())
		if _, ok := s.conn.(*StreamUnderlay); ok {
			info.Protocol = proto.String("TCP")
			lastRXTime := time.UnixMicro(s.lastRXTime.Load())
			info.LastRecvTime = &timestamppb.Timestamp{
				Seconds: lastRXTime.Unix(),
				Nanos:   int32(lastRXTime.Nanosecond()),
			}
			// TCP info.LastRecvSeq is not available.
		} else if _, ok := s.conn.(*PacketUnderlay); ok {
			info.Protocol = proto.String("UDP")
			lastRXTime := time.UnixMicro(s.lastRXTime.Load())
			info.LastRecvTime = &timestamppb.Timestamp{
				Seconds: lastRXTime.Unix(),
				Nanos:   int32(lastRXTime.Nanosecond()),
			}
			info.LastRecvSeq = proto.Uint32(uint32(mathext.Max(0, int(s.nextRecv.Load())-1)))
		} else {
			info.Protocol = proto.String("UNKNOWN")
		}
		lastTXTime := time.UnixMicro(s.lastTXTime.Load())
		info.LastSendTime = &timestamppb.Timestamp{
			Seconds: lastTXTime.Unix(),
			Nanos:   int32(lastTXTime.Nanosecond()),
		}
		info.LastSendSeq = proto.Uint32(uint32(mathext.Max(0, int(s.nextSend.Load())-1)))
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
	if writeDeadline := s.writeDeadline.Load(); writeDeadline != 0 {
		timeC = time.After(time.Until(time.UnixMicro(writeDeadline)))
	}

	seqBeforeWrite, _ := s.sendQueue.MinSeq()

	// Determine number of fragments to write.
	nFragment := 1
	fragmentSize := MaxFragmentSize(s.mtu, s.transportProtocol)
	if len(b) > fragmentSize {
		nFragment = (len(b)-1)/fragmentSize + 1
	}
	for s.sendQueue.Remaining() <= nFragment { // reserve one slot for Close()
		select {
		case <-s.closedChan:
			return 0, io.EOF
		case <-s.outputErr:
			return 0, io.ErrClosedPipe
		case <-timeC:
			return 0, stderror.ErrTimeout
		default:
		}
		time.Sleep(backPressureDelay) // add back pressure if queue is full
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
				seq:        s.nextSend.Load(),
				unAckSeq:   s.nextRecv.Load(),
				windowSize: uint16(s.receiveWindowSize()),
				fragment:   uint8(i),
				payloadLen: uint16(partLen),
			},
			payload:   make([]byte, partLen),
			transport: s.transportProtocol,
		}
		copy(seg.payload, part)
		s.nextSend.Add(1)
		if !s.sendQueue.Insert(seg) {
			s.oLock.Unlock()
			return 0, fmt.Errorf("insert %v to send queue failed", seg)
		}
		ptr = ptr[partLen:]
	}
	s.oLock.Unlock()

	// To create back pressure, wait until sendQueue is empty or moving.
	shouldReturn := false
	for {
		select {
		case <-s.sendQueue.chanEmptyEvent:
			shouldReturn = true
		// do not consume s.sendQueue.chanNotEmptyEvent,
		// because it is used to drive the output loop.
		default:
		}
		if shouldReturn {
			break
		}

		seqAfterWrite, err := s.sendQueue.MinSeq()
		if err != nil || seqAfterWrite > seqBeforeWrite {
			break
		} else {
			time.Sleep(backPressureDelay) // add back pressure if send queue is not moving
		}
	}

	if s.isClient {
		s.readDeadline.Store(time.Now().Add(serverRespTimeout).UnixMicro())
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
				if s.inputHasErr.CompareAndSwap(false, true) {
					close(s.inputErr)
				}
				s.closeWithError(err)
				return err
			}
		}
	}
}

func (s *Session) runOutputLoop(ctx context.Context) error {
	switch s.transportProtocol {
	case common.StreamTransport:
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-s.closedChan:
				return nil
			case <-s.sendQueue.chanNotEmptyEvent:
				s.runOutputOnceStream()
			}
		}
	case common.PacketTransport:
		ticker := time.NewTicker(periodicOutputInterval)
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
			s.runOutputOncePacket()
		}
	default:
		err := fmt.Errorf("unsupported transport protocol %v", s.transportProtocol)
		log.Debugf("%v %v", s, err)
		if s.outputHasErr.CompareAndSwap(false, true) {
			close(s.outputErr)
		}
		return s.closeWithError(err)
	}
}

func (s *Session) runOutputOnceStream() {
	if s.outputHasErr.Load() {
		// Can't run output.
		time.Sleep(periodicOutputInterval)
		return
	}

	s.oLock.Lock()
	for {
		seg, ok := s.sendQueue.DeleteMin()
		if !ok {
			s.oLock.Unlock()
			break
		}
		if err := s.output(seg, nil); err != nil {
			err = fmt.Errorf("output() failed: %w", err)
			log.Debugf("%v %v", s, err)
			if s.outputHasErr.CompareAndSwap(false, true) {
				close(s.outputErr)
			}
			s.oLock.Unlock() // s.oLock can be acquired by s.closeWithError().
			s.closeWithError(err)
			break
		}
	}
}

func (s *Session) runOutputOncePacket() {
	if s.outputHasErr.Load() {
		// Can't run output.
		time.Sleep(periodicOutputInterval)
		return
	}

	var closeSessionReason error
	hasLoss := false
	hasTimeout := false
	var bytesInFlight int64
	totalTransmissionCount := 0

	if time.Now().UnixMicro() >= s.nextRetransmissionTime.Load() {
		// Resend segments in sendBuf.
		//
		// Iterate all the segments in sendBuf to calculate bytesInFlight and
		// update nextRetransmissionTime, but only resend segments if needed.
		//
		// Retransmission is not limited by window.
		//
		// To avoid deadlock, session can't be closed inside Ascend().
		s.oLock.Lock()
		var nextTX int64 = math.MaxInt64
		s.sendBuf.Ascend(func(iter *segment) bool {
			bytesInFlight += int64(packetOverhead + len(iter.payload))
			nextTX = mathext.Min(nextTX, iter.txTime+iter.txTimeout.Microseconds())

			if iter.txCount >= txCountLimit {
				err := fmt.Errorf("too many retransmission of %v", iter)
				log.Debugf("%v is unhealthy: %v", s, err)
				if s.outputHasErr.CompareAndSwap(false, true) {
					close(s.outputErr)
				}
				closeSessionReason = err
				return false
			}

			satisfyEarlyRetransmission := iter.ackCount >= earlyRetransmission && iter.txCount <= earlyRetransmissionLimit
			if satisfyEarlyRetransmission || time.Now().UnixMicro()-iter.txTime > iter.txTimeout.Microseconds() {
				if satisfyEarlyRetransmission {
					hasLoss = true
				} else {
					hasTimeout = true
				}
				iter.ackCount = 0
				iter.txCount++
				iter.txTime = time.Now().UnixMicro()
				iter.txTimeout = mathext.Min(s.rttStat.RTO()*time.Duration(math.Pow(txTimeoutBackOff, float64(iter.txCount))), maxBackOffDuration)
				if isDataAckProtocol(iter.metadata.Protocol()) {
					das, _ := toDataAckStruct(iter.metadata)
					das.unAckSeq = s.nextRecv.Load()
				}
				if err := s.output(iter, s.RemoteAddr()); err != nil {
					err = fmt.Errorf("output() failed: %w", err)
					log.Debugf("%v %v", s, err)
					if s.outputHasErr.CompareAndSwap(false, true) {
						close(s.outputErr)
					}
					closeSessionReason = err
					return false
				}
				bytesInFlight += int64(packetOverhead + len(iter.payload))
				totalTransmissionCount++
				return true
			}
			return true
		})
		if nextTX != math.MaxInt64 {
			s.nextRetransmissionTime.Store(nextTX)
		} else {
			// Account for new segments to be sent below.
			s.nextRetransmissionTime.Store(time.Now().UnixMicro() + 10000)
		}
		s.oLock.Unlock()
		if closeSessionReason != nil {
			s.closeWithError(closeSessionReason)
		}
		if hasTimeout {
			s.cubicSendAlgorithm.OnTimeout()
		} else if hasLoss {
			s.cubicSendAlgorithm.OnLoss()
		}
	}

	// Send new segments in sendQueue.
	skipSendNewSegment := s.sendWindowSize() <= 0
	if s.sendQueue.Len() > 0 && !skipSendNewSegment {
		s.oLock.Lock()
		for {
			if s.sendBuf.Remaining() <= 1 {
				s.oLock.Unlock()
				break
			}

			seg, deleted := s.sendQueue.DeleteMinIf(func(iter *segment) bool {
				bbrCanSend := s.bbrSendAlgorithm.CanSend(bytesInFlight, int64(packetOverhead+len(iter.payload)))
				congestionWindowCanSend := totalTransmissionCount < s.sendWindowSize()
				return bbrCanSend && congestionWindowCanSend
			})
			if !deleted {
				s.oLock.Unlock()
				break
			}

			seg.txCount++
			seg.txTime = time.Now().UnixMicro()
			seg.txTimeout = mathext.Min(s.rttStat.RTO()*time.Duration(math.Pow(txTimeoutBackOff, float64(seg.txCount))), maxBackOffDuration)
			if isDataAckProtocol(seg.metadata.Protocol()) {
				das, _ := toDataAckStruct(seg.metadata)
				das.unAckSeq = s.nextRecv.Load()
			}
			if !s.sendBuf.Insert(seg) {
				s.oLock.Unlock()
				err := fmt.Errorf("output() failed: insert %v to send buffer failed", seg)
				log.Debugf("%v %v", s, err)
				if s.outputHasErr.CompareAndSwap(false, true) {
					close(s.outputErr)
				}
				s.closeWithError(err)
				break
			}
			if err := s.output(seg, s.RemoteAddr()); err != nil {
				s.oLock.Unlock()
				err = fmt.Errorf("output() failed: %w", err)
				log.Debugf("%v %v", s, err)
				if s.outputHasErr.CompareAndSwap(false, true) {
					close(s.outputErr)
				}
				s.closeWithError(err)
				break
			} else {
				seq, err := seg.Seq()
				if err != nil {
					s.oLock.Unlock()
					err = fmt.Errorf("failed to get sequence number from %v: %w", seg, err)
					log.Debugf("%v %v", s, err)
					if s.outputHasErr.CompareAndSwap(false, true) {
						close(s.outputErr)
					}
					s.closeWithError(err)
					break
				}
				newBytesInFlight := int64(packetOverhead + len(seg.payload))
				s.bbrSendAlgorithm.OnPacketSent(time.Now(), bytesInFlight, int64(seq), newBytesInFlight, true)
				bytesInFlight += newBytesInFlight
				totalTransmissionCount++
			}
		}
	} else {
		s.bbrSendAlgorithm.OnApplicationLimited(bytesInFlight)
	}

	// Send ACK or heartbeat if needed.
	// ACK is not limited by window.
	exceedHeartbeatInterval := time.Now().UnixMicro()-s.lastTXTime.Load() > sessionHeartbeatInterval.Microseconds()
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
				seq:        uint32(mathext.Max(0, int(s.nextSend.Load())-1)),
				unAckSeq:   s.nextRecv.Load(),
				windowSize: uint16(s.receiveWindowSize()),
			},
			transport: s.transportProtocol}
		if err := s.output(ackSeg, s.RemoteAddr()); err != nil {
			s.oLock.Unlock()
			err = fmt.Errorf("output() failed: %w", err)
			log.Debugf("%v %v", s, err)
			if s.outputHasErr.CompareAndSwap(false, true) {
				close(s.outputErr)
			}
			s.closeWithError(err)
		} else {
			seq, err := ackSeg.Seq()
			if err != nil {
				panic(fmt.Sprintf("failed to get sequence number from ack segment %v: %v", ackSeg, err))
			} else {
				s.oLock.Unlock()
				newBytesInFlight := int64(packetOverhead + len(ackSeg.payload))
				s.bbrSendAlgorithm.OnPacketSent(time.Now(), bytesInFlight, int64(seq), newBytesInFlight, true)
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
		if s.UserName() == "" && seg.block.BlockContext().UserName != "" {
			userName := seg.block.BlockContext().UserName
			s.userName.Store(&userName)
		}

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

	s.lastRXTime.Store(time.Now().UnixMicro())
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
	switch s.transportProtocol {
	case common.StreamTransport:
		// Deliver the segment directly to recvQueue.
		for {
			if s.recvQueue.Remaining() <= 1 {
				time.Sleep(backPressureDelay)
			} else {
				break
			}
		}
		if !s.recvQueue.Insert(seg) {
			return fmt.Errorf("inputData() failed: insert %v to receive queue failed", seg)
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
				s.rttStat.UpdateRTT(time.Duration(time.Now().UnixMicro()-seg2.txTime) * time.Microsecond)
				s.cubicSendAlgorithm.OnAck()
				seq, _ := seg2.Seq()
				ackedPackets = append(ackedPackets, congestion.AckedPacketInfo{
					PacketNumber:     int64(seq),
					BytesAcked:       int64(packetOverhead + len(seg2.payload)),
					ReceiveTimestamp: time.Now(),
				})
			}
			if len(ackedPackets) > 0 {
				s.bbrSendAlgorithm.OnCongestionEvent(priorInFlight, time.Now(), ackedPackets, nil)
			}
			s.remoteWindowSize.Store(uint32(das.windowSize))
		}

		// Deliver the segment to recvBuf.
		for {
			if s.recvBuf.Remaining() <= 1 {
				time.Sleep(backPressureDelay)
			} else {
				break
			}
		}
		if !s.recvBuf.Insert(seg) {
			return fmt.Errorf("inputData() failed: insert %v to receive buffer failed", seg)
		}

		// Move recvBuf to recvQueue.
		for {
			if s.recvQueue.Remaining() <= 1 {
				break
			}

			seg3, deleted := s.recvBuf.DeleteMinIf(func(iter *segment) bool {
				seq, _ := iter.Seq()
				return seq <= s.nextRecv.Load()
			})
			if seg3 == nil || !deleted {
				break
			}
			seq, _ := seg3.Seq()
			if seq == s.nextRecv.Load() {
				if !s.recvQueue.Insert(seg3) {
					return fmt.Errorf("inputData() failed: insert %v to receive queue failed", seg3)
				}
				s.nextRecv.Add(1)
				das, ok := seg3.metadata.(*dataAckStruct)
				if ok {
					s.remoteWindowSize.Store(uint32(das.windowSize))
				}
			}
		}
		s.ackOnDataRecv.Store(true)
	default:
		return fmt.Errorf("unsupported transport protocol %v", s.transportProtocol)
	}

	if !s.isClient && seg.metadata.Protocol() == openSessionRequest {
		if s.isState(sessionAttached) {
			// Server needs to send open session response.
			// Check user quota if we can identify the user.
			s.oLock.Lock()
			if userName := s.UserName(); userName != "" {
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
					seq:       s.nextSend.Load(),
				},
				transport: s.transportProtocol,
			}
			s.nextSend.Add(1)
			if log.IsLevelEnabled(log.TraceLevel) {
				log.Tracef("%v writing open session response", s)
			}
			if !s.sendQueue.Insert(seg4) {
				s.oLock.Unlock()
				return fmt.Errorf("inputData() failed: insert %v to send queue failed", seg4)
			} else {
				s.oLock.Unlock()
				s.forwardStateTo(sessionEstablished)
			}
		}
	}
	return nil
}

func (s *Session) inputAck(seg *segment) error {
	switch s.transportProtocol {
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
			s.rttStat.UpdateRTT(time.Duration(time.Now().UnixMicro()-seg2.txTime) * time.Microsecond)
			s.cubicSendAlgorithm.OnAck()
			seq, _ := seg2.Seq()
			ackedPackets = append(ackedPackets, congestion.AckedPacketInfo{
				PacketNumber:     int64(seq),
				BytesAcked:       int64(packetOverhead + len(seg2.payload)),
				ReceiveTimestamp: time.Now(),
			})
		}
		if len(ackedPackets) > 0 {
			s.bbrSendAlgorithm.OnCongestionEvent(priorInFlight, time.Now(), ackedPackets, nil)
		}
		s.remoteWindowSize.Store(uint32(das.windowSize))

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
		return fmt.Errorf("unsupported transport protocol %v", s.transportProtocol)
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
				seq:        s.nextSend.Load(),
				statusCode: uint8(statusOK),
				payloadLen: 0,
			},
			transport: s.transportProtocol,
		}
		s.nextSend.Add(1)
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
	switch s.transportProtocol {
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
		return fmt.Errorf("unsupported transport protocol %v", s.transportProtocol)
	}
	seq, _ := seg.Seq()
	s.lastSend.Store(seq)
	s.lastTXTime.Store(time.Now().UnixMicro())
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
		closeRequestSeq := s.nextSend.Load()
		seg := &segment{
			metadata: &sessionStruct{
				baseStruct: baseStruct{
					protocol: uint8(closeSessionRequest),
				},
				sessionID:  s.id,
				seq:        closeRequestSeq,
				statusCode: uint8(s.status),
			},
			transport: s.transportProtocol,
		}
		s.nextSend.Add(1)

		var gracefulCloseSuccess bool
		if gracefulClose {
			if !s.sendQueue.Insert(seg) {
				s.oLock.Unlock()
			} else {
				s.oLock.Unlock()
				for i := 0; i < 1000; i++ {
					time.Sleep(time.Millisecond)
					if s.lastSend.Load() >= closeRequestSeq {
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

// sendWindowSize determines how many more packets this session can send.
func (s *Session) sendWindowSize() int {
	return mathext.Max(0, mathext.Min(int(s.cubicSendAlgorithm.CongestionWindowSize())-s.sendBuf.Len(), int(s.remoteWindowSize.Load())))
}

// receiveWindowSize determines how many more packets this session can receive.
func (s *Session) receiveWindowSize() int {
	return mathext.Max(0, int(s.cubicSendAlgorithm.CongestionWindowSize())-s.recvBuf.Len())
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
