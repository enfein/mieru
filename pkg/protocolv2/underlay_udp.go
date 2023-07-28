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
	"net"
	"sync"
	"time"

	"github.com/enfein/mieru/pkg/cipher"
	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/netutil"
	"github.com/enfein/mieru/pkg/replay"
	"github.com/enfein/mieru/pkg/rng"
	"github.com/enfein/mieru/pkg/stderror"
)

const (
	udpOverhead          = cipher.DefaultNonceSize + metadataLength + cipher.DefaultOverhead*2
	udpNonHeaderPosition = cipher.DefaultNonceSize + metadataLength + cipher.DefaultOverhead
)

var udpReplayCache = replay.NewCache(16*1024*1024, 2*time.Minute)

type UDPUnderlay struct {
	// ---- common fields ----
	baseUnderlay
	conn  *net.UDPConn
	block cipher.BlockCipher

	// Candidates are block ciphers that can be used to encrypt or decrypt data.
	// When isClient is true, there must be exactly 1 element in the slice.
	candidates []cipher.BlockCipher

	// sendMutex is used when write data to the connection.
	sendMutex sync.Mutex

	// ---- client fields ----
	serverAddr *net.UDPAddr
}

var _ Underlay = &UDPUnderlay{}

// NewUDPUnderlay connects to the remote address "raddr" on the network "udp"
// with packet encryption. If "laddr" is empty, an automatic address is used.
// "block" is the block encryption algorithm to encrypt packets.
func NewUDPUnderlay(ctx context.Context, network, laddr, raddr string, mtu int, block cipher.BlockCipher) (*UDPUnderlay, error) {
	switch network {
	case "udp", "udp4", "udp6":
	default:
		return nil, fmt.Errorf("network %s is not supported for UDP underlay", network)
	}
	if !block.IsStateless() {
		return nil, fmt.Errorf("UDP block cipher must be stateless")
	}
	remoteAddr, err := net.ResolveUDPAddr("udp", raddr)
	if err != nil {
		return nil, fmt.Errorf("net.ResolveUDPAddr() failed: %w", err)
	}
	var localAddr *net.UDPAddr
	if laddr != "" {
		localAddr, err = net.ResolveUDPAddr("udp", laddr)
		if err != nil {
			return nil, fmt.Errorf("net.ResolveUDPAddr() failed: %w", err)
		}
	}

	conn, err := net.ListenUDP(network, localAddr)
	if err != nil {
		return nil, fmt.Errorf("net.ListenUDP() failed: %w", err)
	}
	log.Debugf("Created new client UDP underlay [%v - %v]", conn.LocalAddr(), remoteAddr)
	return &UDPUnderlay{
		baseUnderlay: *newBaseUnderlay(true, mtu),
		conn:         conn,
		serverAddr:   remoteAddr,
		block:        block,
		candidates:   []cipher.BlockCipher{block},
	}, nil
}

func (u *UDPUnderlay) String() string {
	if u.conn == nil {
		return "UDPUnderlay{}"
	}
	return fmt.Sprintf("UDPUnderlay{%v - %v}", u.conn.LocalAddr(), u.conn.RemoteAddr())
}

func (u *UDPUnderlay) Close() error {
	select {
	case <-u.done:
		return nil
	default:
	}

	log.Debugf("Closing %v", u)
	u.baseUnderlay.Close()
	return u.conn.Close()
}

func (u *UDPUnderlay) IPVersion() netutil.IPVersion {
	if u.conn == nil {
		return netutil.IPVersionUnknown
	}
	return netutil.GetIPVersion(u.conn.LocalAddr().String())
}

func (u *UDPUnderlay) TransportProtocol() netutil.TransportProtocol {
	return netutil.UDPTransport
}

func (u *UDPUnderlay) LocalAddr() net.Addr {
	return u.conn.LocalAddr()
}

func (u *UDPUnderlay) RemoteAddr() net.Addr {
	return u.conn.RemoteAddr()
}

