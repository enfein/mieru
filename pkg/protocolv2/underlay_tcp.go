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
// along with this program.  If not, see <https://www.gnu.org/licenses/>

package protocolv2

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/enfein/mieru/pkg/cipher"
	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/metrics"
	"github.com/enfein/mieru/pkg/replay"
	"github.com/enfein/mieru/pkg/rng"
	"github.com/enfein/mieru/pkg/stderror"
	"github.com/enfein/mieru/pkg/util"
)

type TCPUnderlay struct {
	baseUnderlay
	conn *net.TCPConn

	send cipher.BlockCipher
	recv cipher.BlockCipher

	// Candidates are block ciphers that can be used to encrypt or decrypt data.
	// When isClient is true, there must be exactly 1 element in the slice.
	candidates []cipher.BlockCipher
}

var _ Underlay = &TCPUnderlay{}

var tcpReplayCache = replay.NewCache(4*1024*1024, 2*time.Minute)

// NewTCPUnderlay connects to the remote address "raddr" on the network "tcp"
// with packet encryption. If "laddr" is empty, an automatic address is used.
// "block" is the block encryption algorithm to encrypt packets.
func NewTCPUnderlay(ctx context.Context, network, laddr, raddr string, mtu int, block cipher.BlockCipher) (*TCPUnderlay, error) {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return nil, fmt.Errorf("network %s is not supported by TCP underlay", network)
	}
	if block.IsStateless() {
		return nil, fmt.Errorf("TCP block cipher must not be stateless")
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
	t := &TCPUnderlay{
		baseUnderlay: *newBaseUnderlay(true, mtu),
		conn:         conn.(*net.TCPConn),
		candidates:   []cipher.BlockCipher{block},
	}
	log.Debugf("Created new client TCP underlay %v", t)
	return t, nil
}

func (t *TCPUnderlay) String() string {
	if t.conn == nil {
		return "TCPUnderlay{}"
	}
	return fmt.Sprintf("TCPUnderlay{local=%v, remote=%v, mtu=%v, ipVersion=%v}", t.conn.LocalAddr(), t.conn.RemoteAddr(), t.mtu, t.IPVersion())
}

func (t *TCPUnderlay) Close() error {
	t.closeMutex.Lock()
	defer t.closeMutex.Unlock()
	select {
	case <-t.done:
		return nil
	default:
	}

	log.Debugf("Closing %v", t)
	t.baseUnderlay.Close()
	return t.conn.Close()
}

func (t *TCPUnderlay) Addr() net.Addr {
	return t.LocalAddr()
}

func (t *TCPUnderlay) IPVersion() util.IPVersion {
	if t.conn == nil {
		return util.IPVersionUnknown
	}
	if t.ipVersion == util.IPVersionUnknown {
		t.ipVersion = util.GetIPVersion(t.conn.LocalAddr().String())
	}
	return t.ipVersion
}

func (t *TCPUnderlay) TransportProtocol() util.TransportProtocol {
	return util.TCPTransport
}

func (t *TCPUnderlay) LocalAddr() net.Addr {
	if t.conn == nil {
		return util.NilNetAddr()
	}
	return t.conn.LocalAddr()
}

func (t *TCPUnderlay) RemoteAddr() net.Addr {
	if t.conn == nil {
		return util.NilNetAddr()
	}
	return t.conn.RemoteAddr()
}

func (t *TCPUnderlay) AddSession(s *Session, remoteAddr net.Addr) error {
	if err := t.baseUnderlay.AddSession(s, remoteAddr); err != nil {
		return err
	}
	s.conn = t // override base underlay
	close(s.ready)
	log.Debugf("Adding session %d to %v", s.id, t)

	s.wg.Add(2)
	go func() {
		if err := s.runInputLoop(context.Background()); err != nil && !stderror.IsEOF(err) && !stderror.IsClosed(err) {
			log.Debugf("%v runInputLoop(): %v", s, err)
		}
		s.wg.Done()
	}()
	go func() {
		if err := s.runOutputLoop(context.Background()); err != nil && !stderror.IsEOF(err) && !stderror.IsClosed(err) {
			log.Debugf("%v runOutputLoop(): %v", s, err)
		}
		s.wg.Done()
	}()
	return nil
}

