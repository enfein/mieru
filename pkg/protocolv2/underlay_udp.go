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
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/enfein/mieru/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/pkg/cipher"
	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/metrics"
	"github.com/enfein/mieru/pkg/replay"
	"github.com/enfein/mieru/pkg/rng"
	"github.com/enfein/mieru/pkg/stderror"
	"github.com/enfein/mieru/pkg/util"
)

const (
	udpOverhead          = cipher.DefaultNonceSize + MetadataLength + cipher.DefaultOverhead*2
	udpNonHeaderPosition = cipher.DefaultNonceSize + MetadataLength + cipher.DefaultOverhead

	idleSessionTickerInterval = 5 * time.Second
	idleSessionTimeout        = time.Minute
)

var udpReplayCache = replay.NewCache(4*1024*1024, 2*time.Minute)

type UDPUnderlay struct {
	// ---- common fields ----
	baseUnderlay
	conn *net.UDPConn

	idleSessionTicker *time.Ticker

	// ---- client fields ----
	serverAddr *net.UDPAddr
	block      cipher.BlockCipher

	// ---- server fields ----
	users map[string]*appctlpb.User // registered users
}

var _ Underlay = &UDPUnderlay{}

// NewUDPUnderlay connects to the remote address "raddr" on the network "udp"
// with packet encryption. If "laddr" is empty, an automatic address is used.
// "block" is the block encryption algorithm to encrypt packets.
func NewUDPUnderlay(ctx context.Context, network, laddr, raddr string, mtu int, block cipher.BlockCipher) (*UDPUnderlay, error) {
	switch network {
	case "udp", "udp4", "udp6":
	default:
		return nil, fmt.Errorf("network %s is not supported by UDP underlay", network)
	}
	if !block.IsStateless() {
		return nil, fmt.Errorf("UDP block cipher must be stateless")
	}
	var localAddr *net.UDPAddr
	var err error
	if laddr != "" {
		localAddr, err = net.ResolveUDPAddr("udp", laddr)
		if err != nil {
			return nil, fmt.Errorf("net.ResolveUDPAddr() failed: %w", err)
		}
	}
	remoteAddr, err := net.ResolveUDPAddr("udp", raddr)
	if err != nil {
		return nil, fmt.Errorf("net.ResolveUDPAddr() failed: %w", err)
	}

	conn, err := net.ListenUDP(network, localAddr)
	if err != nil {
		return nil, fmt.Errorf("net.ListenUDP() failed: %w", err)
	}
	u := &UDPUnderlay{
		baseUnderlay:      *newBaseUnderlay(true, mtu),
		conn:              conn,
		idleSessionTicker: time.NewTicker(idleSessionTickerInterval),
		serverAddr:        remoteAddr,
		block:             block,
	}
	log.Debugf("Created new client UDP underlay %v", u)
	return u, nil
}

func (u *UDPUnderlay) String() string {
	if u.conn == nil {
		return "UDPUnderlay{}"
	}
	if u.isClient {
		return fmt.Sprintf("UDPUnderlay{local=%v, remote=%v, mtu=%v, ipVersion=%v}", u.LocalAddr(), u.RemoteAddr(), u.mtu, u.IPVersion())
	} else {
		return fmt.Sprintf("UDPUnderlay{local=%v, mtu=%v, ipVersion=%v}", u.LocalAddr(), u.mtu, u.IPVersion())
	}
}

func (u *UDPUnderlay) Close() error {
	u.closeMutex.Lock()
	defer u.closeMutex.Unlock()
	select {
	case <-u.done:
		return nil
	default:
	}

	log.Debugf("Closing %v", u)
	u.idleSessionTicker.Stop()
	u.baseUnderlay.Close()
	return u.conn.Close()
}

func (u *UDPUnderlay) IPVersion() util.IPVersion {
	if u.conn == nil {
		return util.IPVersionUnknown
	}
	if u.ipVersion == util.IPVersionUnknown {
		u.ipVersion = util.GetIPVersion(u.conn.LocalAddr().String())
	}
	return u.ipVersion
}

func (u *UDPUnderlay) TransportProtocol() util.TransportProtocol {
	return util.UDPTransport
}

func (u *UDPUnderlay) LocalAddr() net.Addr {
	return u.conn.LocalAddr()
}

func (u *UDPUnderlay) RemoteAddr() net.Addr {
	if u.isClient && u.serverAddr != nil {
		return u.serverAddr
	}
	return util.NilNetAddr()
}

