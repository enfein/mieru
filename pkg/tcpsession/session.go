// Copyright (C) 2022  mieru authors
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

package tcpsession

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
	"time"

	"github.com/enfein/mieru/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/pkg/cipher"
	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/metrics"
	"github.com/enfein/mieru/pkg/netutil"
	"github.com/enfein/mieru/pkg/recording"
	"github.com/enfein/mieru/pkg/replay"
	"github.com/enfein/mieru/pkg/rng"
	"github.com/enfein/mieru/pkg/stderror"
)

const (
	// MaxPayloadSize is the maximum size of a single TCP payload
	// before encryption. It may include a padding inside.
	MaxPayloadSize = 1024 * 16

	// PayloadOverhead is the overhead of decrypted payload.
	// This overhead contains 2 bytes of useful payload length and 2 bytes of
	// useful + padding payload length.
	PayloadOverhead = 4

	baseWriteChunkSize = 9000

	maxWriteChunkSize = MaxPayloadSize - PayloadOverhead

	// acceptBacklog is the maximum number of pending accept requests of a listener.
	acceptBacklog = 1024
)

var (
	// replayCache records possible replay in TCP sessions.
	replayCache = replay.NewCache(1024*1024*1024, 8*time.Minute)

	// maxPaddingSize is the maximum size of padding added to a single TCP payload.
	maxPaddingSize = 256 + rng.FixedInt(256)
)

var (
	// Number of TCP receive errors.
	TCPReceiveErrors = metrics.RegisterMetric("errors", "TCPReceiveErrors")

	// Number of TCP send errors.
	TCPSendErrors = metrics.RegisterMetric("errors", "TCPSendErrors")
)

// TCPSession defines a TCP session.
type TCPSession struct {
	net.Conn             // the network connection associated to the TCPSession
	isClient  bool       // if this TCPSession is created by a client (not listener)
	sendMutex sync.Mutex // mutex used when write data to the connection
	recvMutex sync.Mutex // mutex used when read data from the connection

	send cipher.BlockCipher // block cipher used to encrypt outgoing data
	recv cipher.BlockCipher // block cipher used to decrypt incoming data

	// Candidates are block ciphers that can be used to encrypt or decrypt data.
	// When isClient is true, there must be exactly 1 element in the slice.
	candidates []cipher.BlockCipher

	recvBuf    []byte // data received from network but not read by caller
	recvBufPtr []byte // reading position of received data

	inBytes  *metrics.Metric
	outBytes *metrics.Metric

	recordingEnabled bool
	recordedPackets  recording.Records
}

func newTCPSession(conn net.Conn, isClient bool, blocks []cipher.BlockCipher) *TCPSession {
	if isClient {
		log.Debugf("creating new client TCP session [%v - %v]", conn.LocalAddr(), conn.RemoteAddr())
		metrics.ActiveOpens.Add(1)
	} else {
		log.Debugf("creating new server TCP session [%v - %v]", conn.LocalAddr(), conn.RemoteAddr())
		metrics.PassiveOpens.Add(1)
	}
	currEst := metrics.CurrEstablished.Add(1)
	maxConn := metrics.MaxConn.Load()
	if currEst > maxConn {
		metrics.MaxConn.Store(currEst)
	}
	return &TCPSession{
		Conn:             conn,
		isClient:         isClient,
		candidates:       blocks,
		recvBuf:          make([]byte, MaxPayloadSize),
		recordingEnabled: false,
		recordedPackets:  recording.NewRecords(),
	}
}

// Read overrides the Read() method in the net.Conn interface.
func (s *TCPSession) Read(b []byte) (n int, err error) {
	s.recvMutex.Lock()
	defer s.recvMutex.Unlock()
	var errType stderror.ErrorType
	n, err, errType = s.readInternal(b)
	if err != nil {
		if stderror.IsEOF(err) || stderror.IsClosed(err) {
			// EOF must be reported back to caller as is.
			// Closed pipe is also considered as EOF.
			return n, io.EOF
		}
		TCPReceiveErrors.Add(1)
		log.Debugf("TCP session [%v - %v] Read error: %v", s.LocalAddr(), s.RemoteAddr(), err)
		if errType == stderror.CRYPTO_ERROR {
			// This looks like an attack.
			// Continue to read some data from the TCP connection before closing.
			s.readAfterError()
		}
		return n, err
	}
	return n, nil
}