func (t *TCPUnderlay) RunEventLoop(ctx context.Context) error {
	if t.conn == nil {
		return stderror.ErrNullPointer
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.done:
			return nil
		default:
		}
		seg, err, errType := t.readOneSegment()
		if err != nil {
			if errType == stderror.CRYPTO_ERROR || errType == stderror.REPLAY_ERROR {
				t.drainAfterError()
			}
			return fmt.Errorf("readOneSegment() failed: %w", err)
		}
		if log.IsLevelEnabled(log.TraceLevel) {
			log.Tracef("%v received %v", t, seg)
		}
		if isSessionProtocol(seg.metadata.Protocol()) {
			switch seg.metadata.Protocol() {
			case openSessionRequest:
				if err := t.onOpenSessionRequest(seg); err != nil {
					return fmt.Errorf("onOpenSessionRequest() failed: %w", err)
				}
			case openSessionResponse:
				if err := t.onOpenSessionResponse(seg); err != nil {
					return fmt.Errorf("onOpenSessionResponse() failed: %w", err)
				}
			case closeSessionRequest, closeSessionResponse:
				if err := t.onCloseSession(seg); err != nil {
					return fmt.Errorf("onCloseSession() failed: %w", err)
				}
			default:
				panic(fmt.Sprintf("Protocol %d is a session protocol but not recognized by TCP underlay", seg.metadata.Protocol()))
			}
		} else if isDataAckProtocol(seg.metadata.Protocol()) {
			das, _ := toDataAckStruct(seg.metadata)
			session, ok := t.sessionMap.Load(das.sessionID)
			if !ok {
				log.Debugf("Session %d is not registered to %v", das.sessionID, t)
				// Request the peer to close the session.
				closeReq := &segment{
					metadata: &sessionStruct{
						baseStruct: baseStruct{
							protocol: uint8(closeSessionRequest),
						},
						sessionID:  das.sessionID,
						seq:        das.unAckSeq,
						statusCode: 0,
						payloadLen: 0,
					},
					transport: t.TransportProtocol(),
				}
				if err := t.writeOneSegment(closeReq); err != nil {
					return fmt.Errorf("writeOneSegment() failed: %w", err)
				}
				continue
			}
			session.(*Session).recvChan <- seg
		} else {
			log.Debugf("Ignore unknown protocol %d", seg.metadata.Protocol())
		}
	}
}

func (t *TCPUnderlay) onOpenSessionRequest(seg *segment) error {
	if t.isClient {
		return stderror.ErrInvalidOperation
	}

	// Create a new session.
	sessionID := seg.metadata.(*sessionStruct).sessionID
	if sessionID == 0 {
		// 0 is reserved and can't be used.
		return fmt.Errorf("reserved session ID %d is used", sessionID)
	}
	_, found := t.sessionMap.Load(sessionID)
	if found {
		return fmt.Errorf("%v received open session request, but session ID %d is already used", t, sessionID)
	}
	session := NewSession(sessionID, t.isClient, t.MTU())
	t.AddSession(session, nil)
	session.recvChan <- seg
	t.readySessions <- session
	return nil
}

func (t *TCPUnderlay) onOpenSessionResponse(seg *segment) error {
	if !t.isClient {
		return stderror.ErrInvalidOperation
	}

	sessionID := seg.metadata.(*sessionStruct).sessionID
	session, found := t.sessionMap.Load(sessionID)
	if !found {
		return fmt.Errorf("session ID %d is not found", sessionID)
	}
	session.(*Session).recvChan <- seg
	return nil
}

func (t *TCPUnderlay) onCloseSession(seg *segment) error {
	ss := seg.metadata.(*sessionStruct)
	sessionID := ss.sessionID
	session, found := t.sessionMap.Load(sessionID)
	if !found {
		log.Debugf("%v received close session request or response, but session ID %d is not found", t, sessionID)
		return nil
	}
	s := session.(*Session)
	s.recvChan <- seg
	s.wg.Wait()
	t.RemoveSession(s)
	return nil
}

