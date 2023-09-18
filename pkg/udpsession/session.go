package udpsession

import (
	"context"
	crand "crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	mrand "math/rand"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/enfein/mieru/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/pkg/cipher"
	"github.com/enfein/mieru/pkg/kcp"
	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/metrics"
	"github.com/enfein/mieru/pkg/recording"
	"github.com/enfein/mieru/pkg/replay"
	"github.com/enfein/mieru/pkg/rng"
	"github.com/enfein/mieru/pkg/schedule"
	"github.com/enfein/mieru/pkg/stderror"
	"github.com/enfein/mieru/pkg/util"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	// kcpReservedSize is the reserved size at the front of KCP buffer.
	kcpReservedSize = 0

	// acceptBacklog is the maximum number of pending accept requests of a listener.
	acceptBacklog = 1024

	// netConnReadTimeout is the network connection read timeout.
	// The network connection will return error if no response for this period after request is sent.
	netConnReadTimeout = 10 * time.Second

	// maxIdleDuration is the maximum idle duration before the session is auto closed.
	maxIdleDuration = 120 * time.Second

	// maxHeartbeatDuration is the maximum duration before a keepalive packet is sent.
	maxHeartbeatDuration = 30 * time.Second
)

var (
	// replayCache records possible replay in UDP sessions.
	replayCache = replay.NewCache(1024*1024*1024, 8*time.Minute)

	// globalMTU is the L2 MTU used by all the UDP sessions.
	globalMTU int32 = util.DefaultMTU
)

var (
	// Number of KCP packet input errors.
	KCPInErrors = metrics.RegisterMetric("errors", "KCPInErrors")

	// Number of KCP packet receive errors.
	KCPReceiveErrors = metrics.RegisterMetric("errors", "KCPReceiveErrors")

	// Number of KCP packet send errors.
	KCPSendErrors = metrics.RegisterMetric("errors", "KCPSendErrors")

	// Number of UDP read errors.
	UDPInErrors = metrics.RegisterMetric("errors", "UDPInErrors")
)

type (
	// UDPSession defines a KCP session implemented by UDP.
	UDPSession struct {
		mu sync.Mutex

		conn    net.PacketConn     // the underlying packet connection
		ownConn bool               // true if we created the conn, false if provided by the listener
		kcp     *kcp.KCP           // KCP ARQ protocol
		block   cipher.BlockCipher // block encryption object

		// keepalive
		idleDeadline  time.Time // the time when session will be auto closed due to idle
		heartbeatTime time.Time // client only: the time when a heartbeat packet should be sent to avoid being closed

		// kcp receiving is based on packets, recvBuf turns packets into stream.
		recvBuf    []byte
		recvBufPtr []byte

		// settings
		remote       net.Addr      // remote peer address
		rdeadline    time.Time     // read deadline
		wdeadline    time.Time     // write deadline
		idleDuration time.Duration // idle duration
		headerSize   int           // the header size additional to a KCP frame
		ackNoDelay   bool          // send ack immediately for each incoming packet(testing purpose)
		writeDelay   bool          // delay kcp.flush() for Write() for bulk transfer

		// notifications
		die          chan struct{} // notify current session has Closed
		dieOnce      sync.Once
		chReadEvent  chan struct{} // notify Read() can be called without blocking
		chWriteEvent chan struct{} // notify Write() can be called without blocking

		// socket error handling
		socketReadError      atomic.Value
		socketWriteError     atomic.Value
		chSocketReadError    chan struct{}
		chSocketWriteError   chan struct{}
		socketReadErrorOnce  sync.Once
		socketWriteErrorOnce sync.Once

		// txqueue is another queue outside KCP to do additional processing before sending packets on wire.
		txqueue []ipMessage

		inBytes  *metrics.Metric
		outBytes *metrics.Metric

		recordingEnabled bool
		recordedPackets  recording.Records
	}

	// ipMessage is a simplified ipv4.Message or ipv6.Message.
	ipMessage struct {
		buffer []byte
		addr   net.Addr
	}
)

