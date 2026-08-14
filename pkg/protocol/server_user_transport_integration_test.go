// Copyright (C) 2026  mieru authors
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
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/stderror"
)

type serverUserTestRemoteConn struct {
	net.Conn
	remoteAddr net.Addr
}

func (c *serverUserTestRemoteConn) RemoteAddr() net.Addr {
	return c.remoteAddr
}

func TestStreamServerSourceUserCacheIntegration(t *testing.T) {
	const userName = "stream-cache-user"
	credential := cipher.HashPassword([]byte(t.Name()), []byte(userName))
	mux := NewMux(false).SetServerUsers(userMap(makeTestUser(userName, credential)))
	t.Cleanup(func() { _ = mux.Close() })

	firstAddr := &net.TCPAddr{IP: net.ParseIP("198.51.100.20"), Port: 12001}
	firstWire := buildServerUserStreamWire(t, credential, userName, testSessionSegment(openSessionRequest, 1, common.StreamTransport), nil, nil)
	firstUnderlay, firstSegment, err := readServerUserStreamWire(t, mux, firstAddr, firstWire)
	if err != nil {
		t.Fatalf("first readOneSegment() failed: %v", err)
	}
	if got := firstSegment.serverUserAuthentication.origin; got != serverUserMatchRegistryHint {
		t.Fatalf("first discovery origin = %v, want registry hint", got)
	}
	state := mux.serverUsers.Load()
	if got := state.cache.stats.insertions.Load(); got != 0 {
		t.Fatalf("cache insertions before initial dispatch = %d, want 0", got)
	}
	firstUnderlay.commitServerUserAuthentication(firstSegment)
	if firstSegment.serverUserAuthentication.generation != nil {
		t.Fatal("TCP commit retained the discovery generation")
	}

	// The second socket uses a different source port but the same source IP.
	secondAddr := &net.TCPAddr{IP: net.ParseIP("198.51.100.20"), Port: 12002}
	secondWire := buildServerUserStreamWire(t, credential, userName, testSessionSegment(openSessionRequest, 2, common.StreamTransport), nil, nil)
	_, secondSegment, err := readServerUserStreamWire(t, mux, secondAddr, secondWire)
	if err != nil {
		t.Fatalf("second readOneSegment() failed: %v", err)
	}
	if got := secondSegment.serverUserAuthentication.origin; got != serverUserMatchCachedHint {
		t.Fatalf("second discovery origin = %v, want cached hint", got)
	}

	// TCP and UDP share the cache owned by the same user generation.
	packet := &PacketUnderlay{
		serverUsers:               &mux.serverUsers,
		serverUserHintIsMandatory: &mux.serverUserHintIsMandatory,
	}
	metadata := testSessionSegment(openSessionRequest, 3, common.PacketTransport).metadata.Marshal()
	encrypted := encryptDiscoveryMetadata(t, credential, userName, userMap(makeTestUser(userName, credential)), true, metadata)
	source := serverUserDiscoverySource{}
	source.key, source.valid = sourceUserCacheKey(&net.UDPAddr{IP: net.ParseIP("198.51.100.20"), Port: 32000})
	_, _, authentication, err := packet.serverTryDecryptMetadataForNewSession(encrypted, source)
	if err != nil {
		t.Fatalf("UDP discovery after TCP activity failed: %v", err)
	}
	if authentication.origin != serverUserMatchCachedHint {
		t.Fatalf("UDP discovery after TCP activity origin = %v, want cached hint", authentication.origin)
	}
}

