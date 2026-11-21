package protocol

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/enfein/mieru/v3/pkg/cipher"
)

type closeOrderSocket struct {
	net.PacketConn
	s                         *Session
	readEnter, readExit, done chan struct{}
	readStart, readStop       sync.Once
	socketClose               sync.Once
	closing                   atomic.Bool
	closedBeforeSession       atomic.Bool
	writesOK, writesFailed    atomic.Int64
}

func (c *closeOrderSocket) ReadFrom(buf []byte) (int, net.Addr, error) {
	c.readStart.Do(func() { close(c.readEnter) })
	n, from, err := c.PacketConn.ReadFrom(buf)
	c.readStop.Do(func() { close(c.readExit) })
	return n, from, err
}

func (c *closeOrderSocket) WriteTo(data []byte, addr net.Addr) (int, error) {
	n, err := c.PacketConn.WriteTo(data, addr)
	if c.closing.Load() {
		if err == nil {
			c.writesOK.Add(1)
		} else {
			c.writesFailed.Add(1)
		}
	}
	return n, err
}

func (c *closeOrderSocket) Close() error {
	err := c.PacketConn.Close()
	c.socketClose.Do(func() {
		select {
		case <-c.s.closedChan:
		default:
			c.closedBeforeSession.Store(true)
		}
		close(c.done)
	})
	return err
}

func TestPacketMuxCancelLeavesSocketForSessionClose(t *testing.T) {
	wait := func(ch <-chan struct{}, label string) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s did not complete within 2s", label)
		}
	}
	raw, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	peer, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()

	s := NewSession(901, false, 1400, nil, nil)
	key := cipher.HashPassword([]byte("mux-close-fixture"), []byte("local-observer"))
	block, err := cipher.BlockCipherFromPassword(key, true)
	if err != nil {
		t.Fatal(err)
	}
	s.block.Store(&block)
	// Model a Session that has already sent sequence zero. Its Close must
	// complete a new write rather than matching the initial lastSend value.
	s.nextSend.Store(1)
	p := &closeOrderSocket{PacketConn: raw, s: s, readEnter: make(chan struct{}),
		readExit: make(chan struct{}), done: make(chan struct{})}
	u := &PacketUnderlay{baseUnderlay: *newBaseUnderlay(false, 1400, nil),
		conn: p, sessionCleanTicker: time.NewTicker(sessionCleanInterval)}
	m := NewMux(false)
	defer m.Close()
	if err = u.AddSession(s, peer.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.underlays = append(m.underlays, u)
	m.startServerUnderlayEventLoop(m.ctx, u)
	m.mu.Unlock()
	wait(p.readEnter, "idle receiver entry")

	// Stall the owned Session at its original close-write lock while the
	// genuine Mux Close cancels Context and wakes the real UDP read.
	returned := make(chan struct{})
	p.closing.Store(true)
	var closeErr error
	func() {
		s.oLock.Lock()
		defer s.oLock.Unlock()
		go func() { closeErr = m.Close(); close(returned) }()
		wait(p.readExit, "Close wakes the receiver")
		select {
		case <-p.done:
			t.Log("receiver closed the socket while the Session Close was held")
		case <-time.After(2 * time.Second):
			t.Log("socket remained usable while the Session Close was held")
		}
	}()
	wait(returned, "Mux Close")
	wait(p.done, "receiver releases its socket")
	wait(s.closedChan, "Session close")
	joined := make(chan struct{})
	go func() { s.wg.Wait(); close(joined) }()
	wait(joined, "Session workers")
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	t.Logf("registered=1 closed=1 worker_joins=2 socket_closed_early=%v successful_socket_writes=%d failed_socket_writes=%d map_after_close=%d",
		p.closedBeforeSession.Load(), p.writesOK.Load(), p.writesFailed.Load(), u.SessionCount())
	if p.closedBeforeSession.Load() || p.writesOK.Load() == 0 || p.writesFailed.Load() != 0 || u.SessionCount() != 0 {
		t.Fatal("expected the real Socket to carry graceful Session Close before release and registration reclamation")
	}
}