// newUDPSession create a new udp session for client or server, depending on if listener is nil.
func newUDPSession(conv uint32, conn net.PacketConn, ownConn bool, remote net.Addr, block cipher.BlockCipher) *UDPSession {
	sess := new(UDPSession)
	sess.die = make(chan struct{})
	sess.chReadEvent = make(chan struct{}, 1)
	sess.chWriteEvent = make(chan struct{}, 1)
	sess.chSocketReadError = make(chan struct{})
	sess.chSocketWriteError = make(chan struct{})
	sess.remote = remote
	sess.conn = conn
	sess.ownConn = ownConn
	sess.block = block
	sess.idleDuration = maxIdleDuration
	sess.recvBuf = make([]byte, kcp.MaxBufSize)
	sess.ackNoDelay = false
	sess.recordingEnabled = false
	sess.recordedPackets = recording.NewRecords()

	if _, ok := conn.(*net.UDPConn); ok {
		_, err := net.ResolveUDPAddr("udp", conn.LocalAddr().String())
		if err == nil {
			if sess.IsClient() {
				log.Debugf("creating new client UDP session [%v - %v]", conn.LocalAddr(), remote)
			} else {
				log.Debugf("creating new server UDP session [%v - %v]", conn.LocalAddr(), remote)
			}
		}
	}

	sess.headerSize += kcp.OuterHeaderSize

	sess.kcp = kcp.NewKCP(conv, func(buf []byte, size int) {
		// Make sure the payload at least has the KCP header.
		if size >= kcp.IKCP_OVERHEAD {
			sess.outputCallback(buf[:size])
		}
	})

	sess.kcp.ReserveBytes(kcpReservedSize)

	if util.GetIPVersion(sess.remote.String()) == util.IPVersion4 {
		sess.SetMtu(int(globalMTU) - 20 - 8 - kcp.OuterHeaderSize)
	} else {
		sess.SetMtu(int(globalMTU) - 40 - 8 - kcp.OuterHeaderSize)
	}

	if sess.IsClient() {
		go sess.readLoop()
		metrics.ActiveOpens.Add(1)
	} else {
		metrics.PassiveOpens.Add(1)
	}

	schedule.System.Put(sess.periodicSendTask, time.Now())
	if sess.IsClient() {
		// Only client needs to send heartbeat to server.
		schedule.System.Put(sess.periodicHeartbeatTask, time.Now())
	}

	currEst := metrics.CurrEstablished.Add(1)
	maxConn := metrics.MaxConn.Load()
	if currEst > maxConn {
		metrics.MaxConn.Store(currEst)
	}

	return sess
}

// -------- UDPSession public methods --------

// Read implements net.Conn
func (s *UDPSession) Read(b []byte) (n int, err error) {
	// Clear read deadline after a read.
	defer s.SetReadDeadline(util.ZeroTime())
	for {
		s.mu.Lock()
		// When recvBufPtr has remaining data, copy from recvBufPtr.
		if len(s.recvBufPtr) > 0 {
			n = copy(b, s.recvBufPtr)
			s.recvBufPtr = s.recvBufPtr[n:]
			s.mu.Unlock()
			kcp.BytesReceived.Add(int64(n))
			return n, nil
		}

		// Otherwise, when KCP recv queue has data, copy from recv queue.
		if size := s.kcp.PeekSize(); size > 0 {
			// When b is large enough, receive data into b directly.
			if len(b) >= size {
				if _, err = s.kcp.Recv(b); err != nil {
					KCPReceiveErrors.Add(1)
				}
				s.mu.Unlock()
				kcp.BytesReceived.Add(int64(size))
				return size, nil
			}

			// When b is not large enough, first copy to recvbuf then copy to b.
			// If needed, resize recvBuf to guarantee a sufficient space.
			if cap(s.recvBuf) < size {
				s.recvBuf = make([]byte, size)
			}
			s.recvBuf = s.recvBuf[:size]
			if _, err = s.kcp.Recv(s.recvBuf); err != nil {
				KCPReceiveErrors.Add(1)
			}
			n = copy(b, s.recvBuf)
			s.recvBufPtr = s.recvBuf[n:]
			s.mu.Unlock()
			kcp.BytesReceived.Add(int64(n))
			return n, nil
		}

		// If no data from KCP is available,
		// set deadline for current reading operation.
		var timeout *time.Timer
		var c <-chan time.Time
		if !s.rdeadline.IsZero() {
			if time.Now().After(s.rdeadline) {
				s.mu.Unlock()
				return 0, fmt.Errorf("read deadline exceeded: %w", stderror.ErrTimeout)
			}

			delay := time.Until(s.rdeadline)
			timeout = time.NewTimer(delay)
			c = timeout.C
		}
		s.mu.Unlock()

		// Wait for read event or timeout or error.
		select {
		case <-s.chReadEvent:
			if timeout != nil {
				timeout.Stop()
				// New data is now available to read.
			}
		case <-c:
			return 0, fmt.Errorf("read timeout: %w", stderror.ErrTimeout)
		case <-s.chSocketReadError:
			return 0, s.socketReadError.Load().(error)
		case <-s.die:
			return 0, fmt.Errorf("read after UDP session is closed: %w", io.ErrClosedPipe)
		}
	}
}