func TestStreamServerInvalidInitialSegmentsDoNotRefreshCache(t *testing.T) {
	const userName = "stream-invalid-user"
	credential := cipher.HashPassword([]byte(t.Name()), []byte(userName))
	users := userMap(makeTestUser(userName, credential))
	mux := NewMux(false).SetServerUsers(users)
	t.Cleanup(func() { _ = mux.Close() })
	remoteAddr := &net.TCPAddr{IP: net.ParseIP("203.0.113.30"), Port: 13001}

	validPayload := []byte("authenticated payload")
	invalidPayloadTag := buildServerUserStreamWire(t, credential, userName, testSessionSegment(openSessionRequest, 10, common.StreamTransport), validPayload, nil)
	invalidPayloadTag[len(invalidPayloadTag)-1] ^= 0x80
	invalidPadding := buildServerUserStreamWire(t, credential, userName, testSessionSegment(openSessionRequest, 11, common.StreamTransport), nil, []byte{0})
	invalidPadding = invalidPadding[:len(invalidPadding)-1]

	tests := []struct {
		name      string
		wire      []byte
		errorType stderror.ErrorType
	}{
		{
			name:      "invalid_payload_tag",
			wire:      invalidPayloadTag,
			errorType: stderror.CRYPTO_ERROR,
		},
		{
			name:      "invalid_padding",
			wire:      invalidPadding,
			errorType: stderror.NETWORK_ERROR,
		},
		{
			name:      "wrong_direction",
			wire:      buildServerUserStreamWire(t, credential, userName, testSessionSegment(openSessionResponse, 12, common.StreamTransport), nil, nil),
			errorType: stderror.PROTOCOL_ERROR,
		},
		{
			name:      "reserved_session_id",
			wire:      buildServerUserStreamWire(t, credential, userName, testSessionSegment(openSessionRequest, 0, common.StreamTransport), nil, nil),
			errorType: stderror.PROTOCOL_ERROR,
		},
		{
			name:      "malformed_metadata",
			wire:      buildServerUserStreamWire(t, credential, userName, nil, nil, nil),
			errorType: stderror.PROTOCOL_ERROR,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := mux.serverUserCacheStats.insertions.Load()
			_, seg, err := readServerUserStreamWire(t, mux, remoteAddr, test.wire)
			if err == nil && seg.serverUserAuthentication.valid() {
				err = validateNewServerSessionSegment(seg)
				if err != nil {
					err = stderror.WrapErrorWithType(err, stderror.PROTOCOL_ERROR)
				}
			}
			if err == nil {
				t.Fatal("readOneSegment() succeeded, want rejection")
			}
			if got := stderror.GetErrorType(err); got != test.errorType {
				t.Fatalf("error type = %v, want %v: %v", got, test.errorType, err)
			}
			if got := mux.serverUserCacheStats.insertions.Load(); got != before {
				t.Fatalf("cache insertions changed from %d to %d", before, got)
			}
		})
	}
}

func TestStreamServerReplayDoesNotRefreshCache(t *testing.T) {
	const userName = "stream-replay-user"
	credential := cipher.HashPassword([]byte(t.Name()), []byte(userName))
	mux := NewMux(false).SetServerUsers(userMap(makeTestUser(userName, credential)))
	t.Cleanup(func() { _ = mux.Close() })
	remoteAddr := &net.TCPAddr{IP: net.ParseIP("192.0.2.40"), Port: 14001}
	wire := buildServerUserStreamWire(t, credential, userName, testSessionSegment(openSessionRequest, 20, common.StreamTransport), nil, nil)

	_, first, err := readServerUserStreamWire(t, mux, remoteAddr, wire)
	if err != nil {
		t.Fatalf("first readOneSegment() failed: %v", err)
	}
	// Do not dispatch or commit the first copy. The identical second metadata
	// must be rejected by replay protection and still leave the cache cold.
	first.serverUserAuthentication.generation = nil
	_, _, err = readServerUserStreamWire(t, mux, remoteAddr, wire)
	if err == nil || stderror.GetErrorType(err) != stderror.REPLAY_ERROR {
		t.Fatalf("replayed readOneSegment() error = %v, want replay error", err)
	}
	if got := mux.serverUserCacheStats.insertions.Load(); got != 0 {
		t.Fatalf("cache insertions after replay = %d, want 0", got)
	}
}