func (t *TCPUnderlay) readOneSegment() (*segment, error, stderror.ErrorType) {
	var firstRead bool
	var err error

	// Read encrypted metadata.
	readLen := MetadataLength + cipher.DefaultOverhead
	if t.recv == nil {
		// In the first Read, also include nonce.
		firstRead = true
		readLen += cipher.DefaultNonceSize
	}
	encryptedMeta := make([]byte, readLen)
	if _, err := io.ReadFull(t.conn, encryptedMeta); err != nil {
		return nil, fmt.Errorf("metadata: read %d bytes from TCPUnderlay failed: %w", readLen, err), stderror.NETWORK_ERROR
	}
	metrics.InBytes.Add(int64(len(encryptedMeta)))
	if tcpReplayCache.IsDuplicate(encryptedMeta[:cipher.DefaultOverhead], replay.EmptyTag) {
		if firstRead {
			replay.NewSession.Add(1)
			return nil, fmt.Errorf("found possible replay attack in %v", t), stderror.REPLAY_ERROR
		} else {
			replay.KnownSession.Add(1)
		}
	}

	// Decrypt metadata.
	var decryptedMeta []byte
	if t.recv == nil && t.isClient {
		t.recv = t.candidates[0].Clone()
	}
	if t.recv == nil {
		var peerBlock cipher.BlockCipher
		peerBlock, decryptedMeta, err = cipher.SelectDecrypt(encryptedMeta, cipher.CloneBlockCiphers(t.candidates))
		cipher.ServerIterateDecrypt.Add(1)
		if err != nil {
			cipher.ServerFailedIterateDecrypt.Add(1)
			return nil, fmt.Errorf("cipher.SelectDecrypt() failed: %w", err), stderror.CRYPTO_ERROR
		}
		t.recv = peerBlock.Clone()
	} else {
		decryptedMeta, err = t.recv.Decrypt(encryptedMeta)
		if t.isClient {
			cipher.ClientDirectDecrypt.Add(1)
		} else {
			cipher.ServerDirectDecrypt.Add(1)
		}
		if err != nil {
			if t.isClient {
				cipher.ClientFailedDirectDecrypt.Add(1)
			} else {
				cipher.ServerFailedDirectDecrypt.Add(1)
			}
			return nil, fmt.Errorf("Decrypt() failed: %w", err), stderror.CRYPTO_ERROR
		}
	}
	if len(decryptedMeta) != MetadataLength {
		return nil, fmt.Errorf("decrypted metadata size %d is unexpected", len(decryptedMeta)), stderror.PROTOCOL_ERROR
	}

	// Read payload and construct segment.
	p := decryptedMeta[0]
	if isSessionProtocol(protocolType(p)) {
		ss := &sessionStruct{}
		if err := ss.Unmarshal(decryptedMeta); err != nil {
			return nil, fmt.Errorf("Unmarshal() to sessionStruct failed: %w", err), stderror.PROTOCOL_ERROR
		}
		return t.readSessionSegment(ss)
	} else if isDataAckProtocol(protocolType(p)) {
		das := &dataAckStruct{}
		if err := das.Unmarshal(decryptedMeta); err != nil {
			return nil, fmt.Errorf("Unmarshal() to dataAckStruct failed: %w", err), stderror.PROTOCOL_ERROR
		}
		return t.readDataAckSegment(das)
	}
	return nil, fmt.Errorf("unable to handle protocol %d", p), stderror.PROTOCOL_ERROR
}

func (t *TCPUnderlay) readSessionSegment(ss *sessionStruct) (*segment, error, stderror.ErrorType) {
	var decryptedPayload []byte
	var err error

	if ss.payloadLen > 0 {
		encryptedPayload := make([]byte, ss.payloadLen+cipher.DefaultOverhead)
		if _, err := io.ReadFull(t.conn, encryptedPayload); err != nil {
			return nil, fmt.Errorf("payload: read %d bytes from TCPUnderlay failed: %w", ss.payloadLen+cipher.DefaultOverhead, err), stderror.NETWORK_ERROR
		}
		metrics.InBytes.Add(int64(len(encryptedPayload)))
		if tcpReplayCache.IsDuplicate(encryptedPayload[:cipher.DefaultOverhead], replay.EmptyTag) {
			replay.KnownSession.Add(1)
		}
		decryptedPayload, err = t.recv.Decrypt(encryptedPayload)
		if t.isClient {
			cipher.ClientDirectDecrypt.Add(1)
		} else {
			cipher.ServerDirectDecrypt.Add(1)
		}
		if err != nil {
			if t.isClient {
				cipher.ClientFailedDirectDecrypt.Add(1)
			} else {
				cipher.ServerFailedDirectDecrypt.Add(1)
			}
			return nil, fmt.Errorf("Decrypt() failed: %w", err), stderror.CRYPTO_ERROR
		}
	}
	if ss.suffixLen > 0 {
		padding := make([]byte, ss.suffixLen)
		if _, err := io.ReadFull(t.conn, padding); err != nil {
			return nil, fmt.Errorf("padding: read %d bytes from TCPUnderlay failed: %w", ss.suffixLen, err), stderror.NETWORK_ERROR
		}
		metrics.InBytes.Add(int64(len(padding)))
	}

	return &segment{
		metadata:  ss,
		payload:   decryptedPayload,
		transport: util.TCPTransport,
		block:     t.recv,
	}, nil, stderror.NO_ERROR
}

