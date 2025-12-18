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
	"net"
	"time"

	apicommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/metrics"
	"github.com/enfein/mieru/v3/pkg/replay"
	"github.com/enfein/mieru/v3/pkg/stderror"
)

const (
	packetOverhead          = cipher.DefaultNonceSize + MetadataLength + cipher.DefaultOverhead*2
	packetNonHeaderPosition = cipher.DefaultNonceSize + MetadataLength + cipher.DefaultOverhead

	idleSessionTimeout = time.Minute

	readOneSegmentTimeout = 5 * time.Second
)

var packetReplayCache = replay.NewCache(4*1024*1024, cipher.KeyRefreshInterval*3)

type PacketUnderlay struct {
	// ---- common fields ----
	baseUnderlay
	conn net.PacketConn

	sessionCleanTicker *time.Ticker

	// ---- client fields ----
	serverAddr net.Addr
	block      cipher.BlockCipher

	// ---- server fields ----
	users map[string]*appctlpb.User
}

var _ Underlay = &PacketUnderlay{}

// NewPacketUnderlay connects to the remote address "addr" on the network
// with packet encryption.
// "block" is the block encryption algorithm to encrypt packets.
//
// This function is only used by proxy client.
func NewPacketUnderlay(ctx context.Context, packetDialer apicommon.PacketDialer, resolver apicommon.DNSResolver, network, addr string, mtu int, block cipher.BlockCipher) (*PacketUnderlay, error) {
	switch network {
	case "udp", "udp4", "udp6":
	default:
		return nil, fmt.Errorf("network %s is not supported by packet underlay", network)
	}
	if !block.IsStateless() {
		return nil, fmt.Errorf("packet underlay block cipher must be stateless")
	}
	remoteUDPAddr, err := apicommon.ResolveUDPAddr(ctx, resolver, network, addr)
	if err != nil {
		return nil, fmt.Errorf("ResolveUDPAddr() failed: %w", err)
	}

	conn, err := packetDialer.ListenPacket(ctx, network, "", remoteUDPAddr.String())
	if err != nil {
		return nil, fmt.Errorf("ListenPacket() failed: %w", err)
	}
	u := &PacketUnderlay{
		baseUnderlay:       *newBaseUnderlay(true, mtu),
		conn:               conn,
		sessionCleanTicker: time.NewTicker(sessionCleanInterval),
		serverAddr:         remoteUDPAddr,
		block:              block,
	}

	// The block cipher expires after this time.
	u.scheduler.SetRemainingTime(cipher.KeyRefreshInterval / 2)
	return u, nil
}

func (u *PacketUnderlay) String() string {
	if u.conn == nil {
		return "PacketUnderlay{}"
	}
	if u.isClient {
		return fmt.Sprintf("PacketUnderlay{local=%v, remote=%v, mtu=%v}", u.LocalAddr(), u.RemoteAddr(), u.mtu)
	} else {
		return fmt.Sprintf("PacketUnderlay{local=%v, mtu=%v}", u.LocalAddr(), u.mtu)
	}
}

func (u *PacketUnderlay) Close() error {
	u.closeMutex.Lock()
	defer u.closeMutex.Unlock()
	select {
	case <-u.done:
		return nil
	default:
	}

	log.Debugf("Closing %v", u)
	u.sessionCleanTicker.Stop()
	u.baseUnderlay.Close()
	return u.conn.Close()
}

func (u *PacketUnderlay) TransportProtocol() common.TransportProtocol {
	return common.PacketTransport
}

func (u *PacketUnderlay) LocalAddr() net.Addr {
	return u.conn.LocalAddr()
}

func (u *PacketUnderlay) RemoteAddr() net.Addr {
	if u.isClient && u.serverAddr != nil {
		return u.serverAddr
	}
	return common.NilNetAddr()
}