func TestPacketServerSourceUserCacheIntegration(t *testing.T) {
	const userName = "packet-cache-user"
	credential := cipher.HashPassword([]byte(t.Name()), []byte(userName))
	mux := NewMux(false).SetServerUsers(userMap(makeTestUser(userName, credential)))
	t.Cleanup(func() { _ = mux.Close() })

	serverConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.ListenPacket() server failed: %v", err)
	}
	server := &PacketUnderlay{
		baseUnderlay:              *newBaseUnderlay(false, 1400, nil),
		conn:                      serverConn,
		sessionCleanTicker:        time.NewTicker(sessionCleanInterval),
		serverUsers:               &mux.serverUsers,
		serverUserHintIsMandatory: &mux.serverUserHintIsMandatory,
	}
	t.Cleanup(func() {
		_ = server.Close()
		_ = serverConn.Close()
	})

	first := newServerUserPacketSender(t, serverConn.LocalAddr(), credential, userName)
	defer first.conn.Close()
	second := newServerUserPacketSender(t, serverConn.LocalAddr(), credential, userName)
	defer second.conn.Close()

	// An authenticated wrong-direction control is discarded inside parsing and
	// cannot populate the source cache.
	if err := first.writeOneSegment(testSessionSegment(openSessionResponse, 30, common.PacketTransport), serverConn.LocalAddr()); err != nil {
		t.Fatalf("writeOneSegment(wrong direction) failed: %v", err)
	}
	if err := first.writeOneSegment(testSessionSegment(openSessionRequest, 31, common.PacketTransport), serverConn.LocalAddr()); err != nil {
		t.Fatalf("writeOneSegment(first open) failed: %v", err)
	}
	firstSegment, firstAddr, err := server.readOneSegment()
	if err != nil {
		t.Fatalf("first readOneSegment() failed: %v", err)
	}
	if firstSegment.serverUserAuthentication.origin != serverUserMatchRegistryHint {
		t.Fatalf("first UDP discovery origin = %v, want registry hint", firstSegment.serverUserAuthentication.origin)
	}
	if got := mux.serverUserCacheStats.insertions.Load(); got != 0 {
		t.Fatalf("UDP cache insertions before dispatch = %d, want 0", got)
	}
	if err := server.onOpenSessionRequest(firstSegment, firstAddr); err != nil {
		t.Fatalf("onOpenSessionRequest(first) failed: %v", err)
	}

	// A duplicate ID authenticated from a different source port is dropped by
	// dispatch and must not refresh the cache or enqueue another session.
	insertionsBeforeDuplicate := mux.serverUserCacheStats.insertions.Load()
	readyBeforeDuplicate := len(server.readySessions)
	if err := second.writeOneSegment(testSessionSegment(openSessionRequest, 31, common.PacketTransport), serverConn.LocalAddr()); err != nil {
		t.Fatalf("writeOneSegment(duplicate open) failed: %v", err)
	}
	duplicateSegment, duplicateAddr, err := server.readOneSegment()
	if err != nil {
		t.Fatalf("duplicate readOneSegment() failed: %v", err)
	}
	if err := server.onOpenSessionRequest(duplicateSegment, duplicateAddr); err != nil {
		t.Fatalf("onOpenSessionRequest(duplicate) failed: %v", err)
	}
	if got := mux.serverUserCacheStats.insertions.Load(); got != insertionsBeforeDuplicate {
		t.Fatalf("duplicate UDP open changed cache insertions from %d to %d", insertionsBeforeDuplicate, got)
	}
	if got := len(server.readySessions); got != readyBeforeDuplicate {
		t.Fatalf("duplicate UDP open changed ready session count from %d to %d", readyBeforeDuplicate, got)
	}

	// A second source port on the same IP uses the learned candidate.
	if err := second.writeOneSegment(testSessionSegment(openSessionRequest, 32, common.PacketTransport), serverConn.LocalAddr()); err != nil {
		t.Fatalf("writeOneSegment(second open) failed: %v", err)
	}
	secondSegment, secondAddr, err := server.readOneSegment()
	if err != nil {
		t.Fatalf("second readOneSegment() failed: %v", err)
	}
	if secondSegment.serverUserAuthentication.origin != serverUserMatchCachedHint {
		t.Fatalf("second UDP discovery origin = %v, want cached hint", secondSegment.serverUserAuthentication.origin)
	}
	if err := server.onOpenSessionRequest(secondSegment, secondAddr); err != nil {
		t.Fatalf("onOpenSessionRequest(second) failed: %v", err)
	}

	// Once the first session has installed its block, packets from its original
	// address stay on the existing-session path and do not query the user cache.
	firstSessionValue, ok := server.sessionMap.Load(uint32(31))
	if !ok {
		t.Fatal("first UDP session was not installed")
	}
	firstSession := firstSessionValue.(*Session)
	waitForServerUserSessionBlock(t, firstSession)
	lookupsBefore := mux.serverUserCacheStats.lookups.Load()
	data := testDataSegment(31, 1, []byte("existing session"), common.PacketTransport)
	if err := first.writeOneSegment(data, serverConn.LocalAddr()); err != nil {
		t.Fatalf("writeOneSegment(existing data) failed: %v", err)
	}
	existingSegment, _, err := server.readOneSegment()
	if err != nil {
		t.Fatalf("existing-session readOneSegment() failed: %v", err)
	}
	if existingSegment.serverUserAuthentication.valid() {
		t.Fatal("existing UDP session unexpectedly performed user discovery")
	}
	if got := mux.serverUserCacheStats.lookups.Load(); got != lookupsBefore {
		t.Fatalf("existing UDP session changed cache lookups from %d to %d", lookupsBefore, got)
	}

	// UDP-learned activity is immediately visible to TCP underlays sharing the
	// same generation.
	stream := &StreamUnderlay{
		serverUsers:               &mux.serverUsers,
		serverUserHintIsMandatory: &mux.serverUserHintIsMandatory,
	}
	stream.serverUserSource.key, stream.serverUserSource.valid = sourceUserCacheKey(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 45000})
	metadata := testSessionSegment(openSessionRequest, 33, common.StreamTransport).metadata.Marshal()
	encrypted := encryptDiscoveryMetadata(t, credential, userName, userMap(makeTestUser(userName, credential)), true, metadata)
	_, authentication, err := stream.serverInitRecvBlockCipherAndDecryptMetadata(encrypted)
	if err != nil {
		t.Fatalf("TCP discovery after UDP activity failed: %v", err)
	}
	if authentication.origin != serverUserMatchCachedHint {
		t.Fatalf("TCP discovery after UDP activity origin = %v, want cached hint", authentication.origin)
	}
}