// Write implements net.Conn
func (s *UDPSession) Write(b []byte) (n int, err error) {
	n, err = s.WriteBuffers([][]byte{b})
	// For client, set read deadline after a successful write.
	if err == nil && s.IsClient() {
		if err = s.SetReadDeadline(time.Now().Add(netConnReadTimeout)); err != nil {
			log.Warnf("UDP session SetReadDeadline() failed: %v", err)
		}
	}
	return
}

// WriteBuffers write a vector of byte slices to the underlying connection
func (s *UDPSession) WriteBuffers(v [][]byte) (n int, err error) {
	for {
		select {
		case <-s.chSocketWriteError:
			return 0, s.socketWriteError.Load().(error)
		case <-s.die:
			return 0, fmt.Errorf("write after UDP session is closed: %w", io.ErrClosedPipe)
		default:
		}

		s.mu.Lock()

		// Make sure write do not overflow the max sliding window on both side.
		waitsnd := s.kcp.WaitSendSize()
		if waitsnd < int(s.kcp.SendWindow()) && waitsnd < int(s.kcp.RemoteWindow()) {
			for _, b := range v {
				n += len(b)
				for {
					if len(b) <= int(s.kcp.MSS()) {
						if err = s.kcp.Send(b); err != nil {
							KCPSendErrors.Add(1)
						}
						break
					} else {
						if err = s.kcp.Send(b[:s.kcp.MSS()]); err != nil {
							KCPSendErrors.Add(1)
						}
						b = b[s.kcp.MSS():]
					}
				}
			}

			waitsnd = s.kcp.WaitSendSize()
			if waitsnd >= int(s.kcp.SendWindow()) || waitsnd >= int(s.kcp.RemoteWindow()) || !s.writeDelay {
				s.kcp.Output(false)
				s.uncork()
			}
			s.mu.Unlock()
			kcp.BytesSent.Add(int64(n))
			return n, err
		}

		var timeout *time.Timer
		var c <-chan time.Time
		if !s.wdeadline.IsZero() {
			if time.Now().After(s.wdeadline) {
				s.mu.Unlock()
				return 0, fmt.Errorf("write deadline exceeded: %w", stderror.ErrTimeout)
			}
			delay := time.Until(s.wdeadline)
			timeout = time.NewTimer(delay)
			c = timeout.C
		}
		s.mu.Unlock()

		// Wait for write event or timeout or error.
		select {
		case <-s.chWriteEvent:
			if timeout != nil {
				timeout.Stop()
				// Writing is now open.
			}
		case <-c:
			return 0, fmt.Errorf("write timeout: %w", stderror.ErrTimeout)
		case <-s.chSocketWriteError:
			return 0, s.socketWriteError.Load().(error)
		case <-s.die:
			return 0, fmt.Errorf("write after UDP session is closed: %w", io.ErrClosedPipe)
		}
	}
}

// Close closes the connection.
//
// To avoid deadlock, caller should never hold s.mu lock before calling this method.
func (s *UDPSession) Close() error {
	var once bool
	s.dieOnce.Do(func() {
		close(s.die)
		once = true
	})

	if once {
		log.Debugf("closing UDPSession [%v - %v]", s.LocalAddr(), s.RemoteAddr())
		metrics.CurrEstablished.Add(-1)

		s.mu.Lock()

		// Try best to send all queued messages.
		s.kcp.Output(false)
		s.uncork()

		// Release pending segments.
		s.kcp.ReleaseTX()

		s.mu.Unlock()

		if s.IsServer() {
			// Right now keep the session in record table. It will be removed by periodic clean task.
			return nil
		} else if s.ownConn {
			// client closes the connection to server.
			return s.conn.Close()
		} else {
			return nil
		}
	} else {
		return fmt.Errorf("close after UDP session is already closed: %w", io.ErrClosedPipe)
	}
}

// LocalAddr returns the local network address.
// The Addr returned is shared by all invocations of LocalAddr, so do not modify it.
func (s *UDPSession) LocalAddr() net.Addr {
	return s.conn.LocalAddr()
}

