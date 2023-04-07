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
	"fmt"
	"io"
	mrand "math/rand"
	"net"
	"sync"

	"github.com/enfein/mieru/pkg/cipher"
	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/netutil"
	"github.com/enfein/mieru/pkg/rng"
	"github.com/enfein/mieru/pkg/stderror"
)

const (
	tcpOverhead       = metadataLength + cipher.DefaultOverhead*2
	tcpOverhead1stPkt = cipher.DefaultNonceSize + tcpOverhead
)

type TCPUnderlay struct {
	baseUnderlay

	conn *net.TCPConn

	send cipher.BlockCipher
	recv cipher.BlockCipher

	// Candidates are block ciphers that can be used to encrypt or decrypt data.
	// When isClient is true, there must be exactly 1 element in the slice.
	candidates []cipher.BlockCipher

	// sendMutex is used when write data to the connection.
	sendMutex sync.Mutex
}

var _ Underlay = &TCPUnderlay{}

func (t *TCPUnderlay) String() string {
	if t.conn == nil {
		return "TCPUnderlay[]"
	}
	return fmt.Sprintf("TCPUnderlay[%v - %v]", t.conn.LocalAddr(), t.conn.RemoteAddr())
}

func (t *TCPUnderlay) MTU() int {
	return 1500
}

func (t *TCPUnderlay) IPVersion() netutil.IPVersion {
	if t.conn == nil {
		return netutil.IPVersionUnknown
	}
	return netutil.GetIPVersion(t.conn.LocalAddr().String())
}

func (t *TCPUnderlay) TransportProtocol() netutil.TransportProtocol {
	return netutil.TCPTransport
}

func (t *TCPUnderlay) RunEventLoop() (err error) {
	return nil
}

func (t *TCPUnderlay) Close() error {
	t.baseUnderlay.Close()
	return t.conn.Close()
}

func (t *TCPUnderlay) runInputLoop() (err error) {
	if t.conn == nil {
		return stderror.ErrNullPointer
	}

	for {
		select {
		case <-t.die:
			return nil
		default:
		}
		seg, err := t.readOneSegment()
		if err != nil {
			return fmt.Errorf("readOneSegment() failed: %w", err)
		}
		if isSessionProtocol(seg.metadata.Protocol()) {
			// TODO: handle session lifecycle.
		} else if isDataAckProtocol(seg.metadata.Protocol()) {
			das, _ := toDataAckStruct(seg.metadata)
			t.sessionLock.Lock()
			session, ok := t.sessionMap[das.sessionID]
			t.sessionLock.Unlock()
			if !ok {
				log.Debugf("session %d is not registered to this %s", das.sessionID, t.String())
				continue
			}
			if err := session.input(seg); err != nil {
				log.Debugf("input from %s to session %d failed: %v", t.String(), das.sessionID, err)
				continue
			}
		}
	}
}

func (t *TCPUnderlay) runOutputLoop(s *Session) (err error) {
	return nil
}

func (t *TCPUnderlay) onOpenSessionRequest(ss *sessionStruct) error {
	if t.isClient {
		return stderror.ErrInvalidOperation
	}
	// TODO: filter repeated requests.

	// Create a new session.
	t.sessionLock.Lock()
	var sessionID uint32
	for {
		sessionID = mrand.Uint32()
		if _, inEstablished := t.sessionMap[sessionID]; !inEstablished {
			break
		}
	}
	session := NewSession(sessionID, t.isClient, t.MTU())
	t.pendingSessionMap[sessionID] = session
	t.sessionLock.Unlock()

	// TODO: send open session response.
	return nil
}

func (t *TCPUnderlay) onOpenSessionResponse(ss *sessionStruct) error {
	if !t.isClient {
		return stderror.ErrInvalidOperation
	}

	t.sessionLock.Lock()
	defer t.sessionLock.Unlock()
	session, ok := t.pendingSessionMap[ss.requestID]
	if !ok {
		return stderror.ErrNotFound
	}
	delete(t.pendingSessionMap, ss.requestID)
	session.id = ss.sessionID
	t.sessionMap[ss.sessionID] = session
	return nil
}

func (t *TCPUnderlay) onCloseSessionRequest(ss *sessionStruct) error {
	return nil
}

func (t *TCPUnderlay) onCloseSessionResponse(ss *sessionStruct) error {
	return nil
}