// Write overrides the Write() method in the net.Conn interface.
func (s *TCPSession) Write(b []byte) (n int, err error) {
	s.sendMutex.Lock()
	defer s.sendMutex.Unlock()
	n, err = s.writeInternal(b)
	if err != nil {
		TCPSendErrors.Add(1)
		log.Debugf("TCP session [%v - %v] Write error: %v", s.LocalAddr(), s.RemoteAddr(), err)
		return n, err
	}
	return n, nil
}

// Close overrides the Close() method in the net.Conn interface.
func (s *TCPSession) Close() error {
	log.Debugf("closing TCP session [%v - %v]", s.LocalAddr(), s.RemoteAddr())
	metrics.CurrEstablished.Add(-1)
	return s.Conn.Close()
}

func (s *TCPSession) readInternal(b []byte) (n int, err error, errType stderror.ErrorType) {
	// Read from recvBufPtr if possible.
	if len(s.recvBufPtr) > 0 {
		n = copy(b, s.recvBufPtr)
		s.recvBufPtr = s.recvBufPtr[n:]
		return n, nil, stderror.NO_ERROR
	}

	// Read encrypted payload length.
	readLen := 2 + cipher.DefaultOverhead
	if s.recv == nil {
		// For the first Read, also include nonce.
		readLen += cipher.DefaultNonceSize
	}
	encryptedLen := make([]byte, readLen)
	if _, err := io.ReadFull(s.Conn, encryptedLen); err != nil {
		return 0, fmt.Errorf("read %d bytes from TCPSession failed: %w", readLen, err), stderror.NETWORK_ERROR
	}
	if s.recordingEnabled {
		s.recordedPackets.Append(encryptedLen, recording.Ingress)
	}
	metrics.InBytes.Add(int64(readLen))
	var decryptedLen []byte
	firstRead := false
	if s.recv == nil && s.isClient {
		s.recv = s.candidates[0].Clone()
		firstRead = true
		if s.recv.BlockContext().UserName != "" {
			s.inBytes = metrics.RegisterMetric(fmt.Sprintf(metrics.UserMetricGroupFormat, s.recv.BlockContext().UserName), metrics.UserMetricInBytes)
		}
	}
	if s.recv == nil {
		var peerBlock cipher.BlockCipher
		peerBlock, decryptedLen, err = cipher.SelectDecrypt(encryptedLen, cipher.CloneBlockCiphers(s.candidates))
		if err != nil {
			cipher.ServerFailedIterateDecrypt.Add(1)
			return 0, fmt.Errorf("cipher.SelectDecrypt() failed: %w", err), stderror.CRYPTO_ERROR
		}
		s.recv = peerBlock.Clone()
		firstRead = true
		if s.recv.BlockContext().UserName != "" {
			s.inBytes = metrics.RegisterMetric(fmt.Sprintf(metrics.UserMetricGroupFormat, s.recv.BlockContext().UserName), metrics.UserMetricInBytes)
		}
	} else {
		decryptedLen, err = s.recv.Decrypt(encryptedLen)
		if s.isClient {
			cipher.ClientDirectDecrypt.Add(1)
		} else {
			cipher.ServerDirectDecrypt.Add(1)
		}
		if err != nil {
			if s.isClient {
				cipher.ClientFailedDirectDecrypt.Add(1)
			} else {
				cipher.ServerFailedDirectDecrypt.Add(1)
			}
			return 0, fmt.Errorf("Decrypt() failed: %w", err), stderror.CRYPTO_ERROR
		}
	}
	if s.inBytes != nil {
		s.inBytes.Add(int64(readLen))
	}
	if !s.isClient && replayCache.IsDuplicate(encryptedLen[:cipher.DefaultOverhead], replay.EmptyTag) {
		if firstRead {
			replay.NewSession.Add(1)
			return 0, fmt.Errorf("found possible replay attack from %v", s.Conn.RemoteAddr()), stderror.REPLAY_ERROR
		} else {
			replay.KnownSession.Add(1)
		}
	}
	readLen = int(binary.LittleEndian.Uint16(decryptedLen))
	if readLen > MaxPayloadSize {
		return 0, fmt.Errorf(stderror.SegmentSizeTooBig), stderror.PROTOCOL_ERROR
	}
	readLen += cipher.DefaultOverhead

	// Read encrypted payload.
	encryptedPayload := make([]byte, readLen)
	if _, err := io.ReadFull(s.Conn, encryptedPayload); err != nil {
		return 0, fmt.Errorf("read %d bytes from TCPSession failed: %w", readLen, err), stderror.NETWORK_ERROR
	}
	if s.recordingEnabled {
		s.recordedPackets.Append(encryptedPayload, recording.Ingress)
	}
	metrics.InBytes.Add(int64(readLen))
	if s.inBytes != nil {
		s.inBytes.Add(int64(readLen))
	}
	decryptedPayload, err := s.recv.Decrypt(encryptedPayload)
	if s.isClient {
		cipher.ClientDirectDecrypt.Add(1)
	} else {
		cipher.ServerDirectDecrypt.Add(1)
	}
	if err != nil {
		if s.isClient {
			cipher.ClientFailedDirectDecrypt.Add(1)
		} else {
			cipher.ServerFailedDirectDecrypt.Add(1)
		}
		return 0, fmt.Errorf("Decrypt() failed: %w", err), stderror.CRYPTO_ERROR
	}
	if !s.isClient && replayCache.IsDuplicate(encryptedPayload[:cipher.DefaultOverhead], replay.EmptyTag) {
		replay.KnownSession.Add(1)
	}

	// Extract useful payload from decrypted payload.
	if len(decryptedPayload) < PayloadOverhead {
		return 0, stderror.ErrInvalidArgument, stderror.PROTOCOL_ERROR
	}
	usefulSize := int(binary.LittleEndian.Uint16(decryptedPayload))
	totalSize := int(binary.LittleEndian.Uint16(decryptedPayload[2:]))
	if usefulSize > totalSize || totalSize+PayloadOverhead != len(decryptedPayload) {
		return 0, stderror.ErrInvalidArgument, stderror.PROTOCOL_ERROR
	}
	if log.IsLevelEnabled(log.TraceLevel) {
		log.Tracef("TCPSession received %d bytes actual payload", usefulSize)
	}

	// When b is large enough, receive data into b directly.
	if len(b) >= usefulSize {
		return copy(b, decryptedPayload[PayloadOverhead:PayloadOverhead+usefulSize]), nil, stderror.NO_ERROR
	}

	// When b is not large enough, first copy to recvbuf then copy to b.
	// If needed, resize recvBuf to guarantee a sufficient space.
	if cap(s.recvBuf) < usefulSize {
		s.recvBuf = make([]byte, usefulSize)
	}
	s.recvBuf = s.recvBuf[:usefulSize]
	copy(s.recvBuf, decryptedPayload[PayloadOverhead:PayloadOverhead+usefulSize])
	n = copy(b, s.recvBuf)
	s.recvBufPtr = s.recvBuf[n:]
	return n, nil, stderror.NO_ERROR
}