// RemoteAddr returns the remote network address.
// The Addr returned is shared by all invocations of RemoteAddr, so do not modify it.
func (s *UDPSession) RemoteAddr() net.Addr {
	return s.remote
}

// If this UDP session is owned by client.
func (s *UDPSession) IsClient() bool {
	return s.ownConn
}

// If this UDP session is owned by proxy server.
func (s *UDPSession) IsServer() bool {
	return !s.ownConn
}

// SetDeadline sets the deadline associated with the listener. A zero time value disables the deadline.
func (s *UDPSession) SetDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rdeadline = t
	s.wdeadline = t
	s.notifyReadEvent()
	s.notifyWriteEvent()
	return nil
}

// SetReadDeadline implements the Conn SetReadDeadline method.
func (s *UDPSession) SetReadDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rdeadline = t
	s.notifyReadEvent()
	return nil
}

// SetWriteDeadline implements the Conn SetWriteDeadline method.
func (s *UDPSession) SetWriteDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wdeadline = t
	s.notifyWriteEvent()
	return nil
}

// SetWriteDelay delays write for bulk transfer until the next update interval.
func (s *UDPSession) SetWriteDelay(delay bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeDelay = delay
}

// SetMtu sets the maximum transmission unit (not including UDP header).
func (s *UDPSession) SetMtu(mtu int) bool {
	if mtu > kcp.MaxMTU {
		log.Errorf("MTU %d is bigger than maximum value %d", mtu, kcp.MaxMTU)
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.kcp.SetMtu(mtu); err != nil {
		log.Errorf("KCP SetMtu(%d) failed: %v", mtu, err)
		return false
	}
	return true
}

// SetStreamMode toggles the stream mode on/off.
func (s *UDPSession) SetStreamMode(enable bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if enable {
		s.kcp.SetStreamMode(true)
	} else {
		s.kcp.SetStreamMode(false)
	}
}

// SetACKNoDelay changes ack flush option, set true to flush ack immediately.
func (s *UDPSession) SetACKNoDelay(nodelay bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ackNoDelay = nodelay
}

// SetPollIntervalMs set KCP polling interval in milliseconds.
func (s *UDPSession) SetPollIntervalMs(interval uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kcp.SetPollIntervalMs(interval)
}

// SetDSCP sets the 6bit DSCP field in IPv4 header, or 8bit Traffic Class in IPv6 header.
//
// It has no effect if it's accepted from Listener.
func (s *UDPSession) SetDSCP(dscp int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.IsServer() {
		return stderror.ErrInvalidOperation
	}

	if nc, ok := s.conn.(net.Conn); ok {
		var succeed bool
		if err := ipv4.NewConn(nc).SetTOS(dscp << 2); err == nil {
			succeed = true
		}
		if err := ipv6.NewConn(nc).SetTrafficClass(dscp); err == nil {
			succeed = true
		}

		if succeed {
			return nil
		}
	}
	return stderror.ErrInvalidOperation
}

// GetConv gets conversation id of a session
func (s *UDPSession) GetConv() uint32 {
	return s.kcp.ConversationID()
}

// -------- UDPSession private methods --------

// Client reads packets from the connection (forever).
func (s *UDPSession) readLoop() {
	buf := make([]byte, kcp.MaxBufSize)
	var src string
	for {
		if n, addr, err := s.conn.ReadFrom(buf); err == nil {
			// Make sure the packet is from the same source.
			// This can prevent replay attack.
			if src == "" {
				// Set source address from the first read.
				src = addr.String()
			} else if addr.String() != src {
				UDPInErrors.Add(1)
				continue
			}
			s.packetInput(buf[:n])
		} else {
			s.notifyReadError(fmt.Errorf("ReadFrom() failed: %w", err))
			return
		}
	}
}

// Process raw input packet.
func (s *UDPSession) packetInput(data []byte) {
	if log.IsLevelEnabled(log.TraceLevel) {
		log.Tracef("UDPSession %v: read %d bytes", s.LocalAddr(), len(data))
	}
	if s.recordingEnabled {
		s.recordedPackets.Append(data, recording.Ingress)
	}

	decrypted := false
	var err error
	if len(data) >= kcp.OuterHeaderSize {
		data, err = s.block.Decrypt(data)
		cipher.ClientDirectDecrypt.Add(1)
		if err != nil {
			log.Debugf("UDPSession %v: failed to decrypt input with %d bytes", s.LocalAddr(), len(data))
			cipher.ClientFailedDirectDecrypt.Add(1)
			return
		}
		decrypted = true
	}

	if decrypted && len(data) >= kcp.IKCP_OVERHEAD {
		s.inputToKCP(data)
	}
}

// Input decrypted packet to KCP.
func (s *UDPSession) inputToKCP(data []byte) {
	s.mu.Lock()
	if ret := s.kcp.Input(data, s.ackNoDelay); ret != nil {
		log.Debugf("UDPSession %v: KCP input rejected with err %v", s.LocalAddr(), ret)
		KCPInErrors.Add(1)
	}
	s.updateIdleDeadline(s.kcp.LastInputTime())
	if n := s.kcp.PeekSize(); n >= 0 {
		s.notifyReadEvent()
	}
	waitsnd := s.kcp.WaitSendSize()
	if waitsnd < int(s.kcp.SendWindow()) && waitsnd < int(s.kcp.RemoteWindow()) {
		s.notifyWriteEvent()
	}
	s.uncork()
	s.mu.Unlock()

	metrics.InBytes.Add(int64(len(data)))
	if s.inBytes != nil {
		s.inBytes.Add(int64(len(data)))
	}
}

// uncork sends data to txqueue (if there is any).
func (s *UDPSession) uncork() {
	if len(s.txqueue) > 0 {
		s.tx(s.txqueue)
		// Recycle segments in txqueue.
		for k := range s.txqueue {
			kcp.PktCachePool.Put(s.txqueue[k].buffer)
			s.txqueue[k].buffer = nil
		}
		s.txqueue = s.txqueue[:0]
	}
	if s.IsClient() {
		s.updateHeartbeatTime(s.kcp.LastOutputTime())
	}
}

// Sends all the packets in txqueue to the remote.
func (s *UDPSession) tx(txqueue []ipMessage) {
	nbytes := 0
	npkts := 0
	for k := range txqueue {
		// For 1% of packets, yield CPU to disturb the timing.
		if mrand.Intn(100) == 0 {
			runtime.Gosched()
		}
		if n, err := s.conn.WriteTo(txqueue[k].buffer, txqueue[k].addr); err == nil {
			nbytes += n
			npkts++
		} else {
			s.notifyWriteError(fmt.Errorf("WriteTo() failed: %w", err))
			break
		}
	}
	metrics.OutBytes.Add(int64(nbytes))
	if s.outBytes != nil {
		s.outBytes.Add(int64(nbytes))
	}
}

// Post-processing for sending a packet from kcp core, including
//
// 1. Encryption
// 2. TxQueue
// 3. Add to replay cache
// 4. Record output if enabled
func (s *UDPSession) outputCallback(buf []byte) {
	if log.IsLevelEnabled(log.TraceLevel) {
		log.Tracef("UDPSession %v: starting output %d bytes", s.LocalAddr(), len(buf))
	}

	// Encryption
	var encrypted []byte
	var err error
	encrypted, err = s.block.Encrypt(buf)
	if err != nil {
		log.Debugf("UDPSession %v: failed to encrypt %d bytes.", s.LocalAddr(), len(buf))
		return
	}

	// TxQueue and record output
	var msg ipMessage
	bts := kcp.PktCachePool.Get()[:len(encrypted)]
	copy(bts, encrypted)
	msg.buffer = bts
	msg.addr = s.remote
	s.txqueue = append(s.txqueue, msg)

	// Add egress packet to replay cache.
	if s.IsServer() {
		replayCache.IsDuplicate(bts[:kcp.OuterHeaderSize], replay.EmptyTag)
	}

	if s.recordingEnabled {
		s.recordedPackets.Append(bts, recording.Egress)
	}
}

func (s *UDPSession) updateIdleDeadline(lastInputTime time.Time) {
	s.idleDeadline = lastInputTime.Add(s.idleDuration)
}

func (s *UDPSession) updateHeartbeatTime(lastOutputTime time.Time) {
	long := int64(maxHeartbeatDuration)
	short := long / 3
	d := rng.IntRange64(short, long)
	s.heartbeatTime = lastOutputTime.Add(time.Duration(d))
}

// sess periodicSendTask to trigger protocol
func (s *UDPSession) periodicSendTask() {
	select {
	case <-s.die:
	default:
		s.mu.Lock()
		interval := s.kcp.Output(false)
		waitsnd := s.kcp.WaitSendSize()
		if waitsnd < int(s.kcp.SendWindow()) && waitsnd < int(s.kcp.RemoteWindow()) {
			s.notifyWriteEvent()
		}
		s.uncork()
		s.mu.Unlock()
		// Schedule next call.
		schedule.System.Put(
			s.periodicSendTask,
			time.Now().Add(time.Duration(interval)*time.Millisecond),
		)
	}
}

func (s *UDPSession) periodicHeartbeatTask() {
	select {
	case <-s.die:
	default:
		diff := time.Until(s.heartbeatTime)
		if int64(diff) > 0 {
			newTime := time.Now().Add(diff)
			schedule.System.Put(s.periodicHeartbeatTask, newTime)
			return
		}

		s.mu.Lock()
		s.kcp.SendHeartbeat()
		s.mu.Unlock()
		if log.IsLevelEnabled(log.TraceLevel) {
			log.Tracef("UDPSession %v: sent heartbeat message", s.LocalAddr())
		}

		schedule.System.Put(
			s.periodicHeartbeatTask,
			time.Now().Add(1*time.Second),
		)
	}
}

func (s *UDPSession) notifyReadEvent() {
	select {
	case s.chReadEvent <- struct{}{}:
	default:
	}
}

func (s *UDPSession) notifyWriteEvent() {
	select {
	case s.chWriteEvent <- struct{}{}:
	default:
	}
}

func (s *UDPSession) notifyReadError(err error) {
	s.socketReadErrorOnce.Do(func() {
		s.socketReadError.Store(err)
		close(s.chSocketReadError)
	})
}

func (s *UDPSession) notifyWriteError(err error) {
	s.socketWriteErrorOnce.Do(func() {
		s.socketWriteError.Store(err)
		close(s.chSocketWriteError)
	})
}

func (s *UDPSession) startRecording() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordingEnabled = true
}