func (t *TCPUnderlay) readDataAckSegment(das *dataAckStruct) (*segment, error, stderror.ErrorType) {
	var decryptedPayload []byte
	var err error

	if das.prefixLen > 0 {
		padding1 := make([]byte, das.prefixLen)
		if _, err := io.ReadFull(t.conn, padding1); err != nil {
			return nil, fmt.Errorf("padding: read %d bytes from TCPUnderlay failed: %w", das.prefixLen, err), stderror.NETWORK_ERROR
		}
		metrics.InBytes.Add(int64(len(padding1)))
	}
	if das.payloadLen > 0 {
		encryptedPayload := make([]byte, das.payloadLen+cipher.DefaultOverhead)
		if _, err := io.ReadFull(t.conn, encryptedPayload); err != nil {
			return nil, fmt.Errorf("payload: read %d bytes from TCPUnderlay failed: %w", das.payloadLen+cipher.DefaultOverhead, err), stderror.NETWORK_ERROR
		}
		metrics.InBytes.Add(int64(len(encryptedPayload)))
		if tcpReplayCache.IsDuplicate(encryptedPayload[:cipher.DefaultOverhead], replay.EmptyTag) {
			replay.KnownSession.Add(1)
		}
		decryptedPayload, err = t.recv.Decrypt(encryptedPayload)
		if t.isClient {
			cipher.ClientDirectDecrypt.Add(1)
		} else {
			cipher.ServerDirectDecrypt.Add(1)
		}
		if err != nil {
			if t.isClient {
				cipher.ClientFailedDirectDecrypt.Add(1)
			} else {
				cipher.ServerFailedDirectDecrypt.Add(1)
			}
			return nil, fmt.Errorf("Decrypt() failed: %w", err), stderror.CRYPTO_ERROR
		}
	}
	if das.suffixLen > 0 {
		padding2 := make([]byte, das.suffixLen)
		if _, err := io.ReadFull(t.conn, padding2); err != nil {
			return nil, fmt.Errorf("padding: read %d bytes from TCPUnderlay failed: %w", das.suffixLen, err), stderror.NETWORK_ERROR
		}
		metrics.InBytes.Add(int64(len(padding2)))
	}

	return &segment{
		metadata:  das,
		payload:   decryptedPayload,
		transport: util.TCPTransport,
		block:     t.recv,
	}, nil, stderror.NO_ERROR
}