func TestPacketServerInvalidNewSessionsDoNotRefreshCache(t *testing.T) {
	const userName = "packet-invalid-user"
	credential := cipher.HashPassword([]byte(t.Name()), []byte(userName))
	mux := NewMux(false).SetServerUsers(userMap(makeTestUser(userName, credential)))
	t.Cleanup(func() { _ = mux.Close() })
	serverConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.ListenPacket() server failed: %v", err)
	}
	defer serverConn.Close()
	server := &PacketUnderlay{
		baseUnderlay:              *newBaseUnderlay(false, 1400, nil),
		conn:                      serverConn,
		serverUsers:               &mux.serverUsers,
		serverUserHintIsMandatory: &mux.serverUserHintIsMandatory,
	}
	senderConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.ListenPacket() sender failed: %v", err)
	}
	defer senderConn.Close()
	replayConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.ListenPacket() replay sender failed: %v", err)
	}
	defer replayConn.Close()

	invalidPayloadTag := buildServerUserPacketWire(t, credential, userName, testSessionSegment(openSessionRequest, 50, common.PacketTransport), []byte("authenticated payload"), nil)
	invalidPayloadTag[len(invalidPayloadTag)-1] ^= 0x80
	invalidPadding := buildServerUserPacketWire(t, credential, userName, testSessionSegment(openSessionRequest, 51, common.PacketTransport), nil, []byte{0})
	invalidPadding = invalidPadding[:len(invalidPadding)-1]
	tests := []struct {
		name string
		wire []byte
	}{
		{"invalid_payload_tag", invalidPayloadTag},
		{"invalid_padding", invalidPadding},
		{"wrong_direction", buildServerUserPacketWire(t, credential, userName, testSessionSegment(openSessionResponse, 52, common.PacketTransport), nil, nil)},
		{"reserved_session_id", buildServerUserPacketWire(t, credential, userName, testSessionSegment(openSessionRequest, 0, common.PacketTransport), nil, nil)},
		{"malformed_metadata", buildServerUserPacketWire(t, credential, userName, nil, nil, nil)},
	}

	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := senderConn.WriteTo(test.wire, serverConn.LocalAddr()); err != nil {
				t.Fatalf("WriteTo(invalid) failed: %v", err)
			}
			valid := buildServerUserPacketWire(t, credential, userName, testSessionSegment(openSessionRequest, uint32(60+i), common.PacketTransport), nil, nil)
			if _, err := senderConn.WriteTo(valid, serverConn.LocalAddr()); err != nil {
				t.Fatalf("WriteTo(valid) failed: %v", err)
			}
			seg, _, err := server.readOneSegment()
			if err != nil {
				t.Fatalf("readOneSegment() failed: %v", err)
			}
			if got, _ := seg.SessionID(); got != uint32(60+i) {
				t.Fatalf("returned session ID = %d, want %d", got, 60+i)
			}
			if got := mux.serverUserCacheStats.insertions.Load(); got != 0 {
				t.Fatalf("cache insertions after invalid packet = %d, want 0", got)
			}
			seg.serverUserAuthentication.generation = nil
		})
	}

	// The first copy is authenticated but intentionally not dispatched. The
	// replayed copy is discarded, and a fresh packet unblocks the read loop.
	replayed := buildServerUserPacketWire(t, credential, userName, testSessionSegment(openSessionRequest, 70, common.PacketTransport), nil, nil)
	if _, err := senderConn.WriteTo(replayed, serverConn.LocalAddr()); err != nil {
		t.Fatalf("WriteTo(first replay copy) failed: %v", err)
	}
	first, _, err := server.readOneSegment()
	if err != nil {
		t.Fatalf("readOneSegment(first replay copy) failed: %v", err)
	}
	first.serverUserAuthentication.generation = nil
	if _, err := replayConn.WriteTo(replayed, serverConn.LocalAddr()); err != nil {
		t.Fatalf("WriteTo(replay) failed: %v", err)
	}
	fresh := buildServerUserPacketWire(t, credential, userName, testSessionSegment(openSessionRequest, 71, common.PacketTransport), nil, nil)
	if _, err := senderConn.WriteTo(fresh, serverConn.LocalAddr()); err != nil {
		t.Fatalf("WriteTo(fresh) failed: %v", err)
	}
	seg, _, err := server.readOneSegment()
	if err != nil {
		t.Fatalf("readOneSegment(after replay) failed: %v", err)
	}
	if got, _ := seg.SessionID(); got != 71 {
		t.Fatalf("returned session ID after replay = %d, want 71", got)
	}
	if got := mux.serverUserCacheStats.insertions.Load(); got != 0 {
		t.Fatalf("cache insertions after replay = %d, want 0", got)
	}
}