func (u *PacketUnderlay) AddSession(s *Session, remoteAddr net.Addr) error {
	if err := u.baseUnderlay.AddSession(s, remoteAddr); err != nil {
		return err
	}
	s.conn = u // override base underlay
	s.transportProtocol = u.TransportProtocol()
	s.forwardStateTo(sessionAttached)
	close(s.ready)
	log.Debugf("Adding session %d to %v", s.id, u)

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

func (u *PacketUnderlay) RunEventLoop(ctx context.Context) error {
	if u.conn == nil {
		return stderror.ErrNullPointer
	}

	for {
		select {
		case <-ctx.Done():
			u.cleanSessions()
			return nil
		case <-u.done:
			u.cleanSessions()
			return nil
		case <-u.sessionCleanTicker.C:
			u.cleanSessions()
		default:
		}
		seg, addr, err := u.readOneSegment()
		if err != nil {
			return fmt.Errorf("readOneSegment() failed: %w", err)
		}
		if log.IsLevelEnabled(log.TraceLevel) {
			log.Tracef("%v received %v from peer %v", u, seg, addr)
		}
		if isSessionProtocol(seg.metadata.Protocol()) {
			switch seg.metadata.Protocol() {
			case openSessionRequest:
				if err := u.onOpenSessionRequest(seg, addr); err != nil {
					return fmt.Errorf("onOpenSessionRequest() failed: %w", err)
				}
			case openSessionResponse:
				if err := u.onOpenSessionResponse(seg); err != nil {
					return fmt.Errorf("onOpenSessionResponse() failed: %w", err)
				}
			case closeSessionRequest, closeSessionResponse:
				if err := u.onCloseSession(seg); err != nil {
					return fmt.Errorf("onCloseSession() failed: %w", err)
				}
			default:
				panic(fmt.Sprintf("Protocol %d is a session protocol but not recognized by packet underlay", seg.metadata.Protocol()))
			}
		} else if isDataAckProtocol(seg.metadata.Protocol()) {
			das, _ := toDataAckStruct(seg.metadata)
			session, ok := u.sessionMap.Load(das.sessionID)
			if !ok {
				log.Debugf("Session %d is not registered to %v", das.sessionID, u)
				if seg.block != nil {
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
						transport: u.TransportProtocol(),
						block:     seg.block,
					}
					if err := u.writeOneSegment(closeReq, addr); err != nil {
						return fmt.Errorf("writeOneSegment() failed: %w", err)
					}
				}
				continue
			}
			session.(*Session).recvChan <- seg
		} else {
			log.Debugf("Ignore unknown protocol %d", seg.metadata.Protocol())
		}
	}
}

func (u *PacketUnderlay) onOpenSessionRequest(seg *segment, remoteAddr net.Addr) error {
	if u.isClient {
		return stderror.ErrInvalidOperation
	}

	// Create a new session.
	sessionID := seg.metadata.(*sessionStruct).sessionID
	if sessionID == 0 {
		// 0 is reserved and can't be used.
		return fmt.Errorf("reserved session ID %d is used", sessionID)
	}
	_, found := u.sessionMap.Load(sessionID)
	if found {
		log.Debugf("%v received open session request, but session ID %d is already used", u, sessionID)
		return nil
	}
	session := NewSession(sessionID, false, u.MTU(), u.users)
	u.AddSession(session, remoteAddr)
	session.recvChan <- seg
	u.readySessions <- session
	return nil
}

func (u *PacketUnderlay) onOpenSessionResponse(seg *segment) error {
	if !u.isClient {
		return stderror.ErrInvalidOperation
	}

	sessionID := seg.metadata.(*sessionStruct).sessionID
	session, found := u.sessionMap.Load(sessionID)
	if !found {
		return fmt.Errorf("session ID %d is not found", sessionID)
	}
	session.(*Session).recvChan <- seg
	return nil
}

