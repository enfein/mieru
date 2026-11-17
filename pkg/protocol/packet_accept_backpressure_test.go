// Copyright (C) 2026 mieru contributors
// SPDX-License-Identifier: GPL-3.0-or-later
package protocol

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/protocol/serveruser"
	"github.com/enfein/mieru/v3/pkg/stderror"
)

const backlogProgressWait = 2 * time.Second

func backlogWait(t *testing.T, label string, timeout time.Duration, ready func() bool) bool {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if ready() {
			return true
		}
		select {
		case <-tick.C:
		case <-timer.C:
			t.Errorf("%s did not progress within %s", label, timeout)
			return false
		}
	}
}
func backlogDone(t *testing.T, label string, ch <-chan struct{}, timeout time.Duration) bool {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ch:
		return true
	case <-timer.C:
		t.Errorf("%s did not finish within %s", label, timeout)
		return false
	}
}
func backlogRead(s *Session, want []byte, timeout time.Duration) error {
	if err := s.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	got := make([]byte, len(want))
	n, err := io.ReadFull(s, got)
	if err != nil {
		return fmt.Errorf("ReadFull n=%d/%d: %w", n, len(want), err)
	}
	if !bytes.Equal(got, want) {
		return fmt.Errorf("payload mismatch: %q", got)
	}
	return nil
}
func backlogWrite(t *testing.T, s *Session, p []byte) {
	t.Helper()
	s.SetWriteDeadline(time.Now().Add(backlogProgressWait))
	n, err := s.Write(p)
	if n != len(p) || err != nil {
		t.Fatalf("Write n=%d/%d: %v", n, len(p), err)
	}
}

type backlogPacketConn struct {
	net.PacketConn
	writes            atomic.Int64
	once              sync.Once
	closed            chan struct{}
	mu                sync.Mutex
	target            string
	captured, release chan struct{}
	captureOnce       sync.Once
}

func (c *backlogPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, addr, err := c.PacketConn.ReadFrom(p)
	if err != nil {
		return n, addr, err
	}
	c.mu.Lock()
	match := addr.String() == c.target && c.captured != nil
	captured, release := c.captured, c.release
	c.mu.Unlock()
	if match {
		c.captureOnce.Do(func() {
			close(captured)
			select {
			case <-release:
			case <-c.closed:
			}
		})
	}
	return n, addr, err
}
func (c *backlogPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	n, err := c.PacketConn.WriteTo(p, addr)
	if err == nil {
		c.writes.Add(1)
	}
	return n, err
}
func (c *backlogPacketConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return c.PacketConn.Close()
}
func (c *backlogPacketConn) gate(addr string) (<-chan struct{}, func()) {
	c.mu.Lock()
	c.target = addr
	c.captured = make(chan struct{})
	c.release = make(chan struct{})
	c.captureOnce = sync.Once{}
	captured, release := c.captured, c.release
	c.mu.Unlock()
	var once sync.Once
	return captured, func() { once.Do(func() { close(release) }) }
}
func backlogSocket(t *testing.T) *backlogPacketConn {
	t.Helper()
	raw, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return &backlogPacketConn{PacketConn: raw, closed: make(chan struct{})}
}

// The wrapper deliberately exposes only PacketConn. Socket-buffer tuning and
// kernel burst retention are not the oracle of these handoff tests.
type backlogListener struct {
	mu      sync.Mutex
	sockets map[string]*backlogPacketConn
}

func (l *backlogListener) ListenPacket(_ context.Context, _, addr string) (net.PacketConn, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	c := l.sockets[addr]
	if c == nil {
		return nil, fmt.Errorf("unexpected listener %s", addr)
	}
	return c, nil
}

type backlogDialer struct {
	mu      sync.Mutex
	sockets []*backlogPacketConn
}

func (d *backlogDialer) ListenPacket(_ context.Context, _, _, _ string) (net.PacketConn, error) {
	raw, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	c := &backlogPacketConn{PacketConn: raw, closed: make(chan struct{})}
	d.mu.Lock()
	d.sockets = append(d.sockets, c)
	d.mu.Unlock()
	return c, nil
}

type backlogPair struct{ client, server *Session }
type backlogNetwork struct {
	t                              *testing.T
	server                         *Mux
	clients                        []*Mux
	sockets                        []*backlogPacketConn
	dialers                        []*backlogDialer
	underlays                      []*PacketUnderlay
	clientSessions, serverSessions []*Session
	release                        func()
	once                           sync.Once
}