func (s *UDPSession) stopRecording() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordingEnabled = false
}

// Listener defines a server which will be waiting to accept incoming UDP connections.
type Listener struct {
	users map[string]*appctlpb.User // registered users
	conn  net.PacketConn            // the underlying packet connection

	sessions    map[string]*UDPSession // all sessions accepted by this Listener
	sessionLock sync.RWMutex           // session lock
	chAccepts   chan *UDPSession       // Accept() backlog

	die     chan struct{} // notify the listener has closed
	dieOnce sync.Once

	// socket error handling
	socketReadError     atomic.Value
	chSocketReadError   chan struct{}
	socketReadErrorOnce sync.Once

	rdeadline atomic.Value // read deadline for Accept()
}

// -------- Listener public methods --------

// Accept implements the Accept() method in the net.Listener interface.
func (l *Listener) Accept() (net.Conn, error) {
	var timeout <-chan time.Time
	if tdeadline, ok := l.rdeadline.Load().(time.Time); ok && !tdeadline.IsZero() {
		timeout = time.After(time.Until(tdeadline))
	}

	select {
	case <-timeout:
		return nil, fmt.Errorf("AcceptKCP() timeout: %w", stderror.ErrTimeout)
	case c := <-l.chAccepts:
		return c, nil
	case <-l.chSocketReadError:
		return nil, l.socketReadError.Load().(error)
	case <-l.die:
		// Use raw io.ErrClosedPipe here so consumer can compare the error type.
		return nil, io.ErrClosedPipe
	}
}