func (u *UDPUnderlay) AddSession(s *Session, remoteAddr net.Addr) error {
	if err := u.baseUnderlay.AddSession(s, remoteAddr); err != nil {
		return err
	}
	s.conn = u // override base underlay
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

func (u *UDPUnderlay) RunEventLoop(ctx context.Context) error {
	if u.conn == nil {
		return stderror.ErrNullPointer
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-u.done:
			return nil
		case <-u.idleSessionTicker.C:
			// Close idle sessions.
			u.sessionMap.Range(func(k, v any) bool {
				session := v.(*Session)
				if time.Since(session.lastRXTime) > idleSessionTimeout {
					log.Debugf("Found idle %v", session)
					if err := session.Close(); err != nil && !stderror.IsEOF(err) && !stderror.IsClosed(err) {
						log.Debugf("%v Close() failed: %v", session, err)
					}
					session.wg.Wait()
					if err := u.RemoveSession(session); err != nil {
						log.Debugf("%v RemoveSession() failed: %v", u, err)
					}
				}
				return true
			})
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
				panic(fmt.Sprintf("Protocol %d is a session protocol but not recognized by UDP underlay", seg.metadata.Protocol()))
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

func (u *UDPUnderlay) onOpenSessionRequest(seg *segment, remoteAddr net.Addr) error {
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
	session := NewSession(sessionID, u.isClient, u.MTU())
	u.AddSession(session, remoteAddr)
	session.recvChan <- seg
	u.readySessions <- session
	return nil
}

func (u *UDPUnderlay) onOpenSessionResponse(seg *segment) error {
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

func (u *UDPUnderlay) onCloseSession(seg *segment) error {
	ss := seg.metadata.(*sessionStruct)
	sessionID := ss.sessionID
	session, found := u.sessionMap.Load(sessionID)
	if !found {
		log.Debugf("%v received close session request or response, but session ID %d is not found", u, sessionID)
		return nil
	}
	s := session.(*Session)
	s.recvChan <- seg
	s.wg.Wait()
	u.RemoveSession(s)
	return nil
}

func (u *UDPUnderlay) readOneSegment() (*segment, *net.UDPAddr, error) {
	var n int
	var addr *net.UDPAddr
	var err error
	for {
		select {
		case <-u.done:
			return nil, nil, io.ErrClosedPipe
		default:
		}

		// Peer may select a different MTU.
		// Use the largest possible value here to avoid error.
		b := make([]byte, 1500)
		n, addr, err = u.conn.ReadFromUDP(b)
		if err != nil {
			return nil, nil, fmt.Errorf("ReadFromUDP() failed: %w", err)
		}
		if u.isClient && addr.String() != u.serverAddr.String() {
			UnderlayUnsolicitedUDP.Add(1)
			if log.IsLevelEnabled(log.TraceLevel) {
				log.Tracef("%v received unsolicited UDP packet from %v", u, addr)
			}
			continue
		}
		if n < udpNonHeaderPosition {
			UnderlayMalformedUDP.Add(1)
			if log.IsLevelEnabled(log.TraceLevel) {
				log.Tracef("%v received UDP packet from %v with only %d bytes, which is too short", u, addr, n)
			}
			continue
		}
		b = b[:n]
		metrics.InBytes.Add(int64(n))

		// Read encrypted metadata.
		encryptedMeta := b[:udpNonHeaderPosition]
		if udpReplayCache.IsDuplicate(encryptedMeta[:cipher.DefaultOverhead], addr.String()) {
			replay.NewSession.Add(1)
			return nil, nil, fmt.Errorf("found possible replay attack in %v from %v", u, addr)
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
					log.Tracef("%v Decrypt() failed with UDP packet from %v", u, addr)
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
				if session.block != nil && session.RemoteAddr().String() == addr.String() {
					decryptedMeta, err = session.block.Decrypt(encryptedMeta)
					if err == nil {
						decrypted = true
						blockCipher = session.block
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
				if log.IsLevelEnabled(log.TraceLevel) {
					log.Tracef("%v TryDecrypt() failed with UDP packet from %v", u, addr)
				}
				continue
			} else {
				if blockCipher == nil {
					panic("UDPUnderlay readOneSegment(): block cipher is nil after decryption is successful")
				}
			}
		}
		if len(decryptedMeta) != MetadataLength {
			return nil, nil, fmt.Errorf("decrypted metadata size %d is unexpected", len(decryptedMeta))
		}

		// Read payload and construct segment.
		var seg *segment
		p := decryptedMeta[0]
		if isSessionProtocol(protocolType(p)) {
			ss := &sessionStruct{}
			if err := ss.Unmarshal(decryptedMeta); err != nil {
				return nil, nil, fmt.Errorf("Unmarshal() to sessionStruct failed: %w", err)
			}
			seg, err = u.readSessionSegment(ss, nonce, b[udpNonHeaderPosition:], blockCipher)
			if err != nil {
				return nil, nil, err
			}
			if blockCipher != nil {
				seg.block = blockCipher
			}
			return seg, addr, nil
		} else if isDataAckProtocol(protocolType(p)) {
			das := &dataAckStruct{}
			if err := das.Unmarshal(decryptedMeta); err != nil {
				return nil, nil, fmt.Errorf("Unmarshal() to dataAckStruct failed: %w", err)
			}
			seg, err = u.readDataAckSegment(das, nonce, b[udpNonHeaderPosition:], blockCipher)
			if err != nil {
				return nil, nil, err
			}
			if blockCipher != nil {
				seg.block = blockCipher
			}
			return seg, addr, nil
		}
		return nil, nil, fmt.Errorf("unable to handle protocol %d", p)
	}
}

func (u *UDPUnderlay) readSessionSegment(ss *sessionStruct, nonce, remaining []byte, blockCipher cipher.BlockCipher) (*segment, error) {
	var decryptedPayload []byte
	var err error

	if ss.payloadLen > 0 {
		if len(remaining) < int(ss.payloadLen)+cipher.DefaultOverhead {
			return nil, fmt.Errorf("payload: received incomplete UDP packet")
		}
		if blockCipher == nil {
			if u.isClient {
				blockCipher = u.block
			} else {
				panic("UDPUnderlay readSessionSegment(): block is nil")
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
		transport: util.UDPTransport,
	}, nil
}

func (u *UDPUnderlay) readDataAckSegment(das *dataAckStruct, nonce, remaining []byte, blockCipher cipher.BlockCipher) (*segment, error) {
	var decryptedPayload []byte
	var err error

	if das.prefixLen > 0 {
		remaining = remaining[das.prefixLen:]
	}
	if das.payloadLen > 0 {
		if len(remaining) < int(das.payloadLen)+cipher.DefaultOverhead {
			return nil, fmt.Errorf("payload: received incomplete UDP packet")
		}
		if blockCipher == nil {
			if u.isClient {
				blockCipher = u.block
			} else {
				panic("UDPUnderlay readDataAckSegment(): block is nil")
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
		transport: util.UDPTransport,
	}, nil
}

func (u *UDPUnderlay) writeOneSegment(seg *segment, addr *net.UDPAddr) error {
	if seg == nil {
		return stderror.ErrNullPointer
	}
	if u.isClient && addr.String() != u.serverAddr.String() {
		return fmt.Errorf("can't write to %v, UDP server address is %v", addr, u.serverAddr)
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
			if s.block == nil {
				// stderror.ErrNotReady is needed to trigger stderror.ShouldRetry.
				return fmt.Errorf("%v cipher block is not ready, please try again later: %w", s, stderror.ErrNotReady)
			} else {
				blockCipher = s.block
			}
		}
	}

	if ss, ok := toSessionStruct(seg.metadata); ok {
		suffixLen := rng.Intn(MaxPaddingSize(u.mtu, u.IPVersion(), u.TransportProtocol(), int(ss.payloadLen), 0) + 1)
		ss.suffixLen = uint8(suffixLen)
		padding := newPadding(suffixLen)
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
		if _, err := u.conn.WriteToUDP(dataToSend, addr); err != nil {
			return fmt.Errorf("WriteToUDP() failed: %w", err)
		}
		metrics.OutBytes.Add(int64(len(dataToSend)))
		metrics.OutPaddingBytes.Add(int64(len(padding)))
	} else if das, ok := toDataAckStruct(seg.metadata); ok {
		paddingLen1 := rng.Intn(MaxPaddingSize(u.mtu, u.IPVersion(), u.TransportProtocol(), int(das.payloadLen), 0) + 1)
		paddingLen2 := rng.Intn(MaxPaddingSize(u.mtu, u.IPVersion(), u.TransportProtocol(), int(das.payloadLen), paddingLen1) + 1)
		das.prefixLen = uint8(paddingLen1)
		das.suffixLen = uint8(paddingLen2)
		padding1 := newPadding(paddingLen1)
		padding2 := newPadding(paddingLen2)
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
		if _, err := u.conn.WriteToUDP(dataToSend, addr); err != nil {
			return fmt.Errorf("WriteToUDP() failed: %w", err)
		}
		metrics.OutBytes.Add(int64(len(dataToSend)))
		metrics.OutPaddingBytes.Add(int64(len(padding1)))
		metrics.OutPaddingBytes.Add(int64(len(padding2)))
	} else {
		return stderror.ErrInvalidArgument
	}
	return nil
}