func TestPacketServerReloadMandatoryHintAndStatsRace(t *testing.T) {
	const userName = "packet-race-user"
	credential := cipher.HashPassword([]byte(t.Name()), []byte(userName))
	users := userMap(makeTestUser(userName, credential))
	mux := NewMux(false).SetServerUsers(users)
	t.Cleanup(func() { _ = mux.Close() })
	packet := &PacketUnderlay{
		serverUsers:               &mux.serverUsers,
		serverUserHintIsMandatory: &mux.serverUserHintIsMandatory,
	}
	source := serverUserDiscoverySource{}
	source.key, source.valid = sourceUserCacheKey(&net.UDPAddr{IP: net.ParseIP("192.0.2.60"), Port: 16001})
	metadata := testSessionSegment(openSessionRequest, 80, common.PacketTransport).metadata.Marshal()
	encrypted := encryptDiscoveryMetadata(t, credential, userName, users, true, metadata)

	const iterations = 16
	errCh := make(chan error, iterations)
	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			mux.SetServerUsers(users)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			mux.SetServerUserHintIsMandatory(i%2 == 0)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_, _, authentication, err := packet.serverTryDecryptMetadataForNewSession(encrypted, source)
			if err != nil {
				errCh <- err
				continue
			}
			authentication.recordAuthenticated()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = mux.serverUserCacheStats.lookups.Load()
			_ = mux.serverUserCacheStats.sourceHits.Load()
			_ = mux.serverUserCacheStats.insertions.Load()
		}
	}()
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent UDP discovery failed: %v", err)
	}
}