func (u *PacketUnderlay) onCloseSession(seg *segment) error {
	ss := seg.metadata.(*sessionStruct)
	sessionID := ss.sessionID
	session, found := u.sessionMap.Load(sessionID)
	if !found {
		if log.IsLevelEnabled(log.TraceLevel) {
			log.Tracef("%v received close session request or response, but session ID %d is not found", u, sessionID)
		}
		return nil
	}
	s := session.(*Session)
	s.recvChan <- seg
	s.wg.Wait()
	u.RemoveSession(s)
	return nil
}

func (u *PacketUnderlay) readOneSegment() (*segment, net.Addr, error) {
	for {
		select {
		case <-u.done:
			return nil, nil, io.ErrClosedPipe
		default:
		}

		// Peer may select a different MTU.
		// Use the largest possible value here to avoid error.
		b := make([]byte, 1500)
		common.SetReadTimeout(u.conn, readOneSegmentTimeout)
		n, addr, err := u.conn.ReadFrom(b)
		if err != nil {
			if stderror.IsTimeout(err) {
				continue
			}
			return nil, nil, fmt.Errorf("ReadFrom() failed: %w", err)
		}
		if u.isClient && addr.String() != u.serverAddr.String() {
			UnderlayUnsolicitedUDP.Add(1)
			if log.IsLevelEnabled(log.TraceLevel) {
				log.Tracef("%v received unsolicited packet from %v", u, addr)
			}
			continue
		}
		if n < packetNonHeaderPosition {
			UnderlayMalformedUDP.Add(1)
			if log.IsLevelEnabled(log.TraceLevel) {
				log.Tracef("%v received packet from %v with only %d bytes, which is too short", u, addr, n)
			}
			continue
		}
		b = b[:n]
		u.inBytes.Add(int64(n))
		if u.isClient {
			metrics.DownloadBytes.Add(int64(n))
		} else {
			metrics.UploadBytes.Add(int64(n))
		}

		// Read encrypted metadata.
		encryptedMeta := b[:packetNonHeaderPosition]
		isNewSessionReplay := false
		if packetReplayCache.IsDuplicate(encryptedMeta[:cipher.DefaultOverhead], addr.String()) {
			replay.NewSession.Add(1)
			isNewSessionReplay = true
		}
		nonce := encryptedMeta[:cipher.DefaultNonceSize]

		// Decrypt metadata.
		var decryptedMeta []byte
		var blockCipher cipher.BlockCipher
		if u.isClient {
			decryptedMeta, err = u.block.Decrypt(encryptedMeta)
			cipher.ClientDirectDecrypt.Add(1)
			if err != nil {
				cipher.ClientFailedDirectDecrypt.Add(1)
				if log.IsLevelEnabled(log.TraceLevel) {
					log.Tracef("%v Decrypt() failed with packet from %v", u, addr)
				}
				continue
			}
		} else {
			var decrypted bool
			var err error
			// Try existing sessions.
			cipher.ServerIterateDecrypt.Add(1)
			u.sessionMap.Range(func(k, v any) bool {
				session := v.(*Session)
				if session.block.Load() != nil && session.RemoteAddr().String() == addr.String() {
					decryptedMeta, err = (*session.block.Load()).Decrypt(encryptedMeta)
					if err == nil {
						decrypted = true
						blockCipher = *session.block.Load()
						return false
					}
				}
				return true
			})
			if !decrypted {
				// This is a new session. Try all registered users.
				for _, user := range u.users {
					var password []byte
					password, err = hex.DecodeString(user.GetHashedPassword())
					if err != nil {
						log.Debugf("Unable to decode hashed password %q from user %q", user.GetHashedPassword(), user.GetName())
						continue
					}
					if len(password) == 0 {
						password = cipher.HashPassword([]byte(user.GetPassword()), []byte(user.GetName()))
					}
					blockCipher, decryptedMeta, err = cipher.TryDecrypt(encryptedMeta, password, true)
					if err == nil {
						decrypted = true
						blockCipher.SetBlockContext(cipher.BlockContext{
							UserName: user.GetName(),
						})
						break
					}
				}
			}
			if !decrypted {
				cipher.ServerFailedIterateDecrypt.Add(1)
				if isNewSessionReplay {
					log.Debugf("found possible replay attack in %v from %v", u, addr)
				} else if log.IsLevelEnabled(log.TraceLevel) {
					log.Tracef("%v TryDecrypt() failed with packet from %v", u, addr)
				}
				continue
			} else {
				if blockCipher == nil {
					panic("PacketUnderlay readOneSegment(): block cipher is nil after decryption is successful")
				}
				if isNewSessionReplay {
					replay.NewSessionDecrypted.Add(1)
					log.Debugf("found possible replay attack with payload decrypted in %v from %v", u, addr)
					continue
				}
			}
		}
		if len(decryptedMeta) != MetadataLength {
			log.Debugf("decrypted metadata size %d is unexpected", len(decryptedMeta))
			continue
		}

		// Read payload and construct segment.
		var seg *segment
		p := decryptedMeta[0]
		if isSessionProtocol(protocolType(p)) {
			ss := &sessionStruct{}
			if err := ss.Unmarshal(decryptedMeta); err != nil {
				if u.isClient {
					return nil, nil, fmt.Errorf("Unmarshal() to sessionStruct failed: %w", err)
				} else {
					log.Debugf("%v Unmarshal() to sessionStruct failed: %v", u, err)
					continue
				}
			}
			seg, err = u.parseSessionSegment(ss, nonce, b[packetNonHeaderPosition:], blockCipher)
			if err != nil {
				if u.isClient {
					return nil, nil, err
				} else {
					log.Debugf("%v parseSessionSegment() failed: %v", u, err)
					continue
				}
			}
			if blockCipher != nil {
				seg.block = blockCipher
			}
			return seg, addr, nil
		} else if isDataAckProtocol(protocolType(p)) {
			das := &dataAckStruct{}
			if err := das.Unmarshal(decryptedMeta); err != nil {
				if u.isClient {
					return nil, nil, fmt.Errorf("Unmarshal() to dataAckStruct failed: %w", err)
				} else {
					log.Debugf("%v Unmarshal() to dataAckStruct failed: %v", u, err)
					continue
				}
			}
			seg, err = u.parseDataAckSegment(das, nonce, b[packetNonHeaderPosition:], blockCipher)
			if err != nil {
				if u.isClient {
					return nil, nil, err
				} else {
					log.Debugf("%v parseDataAckSegment() failed: %v", u, err)
					continue
				}
			}
			if blockCipher != nil {
				seg.block = blockCipher
			}
			return seg, addr, nil
		} else {
			if u.isClient {
				return nil, nil, fmt.Errorf("unable to handle protocol %d", p)
			} else {
				log.Debugf("%v unable to handle protocol %d", u, p)
				continue
			}
		}
	}
}