func newBacklogNetwork(t *testing.T, endpoints int) *backlogNetwork {
	t.Helper()
	f := &backlogNetwork{t: t}
	t.Cleanup(func() { f.shutdown() })
	credential := cipher.HashPassword([]byte("synthetic-backlog-fixture"), []byte("backlog-user"))
	users := userMap(makeTestUser("backlog-user", credential))
	listener := &backlogListener{sockets: make(map[string]*backlogPacketConn)}
	props := []UnderlayProperties{}
	for i := 0; i < endpoints; i++ {
		c := backlogSocket(t)
		f.sockets = append(f.sockets, c)
		listener.sockets[c.LocalAddr().String()] = c
		props = append(props, NewUnderlayProperties(1400, common.PacketTransport, c.LocalAddr(), nil))
	}
	f.server = NewMux(false).SetServerUsers(users).SetPacketListenerFactory(listener).SetEndpoints(props)
	if err := f.server.Start(); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.sockets {
		d := &backlogDialer{}
		f.dialers = append(f.dialers, d)
		f.clients = append(f.clients, NewMux(true).SetClientUserNamePassword("backlog-user", credential).SetClientMultiplexFactor(0).SetPacketDialer(d).SetEndpoints([]UnderlayProperties{NewUnderlayProperties(1400, common.PacketTransport, nil, c.LocalAddr())}))
	}
	// Start reports listener readiness before its underlay is registered.
	if !backlogWait(t, "underlay registration", backlogProgressWait, func() bool {
		f.server.mu.Lock()
		defer f.server.mu.Unlock()
		return len(f.server.underlays) == endpoints
	}) {
		t.FailNow()
	}
	f.server.mu.Lock()
	for _, socket := range f.sockets {
		for _, u := range f.server.underlays {
			p := u.(*PacketUnderlay)
			if p.LocalAddr().String() == socket.LocalAddr().String() {
				f.underlays = append(f.underlays, p)
			}
		}
	}
	f.server.mu.Unlock()
	return f
}
func (f *backlogNetwork) dial(t *testing.T, endpoint int) *Session {
	t.Helper()
	if len(f.clientSessions) >= 136 {
		t.Fatal("fixed Session budget exceeded")
	}
	ctx, cancel := context.WithTimeout(context.Background(), backlogProgressWait)
	defer cancel()
	c, err := f.clients[endpoint].DialContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s := c.(*Session)
	f.clientSessions = append(f.clientSessions, s)
	return s
}
func (f *backlogNetwork) pending(t *testing.T, endpoint int, label string) *Session {
	s := f.dial(t, endpoint)
	backlogWrite(t, s, []byte(label))
	return s
}
func (f *backlogNetwork) accept(t *testing.T, n int, timeout time.Duration) []*Session {
	t.Helper()
	result := make(chan *Session, n)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < n; i++ {
			c, err := f.server.Accept()
			if err != nil {
				return
			}
			result <- c.(*Session)
		}
	}()
	if !backlogDone(t, "Accept consumer", done, timeout) {
		return nil
	}
	out := []*Session{}
	for len(result) > 0 {
		s := <-result
		f.serverSessions = append(f.serverSessions, s)
		out = append(out, s)
	}
	if len(out) != n {
		t.Errorf("accepted %d/%d", len(out), n)
	}
	return out
}
func (f *backlogNetwork) bootstrap(t *testing.T, endpoint int, label string) backlogPair {
	c := f.pending(t, endpoint, label)
	accepted := f.accept(t, 1, backlogProgressWait)
	if len(accepted) != 1 {
		t.FailNow()
	}
	s := accepted[0]
	if s.id != c.id {
		t.Fatal("bootstrap SID mismatch")
	}
	// Wait for the existing input-state publication before application metrics
	// accesses; no data-before-open-response window is manufactured here.
	if !backlogWait(t, "bootstrap establishment", backlogProgressWait, func() bool { return c.isState(sessionEstablished) && s.isState(sessionEstablished) }) {
		t.FailNow()
	}
	if err := backlogRead(s, []byte(label), backlogProgressWait); err != nil {
		t.Fatal(err)
	}
	reply := []byte(label + "-reply")
	backlogWrite(t, s, reply)
	if err := backlogRead(c, reply, backlogProgressWait); err != nil {
		t.Fatal(err)
	}
	if !backlogWait(t, "bootstrap ACK", backlogProgressWait, func() bool {
		return c.sendBuf.Len() == 0 && s.sendBuf.Len() == 0 && c.sendQueue.Len() == 0 && s.sendQueue.Len() == 0
	}) {
		t.FailNow()
	}
	return backlogPair{c, s}
}
func (f *backlogNetwork) shutdown() {
	f.once.Do(func() {
		if f.release != nil {
			f.release()
		}
		if f.server == nil {
			for _, s := range f.sockets {
				s.Close()
			}
			return
		}
		set := make(map[*Session]bool)
		for _, s := range f.clientSessions {
			set[s] = true
		}
		for _, s := range f.serverSessions {
			set[s] = true
		}
		for _, u := range f.underlays {
			u.sessionMap.Range(func(_, v interface{}) bool { set[v.(*Session)] = true; return true })
		}
		jobs := make(chan *Session)
		done := make(chan struct{})
		var workers sync.WaitGroup
		for i := 0; i < 4; i++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for s := range jobs {
					if f.t.Failed() {
						s.closeWithError(errors.New("test cleanup after failure"))
					} else {
						s.Close()
					}
					s.wg.Wait()
				}
			}()
		}
		go func() {
			for s := range set {
				jobs <- s
			}
			close(jobs)
			workers.Wait()
			f.server.Close()
			for _, c := range f.clients {
				c.Close()
			}
			close(done)
		}()
		if !backlogDone(f.t, "owned Session/underlay cleanup", done, 30*time.Second) {
			buf := make([]byte, 2<<20)
			n := runtime.Stack(buf, true)
			f.t.Logf("cleanup failure stack (separate sample, truncated=%v):\n%s", n == len(buf), buf[:n])
			for _, c := range f.sockets {
				c.Close()
			}
			return
		}
		for _, u := range f.underlays {
			backlogDone(f.t, "server Underlay done", u.Done(), backlogProgressWait)
		}
		for _, c := range f.sockets {
			backlogDone(f.t, "server Socket closed", c.closed, backlogProgressWait)
		}
		for _, d := range f.dialers {
			d.mu.Lock()
			list := append([]*backlogPacketConn{}, d.sockets...)
			d.mu.Unlock()
			for _, c := range list {
				backlogDone(f.t, "client Socket closed", c.closed, backlogProgressWait)
			}
		}
		// Count and full-stack sampling are deliberately not called one atomic oracle.
		count := runtime.NumGoroutine()
		buf := make([]byte, 2<<20)
		n := runtime.Stack(buf, true)
		f.t.Logf("N: joined %d explicit Session owners; runtime goroutines=%d; separate stack sample bytes=%d truncated=%v", len(set), count, n, n == len(buf))
	})
}

