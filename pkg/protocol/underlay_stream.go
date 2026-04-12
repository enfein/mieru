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

package protocol

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	mrand "math/rand"
	"net"
	"time"

	apicommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/mathext"
	"github.com/enfein/mieru/v3/pkg/metrics"
	"github.com/enfein/mieru/v3/pkg/replay"
	"github.com/enfein/mieru/v3/pkg/rng"
	"github.com/enfein/mieru/v3/pkg/stderror"
)

const (
	streamOverhead = MetadataLength + cipher.DefaultOverhead*2
)

var streamReplayCache = replay.NewCache(4*1024*1024, cipher.KeyRefreshInterval*3)

type StreamUnderlay struct {
	baseUnderlay
	conn net.Conn

	send cipher.BlockCipher
	recv cipher.BlockCipher

	sessionCleanTicker *time.Ticker

	// ---- client fields ----
	block cipher.BlockCipher

	// ---- server fields ----
	users map[string]*appctlpb.User
}

var _ Underlay = &StreamUnderlay{}

// NewStreamUnderlay connects to the remote address "addr" on the network
// with packet encryption.
// "block" is the block encryption algorithm to encrypt packets.
//
// This function is only used by proxy client.
func NewStreamUnderlay(
	ctx context.Context,
	dialer apicommon.Dialer,
	resolver apicommon.DNSResolver,
	dnsConfig *apicommon.ClientDNSConfig,
	network string,
	addr string,
	mtu int,
	block cipher.BlockCipher,
	trafficPattern *appctlpb.TrafficPattern,
) (*StreamUnderlay, error) {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return nil, fmt.Errorf("network %s is not supported by stream underlay", network)
	}
	if block.IsStateless() {
		return nil, fmt.Errorf("stream underlay block cipher must be stateful")
	}
	remote := addr
	if dnsConfig == nil || (!dnsConfig.BypassDialerDNS) {
		remoteTCPAddr, err := apicommon.ResolveTCPAddr(ctx, resolver, network, addr)
		if err != nil {
			return nil, fmt.Errorf("ResolveTCPAddr() failed: %w", err)
		}
		remote = remoteTCPAddr.String()
	}

	conn, err := dialer.DialContext(ctx, network, remote)
	if err != nil {
		return nil, fmt.Errorf("DialContext() failed: %w", err)
	}
	t := &StreamUnderlay{
		baseUnderlay:       *newBaseUnderlay(true, mtu, trafficPattern),
		conn:               conn,
		block:              block,
		sessionCleanTicker: time.NewTicker(sessionCleanInterval),
	}
	return t, nil
}

func (t *StreamUnderlay) String() string {
	if t.conn == nil {
		return "StreamUnderlay{}"
	}
	return fmt.Sprintf("StreamUnderlay{local=%v, remote=%v, mtu=%v}", t.conn.LocalAddr(), t.conn.RemoteAddr(), t.mtu)
}

func (t *StreamUnderlay) Close() error {
	t.closeMutex.Lock()
	defer t.closeMutex.Unlock()
	select {
	case <-t.done:
		return nil
	default:
	}

	log.Debugf("Closing %v", t)
	t.sessionCleanTicker.Stop()
	// Unblock any pending I/O before closing sessions.
	t.conn.SetDeadline(time.Now())
	t.baseUnderlay.Close()
	return nil
}

func (t *StreamUnderlay) Addr() net.Addr {
	return t.LocalAddr()
}

func (t *StreamUnderlay) TransportProtocol() common.TransportProtocol {
	return common.StreamTransport
}

func (t *StreamUnderlay) LocalAddr() net.Addr {
	if t.conn == nil {
		return common.NilNetAddr()
	}
	return t.conn.LocalAddr()
}

func (t *StreamUnderlay) RemoteAddr() net.Addr {
	if t.conn == nil {
		return common.NilNetAddr()
	}
	return t.conn.RemoteAddr()
}