func (u *PacketUnderlay) parseSessionSegment(ss *sessionStruct, nonce, remaining []byte, blockCipher cipher.BlockCipher) (*segment, error) {
	var decryptedPayload []byte
	var err error

	if ss.payloadLen > 0 {
		if len(remaining) < int(ss.payloadLen)+cipher.DefaultOverhead {
			return nil, fmt.Errorf("payload: received incomplete packet")
		}
		if blockCipher == nil {
			if u.isClient {
				blockCipher = u.block
			} else {
				panic("PacketUnderlay readSessionSegment(): block is nil")
			}
		}
		encryptedPayload := remaining[:ss.payloadLen+cipher.DefaultOverhead]
		decryptedPayload, err = blockCipher.DecryptWithNonce(encryptedPayload, nonce)
		if u.isClient {
			cipher.ClientDirectDecrypt.Add(1)
		} else {
			cipher.ServerDirectDecrypt.Add(1)
		}
		if err != nil {
			if u.isClient {
				cipher.ClientFailedDirectDecrypt.Add(1)
			} else {
				cipher.ServerFailedDirectDecrypt.Add(1)
			}
			return nil, fmt.Errorf("DecryptWithNonce() failed: %w", err)
		}
		if int(ss.payloadLen)+cipher.DefaultOverhead+int(ss.suffixLen) != len(remaining) {
			return nil, fmt.Errorf("padding: size not match")
		}
	} else {
		if int(ss.suffixLen) != len(remaining) {
			return nil, fmt.Errorf("padding: size not match")
		}
	}

	return &segment{
		metadata:  ss,
		payload:   decryptedPayload,
		transport: common.PacketTransport,
	}, nil
}