func (t *TCPUnderlay) writeOneSegment(seg *segment) error {
	if seg == nil {
		return stderror.ErrNullPointer
	}

	t.sendMutex.Lock()
	defer t.sendMutex.Unlock()

	if ss, ok := toSessionStruct(seg.metadata); ok {
		suffixLen := rng.Intn(MaxPaddingSize(t.mtu, t.IPVersion(), t.TransportProtocol(), int(ss.payloadLen), 0) + 1)
		ss.suffixLen = uint8(suffixLen)
		padding := newPadding(suffixLen)
		if log.IsLevelEnabled(log.TraceLevel) {
			log.Tracef("%v is sending %v", t, seg)
		}

		plaintextMetadata := seg.metadata.Marshal()
		if err := t.maybeInitSendBlockCipher(); err != nil {
			return fmt.Errorf("maybeInitSendBlockCipher() failed: %w", err)
		}
		encryptedMetadata, err := t.send.Encrypt(plaintextMetadata)
		if err != nil {
			return fmt.Errorf("Encrypt() failed: %w", err)
		}
		dataToSend := encryptedMetadata
		if len(seg.payload) > 0 {
			encryptedPayload, err := t.send.Encrypt(seg.payload)
			if err != nil {
				return fmt.Errorf("Encrypt() failed: %w", err)
			}
			dataToSend = append(dataToSend, encryptedPayload...)
		}
		dataToSend = append(dataToSend, padding...)
		if _, err := t.conn.Write(dataToSend); err != nil {
			return fmt.Errorf("Write() failed: %w", err)
		}
		metrics.OutBytes.Add(int64(len(dataToSend)))
		metrics.OutPaddingBytes.Add(int64(len(padding)))
	} else if das, ok := toDataAckStruct(seg.metadata); ok {
		paddingLen1 := rng.Intn(MaxPaddingSize(t.mtu, t.IPVersion(), t.TransportProtocol(), int(das.payloadLen), 0) + 1)
		paddingLen2 := rng.Intn(MaxPaddingSize(t.mtu, t.IPVersion(), t.TransportProtocol(), int(das.payloadLen), paddingLen1) + 1)
		das.prefixLen = uint8(paddingLen1)
		das.suffixLen = uint8(paddingLen2)
		padding1 := newPadding(paddingLen1)
		padding2 := newPadding(paddingLen2)
		if log.IsLevelEnabled(log.TraceLevel) {
			log.Tracef("%v is sending %v", t, seg)
		}

		plaintextMetadata := seg.metadata.Marshal()
		if err := t.maybeInitSendBlockCipher(); err != nil {
			return fmt.Errorf("maybeInitSendBlockCipher() failed: %w", err)
		}
		encryptedMetadata, err := t.send.Encrypt(plaintextMetadata)
		if err != nil {
			return fmt.Errorf("Encrypt() failed: %w", err)
		}
		dataToSend := append(encryptedMetadata, padding1...)
		if len(seg.payload) > 0 {
			encryptedPayload, err := t.send.Encrypt(seg.payload)
			if err != nil {
				return fmt.Errorf("Encrypt() failed: %w", err)
			}
			dataToSend = append(dataToSend, encryptedPayload...)
		}
		dataToSend = append(dataToSend, padding2...)
		if _, err := t.conn.Write(dataToSend); err != nil {
			return fmt.Errorf("Write() failed: %w", err)
		}
		metrics.OutBytes.Add(int64(len(dataToSend)))
		metrics.OutPaddingBytes.Add(int64(len(padding1)))
		metrics.OutPaddingBytes.Add(int64(len(padding2)))
	} else {
		return stderror.ErrInvalidArgument
	}
	return nil
}

func (t *TCPUnderlay) maybeInitSendBlockCipher() error {
	if t.send != nil {
		return nil
	}
	if t.isClient {
		t.send = t.candidates[0].Clone()
	} else {
		if t.recv != nil {
			t.send = t.recv.Clone()
			t.send.SetImplicitNonceMode(false) // clear implicit nonce
			t.send.SetImplicitNonceMode(true)
		} else {
			return fmt.Errorf("recv cipher is nil")
		}
	}
	return nil
}

// drainAfterError continues to read some data from the TCP connection after
// an error happened to confuse possible attacks.
func (t *TCPUnderlay) drainAfterError() {
	// Set TCP read deadline to avoid being blocked forever.
	timeoutMillis := rng.IntRange(1000, 10000)
	timeoutMillis += rng.FixedInt(50000) // Maximum 60 seconds.
	t.conn.SetReadDeadline(time.Now().Add(time.Duration(timeoutMillis) * time.Millisecond))

	// Determine the read buffer size.
	bufSizeType := rng.FixedInt(4)
	bufSize := 1 << (12 + bufSizeType) // 4, 8, 16, 32 KB
	buf := make([]byte, bufSize)

	// Determine the number of bytes to read.
	// Minimum 2 bytes, maximum bufSize bytes.
	minRead := rng.IntRange(2, bufSize-254)
	minRead += rng.FixedInt(256)

	n, err := io.ReadAtLeast(t.conn, buf, minRead)
	if err != nil {
		log.Debugf("%v read after TCP error failed to complete: %v", t, err)
	} else {
		log.Debugf("%v read at least %d bytes after TCP error", t, n)
	}
}