// Close stops listening on the UDP address, and closes the socket.
func (l *Listener) Close() error {
	var once bool
	l.dieOnce.Do(func() {
		close(l.die)
		once = true
	})

	var err error
	if once {
		err = l.conn.Close()
		log.Infof("Listener %v: closed", l.Addr())
	} else {
		err = fmt.Errorf("close after listener is already closed: %w", io.ErrClosedPipe)
	}
	return err
}

// Addr returns the listener's network address.
func (l *Listener) Addr() net.Addr {
	return l.conn.LocalAddr()
}

// SetDSCP sets the 6bit DSCP field in IPv4 header, or 8bit Traffic Class in IPv6 header.
func (l *Listener) SetDSCP(dscp int) error {
	if nc, ok := l.conn.(net.Conn); ok {
		var succeed bool
		if err := ipv4.NewConn(nc).SetTOS(dscp << 2); err == nil {
			succeed = true
		}
		if err := ipv6.NewConn(nc).SetTrafficClass(dscp); err == nil {
			succeed = true
		}

		if succeed {
			return nil
		}
	}
	return stderror.ErrInvalidOperation
}

// SetDeadline sets the deadline associated with the listener. A zero time value disables the deadline.
func (l *Listener) SetDeadline(t time.Time) error {
	return l.SetReadDeadline(t)
	// SetWriteDeadline() is not supported.
}