func (u *UDPUnderlay) AddSession(s *Session) error {
	if err := u.baseUnderlay.AddSession(s); err != nil {
		return err
	}
	s.conn = u // override base underlay
	close(s.ready)
	log.Debugf("Adding session %d to %v", s.id, u)

	s.wg.Add(2)
	go func() {
		if err := s.runInputLoop(context.Background()); err != nil {
			log.Debugf("%v runInputLoop(): %v", s, err)
		}
		s.wg.Done()
	}()
	go func() {
		if err := s.runOutputLoop(context.Background()); err != nil {
			log.Debugf("%v runOutputLoop(): %v", s, err)
		}
		s.wg.Done()
	}()
	return nil
}

func (u *UDPUnderlay) RemoveSession(s *Session) error {
	err := u.baseUnderlay.RemoveSession(s)
	if len(u.baseUnderlay.sessionMap) == 0 {
		u.Close()
	}
	return err
}

func (u *UDPUnderlay) RunEventLoop(ctx context.Context) error {
	if u.conn == nil {
		return stderror.ErrNullPointer
	}
	return nil
}

func (u *UDPUnderlay) readOneSegment() (*segment, error) {
	b := make([]byte, u.mtu)
	for {
		n, addr, err := u.conn.ReadFromUDP(b)
		if err != nil {
			return nil, fmt.Errorf("ReadFromUDP() failed: %w", err)
		}
		if u.isClient && addr.String() != u.serverAddr.String() {
			UnderlayUnsolicitedUDP.Add(1)
			continue
		}
		if n < udpOverhead {
			return nil, fmt.Errorf("metadata: received incomplete UDP packet")
		}
		b = b[:n]

		// Read encrypted metadata.
		readLen := metadataLength + cipher.DefaultOverhead
		encryptedMeta := b[:readLen]
		if udpReplayCache.IsDuplicate(encryptedMeta[:cipher.DefaultOverhead], addr.String()) {
			replay.NewSession.Add(1)
			return nil, fmt.Errorf("found possible replay attack in %v", u)
		}

		// Decrypt metadata.
		var decryptedMeta []byte
		if u.block == nil && u.isClient {
			u.block = u.candidates[0].Clone()
		}
		if u.block == nil {
			var peerBlock cipher.BlockCipher
			peerBlock, decryptedMeta, err = cipher.SelectDecrypt(encryptedMeta, cipher.CloneBlockCiphers(u.candidates))
			if err != nil {
				return nil, fmt.Errorf("cipher.SelectDecrypt() failed: %w", err)
			}
			u.block = peerBlock.Clone()
		} else {
			decryptedMeta, err = u.block.Decrypt(encryptedMeta)
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
			return u.readSessionSegment(ss, b[udpNonHeaderPosition:])
		} else if isDataAckProtocol(p) {
			das := &dataAckStruct{}
			if err := das.Unmarshal(decryptedMeta); err != nil {
				return nil, fmt.Errorf("Unmarshal() failed: %w", err)
			}
			return u.readDataAckSegment(das, b[udpNonHeaderPosition:])
		}

		// TODO: handle close connection
		return nil, fmt.Errorf("unable to handle protocol %d", p)
	}
}

func (u *UDPUnderlay) readSessionSegment(ss *sessionStruct, remaining []byte) (*segment, error) {
	var decryptedPayload []byte
	var err error

	if ss.payloadLen > 0 {
		if len(remaining) < int(ss.payloadLen)+cipher.DefaultOverhead {
			return nil, fmt.Errorf("payload: received incomplete UDP packet")
		}
		decryptedPayload, err = u.block.Decrypt(remaining[:ss.payloadLen+cipher.DefaultOverhead])
		if err != nil {
			return nil, fmt.Errorf("Decrypt() failed: %w", err)
		}
	}
	if int(ss.payloadLen)+cipher.DefaultOverhead+int(ss.suffixLen) != len(remaining) {
		return nil, fmt.Errorf("padding: size not match")
	}

	return &segment{
		metadata: ss,
		payload:  decryptedPayload,
	}, nil
}