func TestPacketAcceptBackpressureKeepsEstablishedProgress(t *testing.T) {
	t.Logf("native GOOS=%s GOARCH=%s Go=%s GOMAXPROCS=%d", runtime.GOOS, runtime.GOARCH, runtime.Version(), runtime.GOMAXPROCS(0))
	f := newBacklogNetwork(t, 2)
	data := f.bootstrap(t, 0, "data")
	ack := f.bootstrap(t, 0, "ack")
	control := f.bootstrap(t, 0, "control")
	other := f.bootstrap(t, 1, "other")
	pending := []*Session{}
	u := f.underlays[0]
	for i := 0; i < sessionChanCapacity; i++ {
		pending = append(pending, f.pending(t, 0, fmt.Sprintf("q2-%d", i)))
		if !backlogWait(t, "fill Q2", backlogProgressWait, func() bool { return len(f.server.chAccept) == i+1 }) {
			t.FailNow()
		}
	}
	pending = append(pending, f.pending(t, 0, "held-forwarder"))
	for i := 0; i < sessionChanCapacity; i++ {
		pending = append(pending, f.pending(t, 0, fmt.Sprintf("q1-%d", i)))
		if !backlogWait(t, "fill Q1", backlogProgressWait, func() bool { return len(u.readySessions) == i+1 }) {
			t.FailNow()
		}
	}
	admittedFirst := len(pending)
	// A second underlay has its own ready queue and forwarding owner, despite the
	// shared full Mux queue. It must continue serving its established Session.
	pending = append(pending, f.pending(t, 1, "other-new"))
	last := f.dial(t, 0)
	captured, release := f.sockets[0].gate(last.LocalAddr().String())
	f.release = release
	backlogWrite(t, last, []byte("overflow"))
	pending = append(pending, last)
	if !backlogDone(t, "overflow Open captured before dispatch", captured, backlogProgressWait) {
		t.FailNow()
	}
	release()
	t.Run("A_B_both_queues_full_consumer_paused", func(t *testing.T) {
		if len(u.readySessions) != 64 || len(f.server.chAccept) != 64 {
			t.Fatalf("Q1=%d Q2=%d", len(u.readySessions), len(f.server.chAccept))
		}
		t.Log("64 Q1 +64 Q2, held forwarder and pending Open are separate states")
	})
	marker := []byte("established-data-after-full")
	dataConsumed := false
	t.Run("C_existing_DATA", func(t *testing.T) {
		backlogWrite(t, data.client, marker)
		if err := backlogRead(data.server, marker, backlogProgressWait); err != nil {
			t.Error(err)
		} else {
			dataConsumed = true
		}
	})
	t.Run("D_existing_ACK", func(t *testing.T) {
		p := []byte("ACK-needed-after-full")
		backlogWrite(t, ack.server, p)
		if err := backlogRead(ack.client, p, backlogProgressWait); err != nil {
			t.Fatal(err)
		}
		backlogWait(t, "server processing ACK", backlogProgressWait, func() bool { return ack.server.sendBuf.Len() == 0 && ack.server.sendQueue.Len() == 0 })
	})
	closed := make(chan struct{})
	go func() { control.client.Close(); close(closed) }()
	t.Run("E_existing_Close", func(t *testing.T) { backlogDone(t, "peer Close input", control.server.closedChan, backlogProgressWait) })
	wasDeferred := false
	t.Run("F_overload_before_Session_creation", func(t *testing.T) {
		_, exists := u.sessionMap.Load(last.id)
		if exists {
			t.Error("full accept backlog registered an overflow Session before admission")
		} else {
			wasDeferred = true
		}
	})
	t.Run("I_second_underlay_progress", func(t *testing.T) {
		p := []byte("independent-underlay")
		backlogWrite(t, other.client, p)
		if err := backlogRead(other.server, p, backlogProgressWait); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("G_resume_Accept_and_same_SID_retry", func(t *testing.T) {
		got := f.accept(t, len(pending), 10*time.Second)
		want := make(map[uint32]bool)
		for _, s := range pending {
			want[s.id] = true
		}
		seen := make(map[uint32]bool)
		for _, s := range got {
			if !want[s.id] || seen[s.id] {
				t.Errorf("unexpected/duplicate SID%d", s.id)
			}
			seen[s.id] = true
		}
		if len(seen) != len(want) {
			t.Errorf("unique deliveries %d/%d", len(seen), len(want))
		}
		// Cross-underlay interleaving has no global FIFO contract. Preserve the
		// order of the first underlay's already admitted handles.
		nextAdmitted := 0
		for _, s := range got {
			if s.id == pending[admittedFirst].id || s.id == last.id {
				continue
			}
			if nextAdmitted >= admittedFirst || s.id != pending[nextAdmitted].id {
				t.Error("M: admitted Session delivery order changed")
				break
			}
			nextAdmitted++
		}
		if !dataConsumed {
			if err := backlogRead(data.server, marker, backlogProgressWait); err != nil {
				t.Error(err)
			}
		}
		backlogWait(t, "ACK after recovery", backlogProgressWait, func() bool { return ack.server.sendBuf.Len() == 0 })
		backlogDone(t, "Close after recovery", control.server.closedChan, backlogProgressWait)
		backlogDone(t, "local Close returned", closed, backlogProgressWait)
		if wasDeferred {
			pc := last.conn.(*PacketUnderlay).conn.(*backlogPacketConn)
			if pc.writes.Load() < 2 {
				t.Error("deferred Open did not retry on its original Socket")
			}
			if !seen[last.id] {
				t.Error("retried SID was not accepted")
			}
		}
		if len(u.readySessions) != 0 || len(f.server.chAccept) != 0 {
			t.Error("accept queues did not drain")
		}
		next := []byte("ordered-followup")
		backlogWrite(t, data.client, next)
		if err := backlogRead(data.server, next, backlogProgressWait); err != nil {
			t.Error(err)
		}
	})
	t.Run("N_explicit_resource_owners", func(t *testing.T) { f.shutdown() })
}

// Unit-only full slots are explicit inert handles. The integration case above
// fills both queues with real authenticated network Sessions instead.
func backlogFullUnderlay(t *testing.T) (*PacketUnderlay, *backlogPacketConn, []byte) {
	t.Helper()
	conn := backlogSocket(t)
	peer := backlogSocket(t)
	credential := cipher.HashPassword([]byte("unit-only"), []byte("backlog-unit"))
	u := &PacketUnderlay{baseUnderlay: *newBaseUnderlay(false, 1400, nil), conn: conn, sessionCleanTicker: time.NewTicker(sessionCleanInterval), serverUsers: testServerUserRegistry(userMap(makeTestUser("backlog-unit", credential)), false)}
	for i := 0; i < 64; i++ {
		u.readySessions <- &Session{id: uint32(10000 + i)}
	}
	t.Cleanup(func() {
		u.sessionMap.Range(func(_, v interface{}) bool {
			s := v.(*Session)
			s.closeWithError(io.ErrClosedPipe)
			s.wg.Wait()
			return true
		})
		u.Close()
		conn.Close()
		peer.Close()
	})
	return u, peer, credential
}
func backlogAuthenticatedOpen(t *testing.T, u *PacketUnderlay, peer net.Addr, credential []byte, id uint32) *segment {
	t.Helper()
	seg := testSessionSegment(openSessionRequest, id, common.PacketTransport)
	block, err := cipher.BlockCipherFromPassword(credential, true)
	if err != nil {
		t.Fatal(err)
	}
	block.SetBlockContext(cipher.BlockContext{UserName: "backlog-unit"})
	wire := make([]byte, block.NonceSize()+MetadataLength+block.Overhead())
	if err = block.Encrypt(wire[:0], seg.metadata.Marshal()); err != nil {
		t.Fatal(err)
	}
	decoded, _, auth, err := u.serverUsers.Discover(wire, serveruser.SourceFromAddr(peer), false)
	if err != nil {
		t.Fatal(err)
	}
	seg.block = decoded
	seg.serverUserPolicy = auth.Policy()
	seg.serverUserAuthentication = auth
	return seg
}
func TestPacketOpenFullQueueAdmissionAndAuthentication(t *testing.T) {
	u, peer, credential := backlogFullUnderlay(t)
	seg := backlogAuthenticatedOpen(t, u, peer.LocalAddr(), credential, 501)
	done := make(chan error, 1)
	go func() { done <- u.onOpenSessionRequest(seg, peer.LocalAddr()) }()
	var err error
	blocked := false
	select {
	case err = <-done:
	case <-time.After(backlogProgressWait):
		blocked = true
		t.Error("A/F/K: full queue blocked new-Session admission")
	}
	if _, exists := u.sessionMap.Load(uint32(501)); exists {
		t.Error("F/K: overflow allocated and registered Session workers")
	}
	if !seg.serverUserAuthentication.Valid() {
		t.Error("M: rejected admission committed authentication")
	}
	<-u.readySessions
	if blocked {
		select {
		case <-done:
		case <-time.After(backlogProgressWait):
			t.Fatal("baseline producer did not recover after fixture drain")
		}
	} else {
		if err == nil || !stderror.IsNotReady(err) {
			t.Errorf("F: overload error=%v, want retryable not-ready", err)
		}
		if err = u.onOpenSessionRequest(seg, peer.LocalAddr()); err != nil {
			t.Fatal(err)
		}
		if seg.serverUserAuthentication.Valid() {
			t.Error("M: admitted authentication was not committed")
		}
	}
	before := len(u.readySessions)
	duplicate := backlogAuthenticatedOpen(t, u, peer.LocalAddr(), credential, 501)
	for i := 0; i < 8; i++ {
		if err = u.onOpenSessionRequest(duplicate, peer.LocalAddr()); err != nil {
			t.Error(err)
		}
	}
	if len(u.readySessions) != before {
		t.Error("H: duplicate ID re-enqueued")
	}
	if !duplicate.serverUserAuthentication.Valid() {
		t.Error("H/M: duplicate admission refreshed authentication")
	}
	count := u.SessionCount()
	if count != 1 {
		t.Errorf("H: registered Sessions=%d want1", count)
	}
}
func TestPacketOpenClosedUnderlayDoesNotRegister(t *testing.T) {
	u, peer, credential := backlogFullUnderlay(t)
	if err := u.Close(); err != nil {
		t.Fatal(err)
	}
	seg := backlogAuthenticatedOpen(t, u, peer.LocalAddr(), credential, 502)
	if err := u.onOpenSessionRequest(seg, peer.LocalAddr()); !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("J: closed-underlay error=%v", err)
	}
	if u.SessionCount() != 0 {
		t.Error("J/N: new Session registered after owner Close")
	}
	if !seg.serverUserAuthentication.Valid() {
		t.Error("J/M: closed owner committed authentication")
	}
}
func TestPacketOpenFullQueueContextCancellation(t *testing.T) {
	testPacketOpenFullQueueExit(t, false)
}

func TestPacketOpenFullQueueOwnerClose(t *testing.T) {
	testPacketOpenFullQueueExit(t, true)
}

func testPacketOpenFullQueueExit(t *testing.T, ownerClose bool) {
	u, peer, credential := backlogFullUnderlay(t)
	server := u.conn.(*backlogPacketConn)
	captured, release := server.gate(peer.LocalAddr().String())
	defer release()
	block, err := cipher.BlockCipherFromPassword(credential, true)
	if err != nil {
		t.Fatal(err)
	}
	block.SetBlockContext(cipher.BlockContext{UserName: "backlog-unit"})
	sender := &PacketUnderlay{baseUnderlay: *newBaseUnderlay(true, 1400, nil), conn: peer, serverAddr: server.LocalAddr(), block: block}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { u.RunEventLoop(ctx); close(done) }()
	t.Cleanup(func() {
		release()
		u.sessionMap.Range(func(_, v interface{}) bool {
			s := v.(*Session)
			s.closeWithError(io.ErrClosedPipe)
			s.wg.Wait()
			return true
		})
		u.Close()
		backlogDone(t, "cancelled receiver final join", done, backlogProgressWait)
	})
	if err = sender.writeOneSegment(testSessionSegment(openSessionRequest, 503, common.PacketTransport), server.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	if !backlogDone(t, "producer capture", captured, backlogProgressWait) {
		t.FailNow()
	}
	if ownerClose {
		// A consumer that exits must close its owner. Full queues never grant
		// permission to leave a receiver or a partially registered Session alive.
		closed := make(chan struct{})
		release()
		go func() { u.Close(); close(closed) }()
		backlogDone(t, "J/K: full-queue owner Close", closed, backlogProgressWait)
		backlogDone(t, "J/K: full-queue receiver exit", done, backlogProgressWait)
	} else {
		cancel()
		release()
		backlogDone(t, "L: cancelled producer event loop", done, backlogProgressWait)
	}
}
func TestPacketAcceptPreservesSlowConsumerBackpressure(t *testing.T) {
	s := NewSession(601, false, 1400, nil, nil)
	s.transportProtocol = common.StreamTransport
	for i := 0; i < segmentTreeCapacity; i++ {
		if !s.recvQueue.Insert(testDataSegment(601, uint32(i), []byte{1}, common.StreamTransport)) {
			t.Fatal("fixture queue fill")
		}
	}
	done := make(chan error, 1)
	entered := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		close(entered)
		done <- s.inputData(testDataSegment(601, segmentTreeCapacity, []byte{2}, common.StreamTransport))
	}()
	<-entered
	defer func() {
		close(s.closedChan)
		backlogDone(t, "slow-consumer input owner", finished, backlogProgressWait)
	}()
	select {
	case err := <-done:
		t.Fatalf("O/Q: slow consumer pressure disappeared: %v", err)
	default:
	}
	if s.receiveWindowSize() != 0 || s.recvQueue.Len() != segmentTreeCapacity {
		t.Fatal("O/Q: receive budget changed")
	}
	p := make([]byte, 1)
	if _, err := s.Read(p); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(backlogProgressWait):
		t.Fatal("O/Q: reader did not release pressure")
	}
	if s.recvQueue.Len() != segmentTreeCapacity {
		t.Fatal("O/Q: queue escaped its original bound")
	}
}
func TestPacketAcceptPreservesAcknowledgedClose(t *testing.T) {
	f := newBacklogNetwork(t, 1)
	pair := f.bootstrap(t, 0, "acked-close")
	payload := bytes.Repeat([]byte{0x5a}, 64*1024)
	read := make(chan error, 1)
	go func() { read <- backlogRead(pair.client, payload, 10*time.Second) }()
	backlogWrite(t, pair.server, payload)
	select {
	case err := <-read:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("R: payload incomplete")
	}
	if !backlogWait(t, "R: all payload acknowledged", backlogProgressWait, func() bool { return pair.server.sendQueue.Len() == 0 && pair.server.sendBuf.Len() == 0 }) {
		t.FailNow()
	}
	if err := pair.server.Close(); err != nil {
		t.Fatal(err)
	}
	pair.client.SetReadDeadline(time.Now().Add(backlogProgressWait))
	p := make([]byte, 1)
	if n, err := pair.client.Read(p); n != 0 || !errors.Is(err, io.EOF) {
		t.Errorf("R: drained Close n=%d err=%v", n, err)
	}
}