// SetReadDeadline implements the Conn SetReadDeadline method.
func (l *Listener) SetReadDeadline(t time.Time) error {
	l.rdeadline.Store(t)
	return nil
}

// SetWriteDeadline implements the Conn SetWriteDeadline method.
// This method is not supported.
func (l *Listener) SetWriteDeadline(t time.Time) error {
	return stderror.ErrInvalidOperation
}

// -------- Listener private methods --------

// Server reads packets from the connection (forever).
func (l *Listener) readLoop() {
	buf := make([]byte, kcp.MaxBufSize)
	for {
		if n, from, err := l.conn.ReadFrom(buf); err == nil {
			l.packetInput(buf[:n], from)
		} else {
			l.notifyReadError(fmt.Errorf("ReadFrom() failed: %w", err))
			return
		}
	}
}

// Process raw input packet.
func (l *Listener) packetInput(raw []byte, addr net.Addr) {
	if log.IsLevelEnabled(log.TraceLevel) {
		log.Tracef("UDPSession Listener %v: read %d bytes from %s %s", l.Addr(), len(raw), addr.Network(), addr.String())
	}

	decrypted := false
	var data []byte
	var block cipher.BlockCipher
	var err error
	l.sessionLock.RLock()
	s, isKnownSession := l.sessions[addr.String()]
	l.sessionLock.RUnlock()

	if len(raw) >= kcp.OuterHeaderSize {
		if isKnownSession {
			// If this session is known to listener, directly use the cipher block to decrypt.
			block = s.block
			data, err = s.block.Decrypt(raw)
			cipher.ServerDirectDecrypt.Add(1)
			if err == nil {
				decrypted = true
			} else {
				cipher.ServerFailedDirectDecrypt.Add(1)
			}
		}
		if !decrypted {
			// If can't be decrypted because it is a new session, try decrypt with each registered user.
			for _, user := range l.users {
				var password []byte
				password, err = hex.DecodeString(user.GetHashedPassword())
				if err != nil {
					log.Debugf("unable to decode hashed password %q from user %q", user.GetHashedPassword(), user.GetName())
					continue
				}
				if len(password) == 0 {
					password = cipher.HashPassword([]byte(user.GetPassword()), []byte(user.GetName()))
				}
				block, data, err = cipher.TryDecrypt(raw, password, true)
				if err == nil {
					decrypted = true
					block.SetBlockContext(cipher.BlockContext{
						UserName: user.GetName(),
					})
					break
				}
			}
		}
	}

	if !decrypted {
		cipher.ServerFailedIterateDecrypt.Add(1)
		return
	}

	if len(data) >= kcp.IKCP_OVERHEAD {
		var conv, sn uint32
		conv = binary.LittleEndian.Uint32(data)
		sn = binary.LittleEndian.Uint32(data[kcp.IKCP_SN_OFFSET:])

		connWithSameAddr := false

		// This is an existing connection.
		if isKnownSession {
			// For an already established session, we need to put every packet into the replay cache.
			// Since AEAD is used, only the packet prefix is added to the replay cache.
			if replayCache.IsDuplicate(raw[:kcp.OuterHeaderSize], replay.EmptyTag) {
				replay.KnownSession.Add(1)
			}
			if conv == s.kcp.ConversationID() {
				s.inputToKCP(data)
			} else if sn == 0 {
				log.Debugf("another connection reused address %v, closing existing session", addr)
				s.Close()
				connWithSameAddr = true
				// The old session will be garbage collected after the reference
				// in l.sessions is replaced by the new session.
			}
		}

		// This is either a new connection, or a connection that reuses an old address.
		if s == nil || connWithSameAddr {
			// Do not let the new sessions overwhelm accept queue.
			if len(l.chAccepts) < cap(l.chAccepts) {
				if replayCache.IsDuplicate(raw[:kcp.OuterHeaderSize], replay.EmptyTag) && s == nil {
					// Found a replay attack. Don't establish the connection.
					replay.NewSession.Add(1)
					log.Debugf("found possible replay attack from %v", addr)
					return
				}
				s = newUDPSession(conv, l.conn, false, addr, block)
				s.SetPollIntervalMs(1)
				if block.BlockContext().UserName != "" {
					s.inBytes = metrics.RegisterMetric(fmt.Sprintf(metrics.UserMetricGroupFormat, block.BlockContext().UserName), metrics.UserMetricInBytes)
					s.outBytes = metrics.RegisterMetric(fmt.Sprintf(metrics.UserMetricGroupFormat, block.BlockContext().UserName), metrics.UserMetricOutBytes)
				}
				s.inputToKCP(data)
				l.sessionLock.Lock()
				l.sessions[addr.String()] = s
				l.sessionLock.Unlock()
				l.chAccepts <- s
			}
		}
	}
}