func (s *TCPSession) writeInternal(b []byte) (n int, err error) {
	n = len(b)
	if len(b) <= maxWriteChunkSize {
		return s.writeChunk(b)
	}
	for len(b) > 0 {
		sizeToSend := rng.IntRange(baseWriteChunkSize, maxWriteChunkSize)
		if sizeToSend > len(b) {
			sizeToSend = len(b)
		}
		if _, err = s.writeChunk(b[:sizeToSend]); err != nil {
			return 0, err
		}
		b = b[sizeToSend:]
	}
	return n, nil
}

func (s *TCPSession) writeChunk(b []byte) (n int, err error) {
	if len(b) > maxWriteChunkSize {
		return 0, fmt.Errorf(stderror.SegmentSizeTooBig)
	}

	// Construct the payload with padding.
	paddingSizeLimit := MaxPayloadSize - PayloadOverhead - len(b)
	if paddingSizeLimit > maxPaddingSize {
		paddingSizeLimit = maxPaddingSize
	}
	paddingSize := rng.Intn(paddingSizeLimit)
	payload := make([]byte, PayloadOverhead+len(b)+paddingSize)
	binary.LittleEndian.PutUint16(payload, uint16(len(b)))
	binary.LittleEndian.PutUint16(payload[2:], uint16(len(b)+paddingSize))
	copy(payload[PayloadOverhead:], b)
	if paddingSize > 0 {
		crand.Read(payload[PayloadOverhead+len(b):])
	}

	// Create send block cipher if needed.
	if s.send == nil {
		if s.isClient {
			s.send = s.candidates[0].Clone()
		} else {
			if s.recv != nil {
				s.send = s.recv.Clone()
				s.send.SetImplicitNonceMode(false) // clear implicit nonce
				s.send.SetImplicitNonceMode(true)
			} else {
				return 0, fmt.Errorf("recv cipher is nil")
			}
		}
		if s.send.BlockContext().UserName != "" {
			s.outBytes = metrics.RegisterMetric(fmt.Sprintf(metrics.UserMetricGroupFormat, s.send.BlockContext().UserName), metrics.UserMetricOutBytes)
		}
	}

	// Create encrypted payload length.
	plaintextLen := make([]byte, 2)
	binary.LittleEndian.PutUint16(plaintextLen, uint16(len(payload)))
	encryptedLen, err := s.send.Encrypt(plaintextLen)
	if err != nil {
		return 0, fmt.Errorf("Encrypt() failed: %w", err)
	}

	// Create encrypted payload.
	encryptedPayload, err := s.send.Encrypt(payload)
	if err != nil {
		return 0, fmt.Errorf("Encrypted() failed: %w", err)
	}

	// Add egress encrypted length and encrypted payload to replay cache.
	if !s.isClient {
		replayCache.IsDuplicate(encryptedLen[:cipher.DefaultOverhead], replay.EmptyTag)
		replayCache.IsDuplicate(encryptedPayload[:cipher.DefaultOverhead], replay.EmptyTag)
	}

	// Send encrypted payload length + encrypted payload.
	// For 1% of packets, yield CPU to disturb the timing.
	dataToSend := append(encryptedLen, encryptedPayload...)
	if mrand.Intn(100) == 0 {
		runtime.Gosched()
	}
	if _, err := io.WriteString(s.Conn, string(dataToSend)); err != nil {
		return 0, fmt.Errorf("io.WriteString() failed: %w", err)
	}
	if s.recordingEnabled {
		s.recordedPackets.Append(encryptedLen, recording.Egress)
		s.recordedPackets.Append(encryptedPayload, recording.Egress)
	}
	metrics.OutBytes.Add(int64(len(dataToSend)))
	metrics.OutPaddingBytes.Add(int64(paddingSize))
	if s.outBytes != nil {
		s.outBytes.Add(int64(len(dataToSend)))
	}
	if log.IsLevelEnabled(log.TraceLevel) {
		log.Tracef("TCPSession wrote %d bytes to the network", len(dataToSend))
	}
	return len(b), nil
}