func (u *PacketUnderlay) parseDataAckSegment(das *dataAckStruct, nonce, remaining []byte, blockCipher cipher.BlockCipher) (*segment, error) {
	var decryptedPayload []byte
	var err error

	if das.prefixLen > 0 {
		remaining = remaining[das.prefixLen:]
	}
	if das.payloadLen > 0 {
		if len(remaining) < int(das.payloadLen)+cipher.DefaultOverhead {
			return nil, fmt.Errorf("payload: received incomplete packet")
		}
		if blockCipher == nil {
			if u.isClient {
				blockCipher = u.block
			} else {
				panic("PacketUnderlay readDataAckSegment(): block is nil")
			}
		}
		encryptedPayload := remaining[:das.payloadLen+cipher.DefaultOverhead]
		decryptedPayload, err = blockCipher.DecryptWithNonce(encryptedPayload, nonce)
		if u.isClient {
			cipher.ClientDirectDecrypt.Add(1)
		} else {
			cipher.ServerDirectDecrypt.Add(1)
		}
		if err != nil {
			if u.isClient {
				cipher.ClientFailedDirectDecrypt.Add(1)
			} else {
				cipher.ServerFailedDirectDecrypt.Add(1)
			}
			return nil, fmt.Errorf("DecryptWithNonce() failed: %w", err)
		}
		if int(das.payloadLen)+cipher.DefaultOverhead+int(das.suffixLen) != len(remaining) {
			return nil, fmt.Errorf("padding: size not match")
		}
	} else {
		if int(das.suffixLen) != len(remaining) {
			return nil, fmt.Errorf("padding: size not match")
		}
	}

	return &segment{
		metadata:  das,
		payload:   decryptedPayload,
		transport: common.PacketTransport,
	}, nil
}