func (u *UDPUnderlay) readDataAckSegment(das *dataAckStruct, remaining []byte) (*segment, error) {
	var decryptedPayload []byte
	var err error

	if das.prefixLen > 0 {
		remaining = remaining[das.prefixLen:]
	}
	if das.payloadLen > 0 {
		if len(remaining) < int(das.payloadLen)+cipher.DefaultOverhead {
			return nil, fmt.Errorf("payload: received incomplete UDP packet")
		}
		decryptedPayload, err = u.block.Decrypt(remaining[:das.payloadLen+cipher.DefaultOverhead])
		if err != nil {
			return nil, fmt.Errorf("Decrypt() failed: %w", err)
		}
	}
	if int(das.payloadLen)+cipher.DefaultOverhead+int(das.suffixLen) != len(remaining) {
		return nil, fmt.Errorf("padding: size not match")
	}

	return &segment{
		metadata: das,
		payload:  decryptedPayload,
	}, nil
}

func (u *UDPUnderlay) writeOneSegment(seg *segment, destination *net.UDPAddr) error {
	if seg == nil {
		return stderror.ErrNullPointer
	}

	u.sendMutex.Lock()
	defer u.sendMutex.Unlock()

	if u.block == nil {
		return fmt.Errorf("%v cipher block is not ready", u)
	}
	if ss, ok := toSessionStruct(seg.metadata); ok {
		suffixLen := rng.Intn(255)
		ss.suffixLen = uint8(suffixLen)
		padding := newPadding(suffixLen)

		plaintextMetadata := ss.Marshal()
		encryptedMetadata, err := u.block.Encrypt(plaintextMetadata)
		if err != nil {
			return fmt.Errorf("Encrypt() failed: %w", err)
		}
		dataToSend := encryptedMetadata
		if len(seg.payload) > 0 {
			encryptedPayload, err := u.block.Encrypt(seg.payload)
			if err != nil {
				return fmt.Errorf("Encrypt() failed: %w", err)
			}
			dataToSend = append(dataToSend, encryptedPayload...)
		}
		dataToSend = append(dataToSend, padding...)
		if _, err := u.conn.WriteToUDP(dataToSend, destination); err != nil {
			return fmt.Errorf("WriteToUDP() failed: %w", err)
		}
	} else if das, ok := toDataAckStruct(seg.metadata); ok {
		paddingLen1 := rng.Intn(255)
		paddingLen2 := rng.Intn(255)
		das.prefixLen = uint8(paddingLen1)
		das.suffixLen = uint8(paddingLen2)
		padding1 := newPadding(paddingLen1)
		padding2 := newPadding(paddingLen2)

		plaintextMetadata := das.Marshal()
		encryptedMetadata, err := u.block.Encrypt(plaintextMetadata)
		if err != nil {
			return fmt.Errorf("Encrypt() failed: %w", err)
		}
		dataToSend := append(encryptedMetadata, padding1...)
		if len(seg.payload) > 0 {
			encryptedPayload, err := u.block.Encrypt(seg.payload)
			if err != nil {
				return fmt.Errorf("Encrypt() failed: %w", err)
			}
			dataToSend = append(dataToSend, encryptedPayload...)
		}
		dataToSend = append(dataToSend, padding2...)
		if _, err := u.conn.WriteToUDP(dataToSend, destination); err != nil {
			return fmt.Errorf("WriteToUDP() failed: %w", err)
		}
	} else if ccs, ok := toCloseConnStruct(seg.metadata); ok {
		suffixLen := rng.Intn(255)
		ccs.suffixLen = uint8(suffixLen)
		padding := newPadding(suffixLen)

		plaintextMetadata := ccs.Marshal()
		encryptedMetadata, err := u.block.Encrypt(plaintextMetadata)
		if err != nil {
			return fmt.Errorf("Encrypt() failed: %w", err)
		}
		dataToSend := encryptedMetadata
		dataToSend = append(dataToSend, padding...)
		if _, err := u.conn.WriteToUDP(dataToSend, destination); err != nil {
			return fmt.Errorf("WriteToUDP() failed: %w", err)
		}
	} else {
		return stderror.ErrInvalidArgument
	}

	return nil
}