func TestRetiredGenerationAuthenticationRecordIsNoOp(t *testing.T) {
	const userName = "retired-record-user"
	credential := cipher.HashPassword([]byte(t.Name()), []byte(userName))
	mux := NewMux(false).SetServerUsers(userMap(makeTestUser(userName, credential)))
	t.Cleanup(func() { _ = mux.Close() })
	old := mux.serverUsers.Load()
	source := serverUserDiscoverySource{}
	source.key, source.valid = sourceUserCacheKey(&net.UDPAddr{IP: net.ParseIP("192.0.2.50"), Port: 15001})
	metadata := testSessionSegment(openSessionRequest, 40, common.PacketTransport).metadata.Marshal()
	encrypted := encryptDiscoveryMetadata(t, credential, userName, userMap(makeTestUser(userName, credential)), true, metadata)
	packet := &PacketUnderlay{
		serverUsers:               &mux.serverUsers,
		serverUserHintIsMandatory: &mux.serverUserHintIsMandatory,
	}
	_, _, authentication, err := packet.serverTryDecryptMetadataForNewSession(encrypted, source)
	if err != nil {
		t.Fatalf("serverTryDecryptMetadataForNewSession() failed: %v", err)
	}

	mux.SetServerUsers(rawUserMap("replacement-user", "replacement-password"))
	before := mux.serverUserCacheStats.insertions.Load()
	authentication.recordAuthenticated()
	if got := mux.serverUserCacheStats.insertions.Load(); got != before {
		t.Fatalf("retired cache record changed insertions from %d to %d", before, got)
	}
	if authentication.generation != nil {
		t.Fatal("retired cache record retained its generation")
	}
	if old.cache.loadTable() != nil {
		t.Fatal("old generation cache was republished")
	}
}

func readServerUserStreamWire(t *testing.T, mux *Mux, remoteAddr net.Addr, wire []byte) (*StreamUnderlay, *segment, error) {
	t.Helper()
	reader, writer := net.Pipe()
	conn := &serverUserTestRemoteConn{Conn: reader, remoteAddr: remoteAddr}
	underlay := mux.serverWrapTCPConn(conn, 1400, nil).(*StreamUnderlay)
	writeDone := make(chan struct{})
	go func() {
		_, _ = writer.Write(wire)
		_ = writer.Close()
		close(writeDone)
	}()
	seg, err := underlay.readOneSegment()
	_ = reader.Close()
	underlay.sessionCleanTicker.Stop()
	select {
	case <-writeDone:
	case <-time.After(time.Second):
		t.Fatal("stream test writer didn't stop")
	}
	return underlay, seg, err
}