func (u *PacketUnderlay) writeOneSegment(seg *segment, addr net.Addr) error {
	if seg == nil {
		return stderror.ErrNullPointer
	}
	if u.isClient && addr.String() != u.serverAddr.String() {
		return fmt.Errorf("can't write to %v, server address is %v", addr, u.serverAddr)
	}

	u.sendMutex.Lock()
	defer u.sendMutex.Unlock()

	var blockCipher cipher.BlockCipher
	if u.isClient {
		if u.block == nil {
			panic(fmt.Sprintf("%v cipher block is not ready", u))
		} else {
			blockCipher = u.block
		}
	} else {
		if seg.block != nil {
			blockCipher = seg.block
		} else {
			sessionID, err := seg.SessionID()
			if err != nil {
				return fmt.Errorf("%v SessionID() failed: %v", seg, err)
			}
			session, ok := u.sessionMap.Load(sessionID)
			if !ok {
				return fmt.Errorf("session %d not found", sessionID)
			}
			s := session.(*Session)
			if s.block.Load() == nil {
				// stderror.ErrNotReady is needed to trigger stderror.ShouldRetry.
				return fmt.Errorf("%v cipher block is not ready, please try again later: %w", s, stderror.ErrNotReady)
			} else {
				blockCipher = *s.block.Load()
			}
		}
	}

	if blockCipher == nil {
		panic(fmt.Sprintf("%v cipher block is not ready", u))
	}

	if ss, ok := toSessionStruct(seg.metadata); ok {
		maxPaddingSize := MaxPaddingSize(u.mtu, u.TransportProtocol(), int(ss.payloadLen), 0)
		padding := newPadding(
			buildRecommendedPaddingOpts(maxPaddingSize, packetOverhead+int(ss.payloadLen), blockCipher.BlockContext().UserName),
		)
		ss.suffixLen = uint8(len(padding))
		if log.IsLevelEnabled(log.TraceLevel) {
			log.Tracef("%v is sending %v", u, seg)
		}

		plaintextMetadata := seg.metadata.Marshal()
		encryptedMetadata, err := blockCipher.Encrypt(plaintextMetadata)
		if err != nil {
			return fmt.Errorf("Encrypt() failed: %w", err)
		}
		nonce := encryptedMetadata[:cipher.DefaultNonceSize]
		dataToSend := encryptedMetadata
		if len(seg.payload) > 0 {
			encryptedPayload, err := blockCipher.EncryptWithNonce(seg.payload, nonce)
			if err != nil {
				return fmt.Errorf("EncryptWithNonce() failed: %w", err)
			}
			dataToSend = append(dataToSend, encryptedPayload...)
		}
		dataToSend = append(dataToSend, padding...)
		if _, err := u.conn.WriteTo(dataToSend, addr); err != nil {
			return fmt.Errorf("WriteTo() failed: %w", err)
		}
		u.outBytes.Add(int64(len(dataToSend)))
		if u.isClient {
			metrics.UploadBytes.Add(int64(len(dataToSend)))
		} else {
			metrics.DownloadBytes.Add(int64(len(dataToSend)))
		}
		metrics.OutputPaddingBytes.Add(int64(len(padding)))
	} else if das, ok := toDataAckStruct(seg.metadata); ok {
		padding1 := newPadding(paddingOpts{
			maxLen: MaxPaddingSize(u.mtu, u.TransportProtocol(), int(das.payloadLen), 0),
			ascii:  &asciiPaddingOpts{},
		})
		padding2 := newPadding(paddingOpts{
			maxLen: MaxPaddingSize(u.mtu, u.TransportProtocol(), int(das.payloadLen), len(padding1)),
			ascii:  &asciiPaddingOpts{},
		})
		das.prefixLen = uint8(len(padding1))
		das.suffixLen = uint8(len(padding2))
		if log.IsLevelEnabled(log.TraceLevel) {
			log.Tracef("%v is sending %v", u, seg)
		}

		plaintextMetadata := seg.metadata.Marshal()
		encryptedMetadata, err := blockCipher.Encrypt(plaintextMetadata)
		if err != nil {
			return fmt.Errorf("Encrypt() failed: %w", err)
		}
		nonce := encryptedMetadata[:cipher.DefaultNonceSize]
		dataToSend := append(encryptedMetadata, padding1...)
		if len(seg.payload) > 0 {
			encryptedPayload, err := blockCipher.EncryptWithNonce(seg.payload, nonce)
			if err != nil {
				return fmt.Errorf("EncryptWithNonce() failed: %w", err)
			}
			dataToSend = append(dataToSend, encryptedPayload...)
		}
		dataToSend = append(dataToSend, padding2...)
		if _, err := u.conn.WriteTo(dataToSend, addr); err != nil {
			return fmt.Errorf("WriteTo() failed: %w", err)
		}
		u.outBytes.Add(int64(len(dataToSend)))
		if u.isClient {
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

func (u *PacketUnderlay) cleanSessions() {
	u.sessionMap.Range(func(k, v any) bool {
		session := v.(*Session)
		select {
		case <-session.closedChan:
			log.Debugf("Found closed %v", session)
			if err := u.RemoveSession(session); err != nil {
				log.Debugf("%v RemoveSession() failed: %v", u, err)
			}
		default:
		}
		if time.Now().UnixMicro()-session.lastRXTime.Load() > idleSessionTimeout.Microseconds() {
			log.Debugf("Found idle %v", session)
			if err := u.RemoveSession(session); err != nil {
				log.Debugf("%v RemoveSession() failed: %v", u, err)
			}
		}
		return true
	})
}