func (t *StreamUnderlay) AddSession(s *Session, remoteAddr net.Addr) error {
	if err := t.baseUnderlay.AddSession(s, remoteAddr); err != nil {
		return err
	}
	s.conn = t // override base underlay
	s.transportProtocol = t.TransportProtocol()
	s.forwardStateTo(sessionAttached)
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

func (t *StreamUnderlay) RunEventLoop(ctx context.Context) error {
	if t.conn == nil {
		return stderror.ErrNullPointer
	}
	defer t.conn.Close()

	for {
		select {
		case <-ctx.Done():
			t.cleanSessions()
			return nil
		case <-t.done:
			t.cleanSessions()
			return nil
		case <-t.sessionCleanTicker.C:
			t.cleanSessions()
		default:
		}
		seg, err := t.readOneSegment()
		if err != nil {
			// If the underlay is closing, return gracefully.
			select {
			case <-t.done:
				t.cleanSessions()
				return nil
			default:
			}
			errType := stderror.GetErrorType(err)
			if errType == stderror.NO_ERROR {
				panic(fmt.Sprintf("%v error type is NO_ERROR while error is not nil", t))
			}
			if errType == stderror.UNKNOWN_ERROR {
				panic(fmt.Sprintf("%v got unexpected error type UNKNOWN_ERROR", t))
			}
			if errType == stderror.CRYPTO_ERROR || errType == stderror.REPLAY_ERROR {
				t.drainAfterError()
			}
			return fmt.Errorf("readOneSegment() failed: %w", err)
		} else if seg == nil {
			// Timeout reading the segment, will try again later.
			continue
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
				panic(fmt.Sprintf("Protocol %d is a session protocol but not recognized by stream underlay", seg.metadata.Protocol()))
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

func (t *StreamUnderlay) onOpenSessionRequest(seg *segment) error {
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
	session := NewSession(sessionID, false, t.MTU(), t.users, t.trafficPattern)
	t.AddSession(session, nil)
	session.recvChan <- seg
	t.readySessions <- session
	return nil
}

func (t *StreamUnderlay) onOpenSessionResponse(seg *segment) error {
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

func (t *StreamUnderlay) onCloseSession(seg *segment) error {
	ss := seg.metadata.(*sessionStruct)
	sessionID := ss.sessionID
	session, found := t.sessionMap.Load(sessionID)
	if !found {
		if log.IsLevelEnabled(log.TraceLevel) {
			log.Tracef("%v received close session request or response, but session ID %d is not found", t, sessionID)
		}
		return nil
	}
	s := session.(*Session)
	s.recvChan <- seg
	return nil
}

func (t *StreamUnderlay) readOneSegment() (*segment, error) {
	var firstRead bool
	var err error

	common.SetReadTimeout(t.conn, readOneSegmentTimeout)
	defer common.SetReadTimeout(t.conn, 0)

	// Read encrypted metadata.
	readLen := MetadataLength + cipher.DefaultOverhead
	if t.recv == nil {
		// In the first Read, also include nonce.
		firstRead = true
		readLen += cipher.DefaultNonceSize
	}
	encryptedMeta := make([]byte, readLen)
	if n, err := io.ReadFull(t.conn, encryptedMeta); err != nil {
		if stderror.IsTimeout(err) && n == 0 {
			// No TCP data received. Caller will retry.
			return nil, nil
		}
		err = fmt.Errorf("metadata: read %d bytes from StreamUnderlay failed: %w", readLen, err)
		return nil, stderror.WrapErrorWithType(err, stderror.NETWORK_ERROR)
	}
	t.inBytes.Add(int64(len(encryptedMeta)))
	if t.isClient {
		metrics.DownloadBytes.Add(int64(len(encryptedMeta)))
	} else {
		metrics.UploadBytes.Add(int64(len(encryptedMeta)))
	}
	isNewSessionReplay := false
	if streamReplayCache.IsDuplicate(encryptedMeta[:cipher.DefaultOverhead], replay.EmptyTag) {
		if firstRead {
			replay.NewSession.Add(1)
			isNewSessionReplay = true
		} else {
			replay.KnownSession.Add(1)
		}
	}

	// Decrypt metadata.
	var decryptedMeta []byte
	if t.recv == nil && t.isClient {
		t.recv = t.block.Clone()
	}
	if t.recv == nil {
		decryptedMeta, err = t.serverInitRecvBlockCipherAndDecryptMetadata(encryptedMeta)
		cipher.ServerIterateDecrypt.Add(1)
		if err != nil {
			cipher.ServerFailedIterateDecrypt.Add(1)
			if isNewSessionReplay {
				err = fmt.Errorf("found possible replay attack in %v", t)
				return nil, stderror.WrapErrorWithType(err, stderror.REPLAY_ERROR)
			} else {
				err = fmt.Errorf("%w", err)
				return nil, stderror.WrapErrorWithType(err, stderror.CRYPTO_ERROR)
			}
		} else if isNewSessionReplay {
			replay.NewSessionDecrypted.Add(1)
			err = fmt.Errorf("found possible replay attack with payload decrypted in %v", t)
			return nil, stderror.WrapErrorWithType(err, stderror.REPLAY_ERROR)
		}
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
			err = fmt.Errorf("Decrypt() failed: %w", err)
			return nil, stderror.WrapErrorWithType(err, stderror.CRYPTO_ERROR)
		}
	}
	if len(decryptedMeta) != MetadataLength {
		err = fmt.Errorf("decrypted metadata size %d is unexpected", len(decryptedMeta))
		return nil, stderror.WrapErrorWithType(err, stderror.PROTOCOL_ERROR)
	}

	// Read payload and construct segment.
	p := decryptedMeta[0]
	if isSessionProtocol(protocolType(p)) {
		ss := &sessionStruct{}
		if err := ss.Unmarshal(decryptedMeta); err != nil {
			err = fmt.Errorf("Unmarshal() to sessionStruct failed: %w", err)
			return nil, stderror.WrapErrorWithType(err, stderror.PROTOCOL_ERROR)
		}
		return t.readSessionSegment(ss)
	} else if isDataAckProtocol(protocolType(p)) {
		das := &dataAckStruct{}
		if err := das.Unmarshal(decryptedMeta); err != nil {
			err = fmt.Errorf("Unmarshal() to dataAckStruct failed: %w", err)
			return nil, stderror.WrapErrorWithType(err, stderror.PROTOCOL_ERROR)
		}
		return t.readDataAckSegment(das)
	} else {
		err = fmt.Errorf("unable to handle protocol %d", p)
		return nil, stderror.WrapErrorWithType(err, stderror.PROTOCOL_ERROR)
	}
}

func (t *StreamUnderlay) readSessionSegment(ss *sessionStruct) (*segment, error) {
	var decryptedPayload []byte
	var err error

	if ss.payloadLen > 0 {
		encryptedPayload := make([]byte, ss.payloadLen+cipher.DefaultOverhead)
		if _, err := io.ReadFull(t.conn, encryptedPayload); err != nil {
			err = fmt.Errorf("payload: read %d bytes from StreamUnderlay failed: %w", ss.payloadLen+cipher.DefaultOverhead, err)
			return nil, stderror.WrapErrorWithType(err, stderror.NETWORK_ERROR)
		}
		t.inBytes.Add(int64(len(encryptedPayload)))
		if t.isClient {
			metrics.DownloadBytes.Add(int64(len(encryptedPayload)))
		} else {
			metrics.UploadBytes.Add(int64(len(encryptedPayload)))
		}
		if streamReplayCache.IsDuplicate(encryptedPayload[:cipher.DefaultOverhead], replay.EmptyTag) {
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
			err = fmt.Errorf("Decrypt() failed: %w", err)
			return nil, stderror.WrapErrorWithType(err, stderror.CRYPTO_ERROR)
		}
	}
	if ss.suffixLen > 0 {
		padding := make([]byte, ss.suffixLen)
		if _, err := io.ReadFull(t.conn, padding); err != nil {
			err = fmt.Errorf("padding: read %d bytes from StreamUnderlay failed: %w", ss.suffixLen, err)
			return nil, stderror.WrapErrorWithType(err, stderror.NETWORK_ERROR)
		}
		t.inBytes.Add(int64(len(padding)))
		if t.isClient {
			metrics.DownloadBytes.Add(int64(len(padding)))
		} else {
			metrics.UploadBytes.Add(int64(len(padding)))
		}
	}

	return &segment{
		metadata:  ss,
		payload:   decryptedPayload,
		transport: common.StreamTransport,
		block:     t.recv,
	}, nil
}

func (t *StreamUnderlay) readDataAckSegment(das *dataAckStruct) (*segment, error) {
	var decryptedPayload []byte
	var err error

	if das.prefixLen > 0 {
		padding1 := make([]byte, das.prefixLen)
		if _, err := io.ReadFull(t.conn, padding1); err != nil {
			err = fmt.Errorf("padding: read %d bytes from StreamUnderlay failed: %w", das.prefixLen, err)
			return nil, stderror.WrapErrorWithType(err, stderror.NETWORK_ERROR)
		}
		t.inBytes.Add(int64(len(padding1)))
		if t.isClient {
			metrics.DownloadBytes.Add(int64(len(padding1)))
		} else {
			metrics.UploadBytes.Add(int64(len(padding1)))
		}
	}
	if das.payloadLen > 0 {
		encryptedPayload := make([]byte, das.payloadLen+cipher.DefaultOverhead)
		if _, err := io.ReadFull(t.conn, encryptedPayload); err != nil {
			err = fmt.Errorf("payload: read %d bytes from StreamUnderlay failed: %w", das.payloadLen+cipher.DefaultOverhead, err)
			return nil, stderror.WrapErrorWithType(err, stderror.NETWORK_ERROR)
		}
		t.inBytes.Add(int64(len(encryptedPayload)))
		if t.isClient {
			metrics.DownloadBytes.Add(int64(len(encryptedPayload)))
		} else {
			metrics.UploadBytes.Add(int64(len(encryptedPayload)))
		}
		if streamReplayCache.IsDuplicate(encryptedPayload[:cipher.DefaultOverhead], replay.EmptyTag) {
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
			err = fmt.Errorf("Decrypt() failed: %w", err)
			return nil, stderror.WrapErrorWithType(err, stderror.CRYPTO_ERROR)
		}
	}
	if das.suffixLen > 0 {
		padding2 := make([]byte, das.suffixLen)
		if _, err := io.ReadFull(t.conn, padding2); err != nil {
			err = fmt.Errorf("padding: read %d bytes from StreamUnderlay failed: %w", das.suffixLen, err)
			return nil, stderror.WrapErrorWithType(err, stderror.NETWORK_ERROR)
		}
		t.inBytes.Add(int64(len(padding2)))
		if t.isClient {
			metrics.DownloadBytes.Add(int64(len(padding2)))
		} else {
			metrics.UploadBytes.Add(int64(len(padding2)))
		}
	}

	return &segment{
		metadata:  das,
		payload:   decryptedPayload,
		transport: common.StreamTransport,
		block:     t.recv,
	}, nil
}

func (t *StreamUnderlay) writeOneSegment(seg *segment) error {
	if seg == nil {
		return stderror.ErrNullPointer
	}

	t.sendMutex.Lock()
	defer t.sendMutex.Unlock()

	if err := t.maybeInitSendBlockCipher(); err != nil {
		return fmt.Errorf("maybeInitSendBlockCipher() failed: %w", err)
	}

	if ss, ok := toSessionStruct(seg.metadata); ok {
		maxPaddingSize := MaxPaddingSize(t.mtu, t.TransportProtocol(), int(ss.payloadLen), 0)
		padding := newPadding(
			buildRecommendedPaddingOpts(maxPaddingSize, streamOverhead+int(ss.payloadLen), t.send.BlockContext().UserName),
		)
		ss.suffixLen = uint8(len(padding))
		if log.IsLevelEnabled(log.TraceLevel) {
			log.Tracef("%v is sending %v", t, seg)
		}

		plaintextMetadata := seg.metadata.Marshal()
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
		if err := t.writeWithPossibleFragment(dataToSend); err != nil {
			return err
		}
		t.outBytes.Add(int64(len(dataToSend)))
		if t.isClient {
			metrics.UploadBytes.Add(int64(len(dataToSend)))
		} else {
			metrics.DownloadBytes.Add(int64(len(dataToSend)))
		}
		metrics.OutputPaddingBytes.Add(int64(len(padding)))
	} else if das, ok := toDataAckStruct(seg.metadata); ok {
		padding1 := newPadding(paddingOpts{
			maxLen: MaxPaddingSize(t.mtu, t.TransportProtocol(), int(das.payloadLen), 0),
			ascii:  &asciiPaddingOpts{},
		})
		padding2 := newPadding(paddingOpts{
			maxLen: MaxPaddingSize(t.mtu, t.TransportProtocol(), int(das.payloadLen), len(padding1)),
			ascii:  &asciiPaddingOpts{},
		})
		das.prefixLen = uint8(len(padding1))
		das.suffixLen = uint8(len(padding2))
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
		t.outBytes.Add(int64(len(dataToSend)))
		if t.isClient {
			metrics.UploadBytes.Add(int64(len(dataToSend)))
		} else {
			metrics.DownloadBytes.Add(int64(len(dataToSend)))
		}
		metrics.OutputPaddingBytes.Add(int64(len(padding1)))
		metrics.OutputPaddingBytes.Add(int64(len(padding2)))
	} else {
		return stderror.ErrInvalidArgument
	}
	return nil
}

// serverInitRecvBlockCipherAndDecryptMetadata performs decryption against all
// registered users and, on success, initializes t.recv with a stateful clone
// of the single matched cipher. It returns the decrypted metadata.
func (t *StreamUnderlay) serverInitRecvBlockCipherAndDecryptMetadata(encryptedMeta []byte) ([]byte, error) {
	if t.recv != nil {
		return nil, fmt.Errorf("recv cipher is already set")
	}
	nonce := encryptedMeta[:cipher.DefaultNonceSize]

	var matchedBlock cipher.BlockCipher
	var matchedUserName string

	// First, try to narrow down the user using the nonce hint.
	var hintUser *appctlpb.User
	for _, user := range t.users {
		if cipher.CheckUserFromHint([]byte(user.GetName()), nonce) {
			hintUser = user
			break
		}
	}
	if hintUser != nil {
		cipher.ServerHintMatchDecrypt.Add(1)
		password, err := hex.DecodeString(hintUser.GetHashedPassword())
		if err == nil {
			if len(password) == 0 {
				password = cipher.HashPassword([]byte(hintUser.GetPassword()), []byte(hintUser.GetName()))
			}
			matchedBlock, _, err = cipher.TryDecrypt(encryptedMeta, password, true)
			if err == nil {
				matchedUserName = hintUser.GetName()
			} else {
				cipher.ServerFailedHintMatchDecrypt.Add(1)
			}
		}
	}

	// Fallback: try all registered users.
	// This handles old clients without hints and hint hash collisions.
	if matchedBlock == nil {
		for _, user := range t.users {
			password, err := hex.DecodeString(user.GetHashedPassword())
			if err != nil {
				continue
			}
			if len(password) == 0 {
				password = cipher.HashPassword([]byte(user.GetPassword()), []byte(user.GetName()))
			}
			matchedBlock, _, err = cipher.TryDecrypt(encryptedMeta, password, true)
			if err == nil {
				matchedUserName = user.GetName()
				break
			}
		}
	}
	if matchedBlock == nil {
		return nil, fmt.Errorf("cipher.TryDecrypt() failed for all users")
	}

	// Clone only the matched cipher as stateful, and re-decrypt to capture nonce state.
	t.recv = matchedBlock.Clone()
	t.recv.SetBlockContext(cipher.BlockContext{
		UserName: matchedUserName,
	})
	if t.trafficPattern != nil {
		t.recv.SetNoncePattern(t.trafficPattern.GetNonce())
	}
	t.recv.SetImplicitNonceMode(true)
	decryptedMeta, err := t.recv.Decrypt(encryptedMeta)
	if err != nil {
		t.recv = nil
		return nil, fmt.Errorf("stateful Decrypt() failed: %w", err)
	}
	return decryptedMeta, nil
}

func (t *StreamUnderlay) maybeInitSendBlockCipher() error {
	if t.send != nil {
		return nil
	}
	if t.isClient {
		t.send = t.block.Clone()
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

// writeWithPossibleFragment writes data to the stream network connection
// with or without fragmentation depending on the traffic pattern.
func (t *StreamUnderlay) writeWithPossibleFragment(dataToSend []byte) error {
	if t.trafficPattern == nil || t.trafficPattern.GetTcpFragment() == nil || !t.trafficPattern.GetTcpFragment().GetEnable() {
		if _, err := t.conn.Write(dataToSend); err != nil {
			return fmt.Errorf("Write() failed: %w", err)
		}
		return nil
	}

	remaining := dataToSend
	for len(remaining) > 0 {
		minLenToSend := int(math.Sqrt(float64(len(dataToSend)))) + 1
		maxLenToSend := mathext.Max(minLenToSend, len(dataToSend)/2)
		lenToSend := mrand.Intn(maxLenToSend-minLenToSend+1) + minLenToSend
		if lenToSend > len(remaining) {
			lenToSend = len(remaining)
		}
		if _, err := t.conn.Write(remaining[:lenToSend]); err != nil {
			return fmt.Errorf("Write() failed: %w", err)
		}
		if t.trafficPattern.GetTcpFragment().GetMaxSleepMs() > 0 {
			sleepMs := mrand.Intn(int(t.trafficPattern.GetTcpFragment().GetMaxSleepMs()) + 1)
			time.Sleep(time.Duration(sleepMs) * time.Millisecond)
		}
		remaining = remaining[lenToSend:]
	}
	return nil
}

// drainAfterError continues to read some data from the stream network connection
// after an error happened to confuse possible attacks.
func (t *StreamUnderlay) drainAfterError() {
	// Set read deadline to avoid being blocked forever.
	timeoutMillis := rng.IntRange(1000, 10000)
	timeoutMillis += rng.FixedIntVH(50000) // Maximum 60 seconds.
	t.conn.SetReadDeadline(time.Now().Add(time.Duration(timeoutMillis) * time.Millisecond))

	// Determine the read buffer size.
	bufSizeType := rng.FixedIntVH(4)
	bufSize := 1 << (12 + bufSizeType) // 4, 8, 16, 32 KB
	buf := make([]byte, bufSize)

	// Determine the number of bytes to read.
	// Minimum 0 bytes, maximum bufSize bytes.
	minRead := rng.IntRange(0, bufSize-254)
	minRead += rng.FixedIntVH(256)

	if minRead > 0 {
		n, err := io.ReadAtLeast(t.conn, buf, minRead)
		if err != nil {
			log.Debugf("%v read after stream error failed to complete: %v", t, err)
		} else {
			log.Debugf("%v read at least %d bytes after stream error", t, n)
		}
	}
}

func (t *StreamUnderlay) cleanSessions() {
	t.sessionMap.Range(func(k, v any) bool {
		session := v.(*Session)
		select {
		case <-session.closedChan:
			log.Debugf("Found closed %v", session)
			if err := t.RemoveSession(session); err != nil {
				log.Debugf("%v RemoveSession() failed: %v", t, err)
			}
		default:
		}
		return true
	})
}