func buildServerUserStreamWire(t *testing.T, credential []byte, userName string, seg *segment, payload, padding []byte) []byte {
	t.Helper()
	var plaintextMetadata []byte
	if seg == nil {
		plaintextMetadata = newDummyMetadata()
	} else {
		ss, ok := seg.metadata.(*sessionStruct)
		if !ok {
			t.Fatalf("stream test metadata type = %T, want sessionStruct", seg.metadata)
		}
		ss.payloadLen = uint16(len(payload))
		ss.suffixLen = uint8(len(padding))
		plaintextMetadata = ss.Marshal()
	}
	block, err := cipher.BlockCipherFromPassword(credential, false)
	if err != nil {
		t.Fatalf("BlockCipherFromPassword() failed: %v", err)
	}
	block.SetBlockContext(cipher.BlockContext{UserName: userName})
	encryptedMetadata, err := block.Encrypt(plaintextMetadata)
	if err != nil {
		t.Fatalf("Encrypt(metadata) failed: %v", err)
	}
	wire := append([]byte(nil), encryptedMetadata...)
	if len(payload) > 0 {
		encryptedPayload, err := block.Encrypt(payload)
		if err != nil {
			t.Fatalf("Encrypt(payload) failed: %v", err)
		}
		wire = append(wire, encryptedPayload...)
	}
	wire = append(wire, padding...)
	return wire
}

func buildServerUserPacketWire(t *testing.T, credential []byte, userName string, seg *segment, payload, padding []byte) []byte {
	t.Helper()
	var plaintextMetadata []byte
	if seg == nil {
		plaintextMetadata = newDummyMetadata()
	} else {
		ss, ok := seg.metadata.(*sessionStruct)
		if !ok {
			t.Fatalf("packet test metadata type = %T, want sessionStruct", seg.metadata)
		}
		ss.payloadLen = uint16(len(payload))
		ss.suffixLen = uint8(len(padding))
		plaintextMetadata = ss.Marshal()
	}
	block, err := cipher.BlockCipherFromPassword(credential, true)
	if err != nil {
		t.Fatalf("BlockCipherFromPassword() failed: %v", err)
	}
	block.SetBlockContext(cipher.BlockContext{UserName: userName})
	encryptedMetadata, err := block.Encrypt(plaintextMetadata)
	if err != nil {
		t.Fatalf("Encrypt(metadata) failed: %v", err)
	}
	nonce := encryptedMetadata[:cipher.DefaultNonceSize]
	wire := append([]byte(nil), encryptedMetadata...)
	if len(payload) > 0 {
		encryptedPayload, err := block.EncryptWithNonce(payload, nonce)
		if err != nil {
			t.Fatalf("EncryptWithNonce(payload) failed: %v", err)
		}
		wire = append(wire, encryptedPayload...)
	}
	wire = append(wire, padding...)
	return wire
}

func newServerUserPacketSender(t *testing.T, serverAddr net.Addr, credential []byte, userName string) *PacketUnderlay {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.ListenPacket() sender failed: %v", err)
	}
	block, err := cipher.BlockCipherFromPassword(credential, true)
	if err != nil {
		conn.Close()
		t.Fatalf("BlockCipherFromPassword() failed: %v", err)
	}
	block.SetBlockContext(cipher.BlockContext{UserName: userName})
	return &PacketUnderlay{
		baseUnderlay: *newBaseUnderlay(true, 1400, nil),
		conn:         conn,
		serverAddr:   serverAddr,
		block:        block,
	}
}

func waitForServerUserSessionBlock(t *testing.T, session *Session) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for session.block.Load() == nil {
		if time.Now().After(deadline) {
			t.Fatal("session didn't install its block cipher")
		}
		time.Sleep(time.Millisecond)
	}
}

func (o serverUserMatchOrigin) String() string {
	switch o {
	case serverUserMatchCachedHint:
		return "cached hint"
	case serverUserMatchRegistryHint:
		return "registry hint"
	case serverUserMatchCachedFallback:
		return "cached fallback"
	case serverUserMatchRegistryFallback:
		return "registry fallback"
	default:
		return fmt.Sprintf("unknown(%d)", o)
	}
}