// readAfterError continues to read some data from the TCP connection after
// an error happened due to possible attack.
func (s *TCPSession) readAfterError() {
	// Set TCP read deadline to avoid being blocked forever.
	timeoutMillis := rng.IntRange(1000, 5000)
	timeoutMillis += rng.FixedInt(60000) // Maximum 60 seconds.
	s.Conn.SetReadDeadline(time.Now().Add(time.Duration(timeoutMillis) * time.Millisecond))

	// Determine the read buffer size.
	bufSizeType := rng.FixedInt(4)
	bufSize := 1 << (12 + bufSizeType) // 4, 8, 16, 32 KB
	buf := make([]byte, bufSize)

	// Determine the number of bytes to read.
	// Minimum 2 bytes, maximum 1280 bytes.
	min := rng.IntRange(2, 1026)
	min += rng.FixedInt(256)

	n, err := io.ReadAtLeast(s.Conn, buf, min)
	if err != nil {
		log.Debugf("TCP session [%v - %v] read after TCP error failed to complete: %v", s.LocalAddr(), s.RemoteAddr(), err)
	} else {
		log.Debugf("TCP session [%v - %v] read at least %d bytes after TCP error", s.LocalAddr(), s.RemoteAddr(), n)
	}
}

func (s *TCPSession) startRecording() {
	s.recvMutex.Lock()
	defer s.recvMutex.Unlock()
	s.sendMutex.Lock()
	defer s.sendMutex.Unlock()
	s.recordingEnabled = true
}