func (l *Listener) periodCleanTask() {
	l.sessionLock.Lock()
	defer l.sessionLock.Unlock()
	if log.IsLevelEnabled(log.TraceLevel) {
		log.Tracef("UDPSession Listener %v: running periodic clean task with %d sessions", l.Addr(), len(l.sessions))
	}
	for addr, s := range l.sessions {
		if !s.idleDeadline.IsZero() && s.idleDeadline.Before(time.Now()) {
			log.Debugf("closing UDP session [%v - %v] due to timeout", s.conn.LocalAddr(), addr)
			s.Close()
			delete(l.sessions, addr)
		}
	}
	schedule.System.Put(l.periodCleanTask, time.Now().Add(5*time.Second))
}

func (l *Listener) notifyReadError(err error) {
	l.socketReadErrorOnce.Do(func() {
		l.socketReadError.Store(err)
		close(l.chSocketReadError)

		// propagate read error to all sessions
		l.sessionLock.RLock()
		for _, s := range l.sessions {
			s.notifyReadError(err)
		}
		l.sessionLock.RUnlock()
	})
}

// -------- other public functions --------

// ListenWithOptions listens for incoming KCP packets addressed to the local address laddr
// on the network "udp" with packet encryption.
func ListenWithOptions(laddr string, users map[string]*appctlpb.User) (*Listener, error) {
	listenAddr, err := net.ResolveUDPAddr("udp", laddr)
	if err != nil {
		return nil, fmt.Errorf("net.ResolveUDPAddr() failed: %w", err)
	}
	conn, err := net.ListenUDP("udp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("net.ListenUDP() failed: %w", err)
	}

	l := new(Listener)
	l.users = users
	l.conn = conn
	l.sessions = make(map[string]*UDPSession)
	l.chAccepts = make(chan *UDPSession, acceptBacklog)
	l.die = make(chan struct{})
	l.chSocketReadError = make(chan struct{})
	schedule.System.Put(l.periodCleanTask, time.Now().Add(5*time.Second))
	go l.readLoop()
	return l, nil
}

// DialWithOptions connects to the remote address "raddr" on the network "udp"
// with packet encryption. If "laddr" is empty, an automatic address is used.
// "block" is the block encryption algorithm to encrypt packets.
func DialWithOptions(ctx context.Context, network, laddr, raddr string, block cipher.BlockCipher) (*UDPSession, error) {
	switch network {
	case "udp", "udp4", "udp6":
	default:
		return nil, fmt.Errorf("network %s not supported", network)
	}
	udpRemoteAddr, err := net.ResolveUDPAddr("udp", raddr)
	if err != nil {
		return nil, fmt.Errorf("net.ResolveUDPAddr() failed: %w", err)
	}
	var udpLocalAddr *net.UDPAddr
	if laddr != "" {
		udpLocalAddr, err = net.ResolveUDPAddr("udp", laddr)
		if err != nil {
			return nil, fmt.Errorf("net.ResolveUDPAddr() failed: %w", err)
		}
	}

	conn, err := net.ListenUDP(network, udpLocalAddr)
	if err != nil {
		return nil, fmt.Errorf("net.ListenUDP() failed: %w", err)
	}

	var convid uint32
	if err = binary.Read(crand.Reader, binary.LittleEndian, &convid); err != nil {
		return nil, fmt.Errorf("binary.Read() failed: %w", err)
	}
	return newUDPSession(convid, conn, true, udpRemoteAddr, block), nil
}

// DialWithOptionsReturnConn calls DialWithOptions and returns a generic net.Conn.
func DialWithOptionsReturnConn(ctx context.Context, network, laddr, raddr string, block cipher.BlockCipher) (net.Conn, error) {
	return DialWithOptions(ctx, network, laddr, raddr, block)
}

// SetGlobalMTU adjust the L2 MTU of all UDP sessions.
// It does nothing if the MTU value is out of range.
func SetGlobalMTU(mtu int) {
	if mtu >= 1280 && mtu <= 1500 {
		atomic.StoreInt32(&globalMTU, int32(mtu))
		log.Infof("UDP session MTU is set to %d", int32(mtu))
	}
}