func (t *TCPUnderlay) readOneSegment() (*segment, error) {
	var err error

	// Read encrypted metadata.
	readLen := metadataLength + cipher.DefaultOverhead
	if t.recv == nil {
		// In the first Read, also include nonce.
		readLen += cipher.DefaultNonceSize
	}
	encryptedMeta := make([]byte, readLen)
	if _, err := io.ReadFull(t.conn, encryptedMeta); err != nil {
		return nil, fmt.Errorf("metadata: read %d bytes from TCPUnderlay failed: %w", readLen, err)
	}

	// Decrypt metadata.
	var decryptedMeta []byte
	if t.recv == nil && t.isClient {
		t.recv = t.candidates[0].Clone()
	}
	if t.recv == nil {
		var peerBlock cipher.BlockCipher
		peerBlock, decryptedMeta, err = cipher.SelectDecrypt(encryptedMeta, cipher.CloneBlockCiphers(t.candidates))
		if err != nil {
			return nil, fmt.Errorf("cipher.SelectDecrypt() failed: %w", err)
		}
		t.recv = peerBlock.Clone()
	} else {
		decryptedMeta, err = t.recv.Decrypt(encryptedMeta)
		if err != nil {
			return nil, fmt.Errorf("Decrypt() failed: %w", err)
		}
	}
	if len(decryptedMeta) != metadataLength {
		return nil, fmt.Errorf("decrypted metadata size %d is unexpected", len(decryptedMeta))
	}

	// Read payload and construct segment.
	p := decryptedMeta[0]
	if isSessionProtocol(p) {
		ss := &sessionStruct{}
		if err := ss.Unmarshal(decryptedMeta); err != nil {
			return nil, fmt.Errorf("Unmarshal() failed: %w", err)
		}
		return t.readSessionSegment(ss)
	} else if isDataAckProtocol(p) {
		das := &dataAckStruct{}
		if err := das.Unmarshal(decryptedMeta); err != nil {
			return nil, fmt.Errorf("Unmarshal() failed: %w", err)
		}
		return t.readDataAckSegment(das)
	}

	return nil, fmt.Errorf("unable to handle protocol %d", p)
}

func (t *TCPUnderlay) readSessionSegment(ss *sessionStruct) (*segment, error) {
	padding := make([]byte, ss.paddingLen)
	if len(padding) > 0 {
		if _, err := io.ReadFull(t.conn, padding); err != nil {
			return nil, fmt.Errorf("padding: read %d bytes from TCPUnderlay failed: %w", ss.paddingLen, err)
		}
	}
	return &segment{metadata: ss}, nil
}

func (t *TCPUnderlay) readDataAckSegment(das *dataAckStruct) (*segment, error) {
	padding1 := make([]byte, das.prefixLen)
	if len(padding1) > 0 {
		if _, err := io.ReadFull(t.conn, padding1); err != nil {
			return nil, fmt.Errorf("padding: read %d bytes from TCPUnderlay failed: %w", das.prefixLen, err)
		}
	}

	encryptedPayload := make([]byte, das.payloadLen+cipher.DefaultOverhead)
	if _, err := io.ReadFull(t.conn, encryptedPayload); err != nil {
		return nil, fmt.Errorf("payload: read %d bytes from TCPUnderlay failed: %w", das.payloadLen+cipher.DefaultOverhead, err)
	}
	decryptedPayload, err := t.recv.Decrypt(encryptedPayload)
	if err != nil {
		return nil, fmt.Errorf("Decrypt() failed: %w", err)
	}

	padding2 := make([]byte, das.suffixLen)
	if len(padding2) > 0 {
		if _, err := io.ReadFull(t.conn, padding2); err != nil {
			return nil, fmt.Errorf("padding: read %d bytes from TCPUnderlay failed: %w", das.suffixLen, err)
		}
	}

	return &segment{metadata: das, payload: decryptedPayload}, nil
}

func (t *TCPUnderlay) writeOneSegment(seg *segment) error {
	if seg == nil {
		return stderror.ErrNullPointer
	}

	t.sendMutex.Lock()
	defer t.sendMutex.Unlock()

	if ss, ok := toSessionStruct(seg.metadata); ok {
		paddingLen := rng.Intn(255)
		ss.paddingLen = uint8(paddingLen)
		padding := newPadding(paddingLen)

		plaintextMetadata, err := ss.Marshal()
		if err != nil {
			return fmt.Errorf("Marshal() failed: %w", err)
		}
		if err := t.maybeInitSendBlockCipher(); err != nil {
			return fmt.Errorf("maybeInitSendBlockCipher() failed: %w", err)
		}
		encryptedMetadata, err := t.send.Encrypt(plaintextMetadata)
		if err != nil {
			return fmt.Errorf("Encrypt() failed: %w", err)
		}

		dataToSend := append(encryptedMetadata, padding...)
		if _, err := t.conn.Write(dataToSend); err != nil {
			return fmt.Errorf("Write() failed: %w", err)
		}
	} else if das, ok := toDataAckStruct(seg.metadata); ok {
		paddingLen1 := rng.Intn(255)
		paddingLen2 := rng.Intn(255)
		das.prefixLen = uint8(paddingLen1)
		das.suffixLen = uint8(paddingLen2)
		padding1 := newPadding(paddingLen1)
		padding2 := newPadding(paddingLen2)

		plaintextMetadata, err := ss.Marshal()
		if err != nil {
			return fmt.Errorf("Marshal() failed: %w", err)
		}
		if err := t.maybeInitSendBlockCipher(); err != nil {
			return fmt.Errorf("maybeInitSendBlockCipher() failed: %w", err)
		}
		encryptedMetadata, err := t.send.Encrypt(plaintextMetadata)
		if err != nil {
			return fmt.Errorf("Encrypt() failed: %w", err)
		}
		encryptedPayload, err := t.send.Encrypt(seg.payload)
		if err != nil {
			return fmt.Errorf("Encrypt() failed: %w", err)
		}

		dataToSend := append(encryptedMetadata, padding1...)
		dataToSend = append(dataToSend, encryptedPayload...)
		dataToSend = append(dataToSend, padding2...)
		if _, err := t.conn.Write(dataToSend); err != nil {
			return fmt.Errorf("Write() failed: %w", err)
		}
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