func (s *TCPSession) stopRecording() {
	s.recvMutex.Lock()
	defer s.recvMutex.Unlock()
	s.sendMutex.Lock()
	defer s.sendMutex.Unlock()
	s.recordingEnabled = false
}

type TCPSessionListener struct {
	net.Listener

	users map[string]*appctlpb.User // registered users

	startOnce   sync.Once
	chAccept    chan net.Conn
	chAcceptErr chan error
	die         chan struct{}
}

// Start starts to accept incoming TCP connections.
func (l *TCPSessionListener) Start() {
	l.startOnce.Do(func() {
		go l.acceptLoop()
	})
}

// WrapConn adds TCPSession into a raw network connection.
func (l *TCPSessionListener) WrapConn(conn net.Conn) net.Conn {
	var err error
	var blocks []cipher.BlockCipher
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
		blocksFromUser, err := cipher.BlockCipherListFromPassword(password, false)
		if err != nil {
			log.Debugf("unable to create block cipher of user %q", user.GetName())
			continue
		}
		for _, block := range blocksFromUser {
			block.SetBlockContext(cipher.BlockContext{
				UserName: user.GetName(),
			})
		}
		blocks = append(blocks, blocksFromUser...)
	}
	return newTCPSession(conn, false, blocks)
}

// Accept implements the Accept() method in the net.Listener interface.
func (l *TCPSessionListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.chAccept:
		return l.WrapConn(c), nil
	case <-l.die:
		// Use raw io.ErrClosedPipe here so consumer can compare the error type.
		return nil, io.ErrClosedPipe
	}
}

// Close closes the session manager.
func (l *TCPSessionListener) Close() error {
	close(l.die)
	return nil
}

// Addr returns the listener's network address.
func (l *TCPSessionListener) Addr() net.Addr {
	return l.Listener.Addr()
}

func (l *TCPSessionListener) acceptLoop() {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			l.chAcceptErr <- err
			return
		}
		l.chAccept <- conn
	}
}

// ListenWithOptions creates a new TCPSession listener.
func ListenWithOptions(laddr string, users map[string]*appctlpb.User) (*TCPSessionListener, error) {
	listenConfig := net.ListenConfig{
		Control: netutil.ReuseAddrPort,
	}
	l, err := listenConfig.Listen(context.Background(), "tcp", laddr)
	if err != nil {
		return nil, fmt.Errorf("net.ListenTCP() failed: %w", err)
	}
	listener := &TCPSessionListener{
		Listener:    l,
		users:       users,
		chAccept:    make(chan net.Conn, acceptBacklog),
		chAcceptErr: make(chan error),
		die:         make(chan struct{}),
	}
	listener.Start()
	return listener, nil
}

// DialWithOptions connects to the remote address "raddr" on the network "tcp"
// with packet encryption. If "laddr" is empty, an automatic address is used.
// "block" is the block encryption algorithm to encrypt packets.
func DialWithOptions(ctx context.Context, network, laddr, raddr string, block cipher.BlockCipher) (*TCPSession, error) {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return nil, fmt.Errorf("network %s not supported", network)
	}
	if block.IsStateless() {
		return nil, fmt.Errorf("block cipher should not be stateless")
	}
	dialer := net.Dialer{}
	if laddr != "" {
		tcpLocalAddr, err := net.ResolveTCPAddr(network, laddr)
		if err != nil {
			return nil, fmt.Errorf("net.ResolveTCPAddr() failed: %w", err)
		}
		dialer.LocalAddr = tcpLocalAddr
	}

	conn, err := dialer.DialContext(ctx, network, raddr)
	if err != nil {
		return nil, fmt.Errorf("DialContext() failed: %w", err)
	}
	return newTCPSession(conn, true, []cipher.BlockCipher{block}), nil
}

// DialWithOptionsReturnConn calls DialWithOptions and returns a generic net.Conn.
func DialWithOptionsReturnConn(ctx context.Context, network, laddr, raddr string, block cipher.BlockCipher) (net.Conn, error) {
	return DialWithOptions(ctx, network, laddr, raddr, block)
}
