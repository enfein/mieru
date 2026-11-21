package protocol

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/enfein/mieru/v3/pkg/common"
)

// closeDeadlineSocket pauses the first idle deadline just before applying it
// to an actual loopback socket. Close's expired deadlines still reach the OS.
type closeDeadlineSocket struct {
	net.PacketConn
	start                     time.Time
	mu                        sync.Mutex
	trace                     []string
	idle, release, wake, read chan struct{}
	closed                    chan struct{}
	idleOnce, wakeOnce        sync.Once
	readOnce, closeOnce       sync.Once
	releaseOnce               sync.Once
}

func (c *closeDeadlineSocket) event(format string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.trace) < 64 {
		c.trace = append(c.trace, fmt.Sprintf("%s %s", time.Since(c.start), fmt.Sprintf(format, args...)))
	}
}

func (c *closeDeadlineSocket) SetReadDeadline(deadline time.Time) error {
	if time.Until(deadline) > time.Second {
		c.idleOnce.Do(func() {
			c.event("idle deadline pending")
			close(c.idle)
			<-c.release
		})
		err := c.PacketConn.SetReadDeadline(deadline)
		c.event("idle deadline applied remaining=%s err=%v", time.Until(deadline), err)
		return err
	}
	err := c.PacketConn.SetReadDeadline(deadline)
	c.event("close wake applied remaining=%s err=%v", time.Until(deadline), err)
	c.wakeOnce.Do(func() { close(c.wake) })
	return err
}

func (c *closeDeadlineSocket) ReadFrom(p []byte) (int, net.Addr, error) {
	c.event("ReadFrom entered")
	c.readOnce.Do(func() { close(c.read) })
	n, addr, err := c.PacketConn.ReadFrom(p)
	c.event("ReadFrom returned n=%d err=%v", n, err)
	return n, addr, err
}

func (c *closeDeadlineSocket) Close() error {
	err := c.PacketConn.Close()
	c.event("Socket Close returned err=%v", err)
	c.closeOnce.Do(func() { close(c.closed) })
	return err
}

func (c *closeDeadlineSocket) unblock() {
	c.releaseOnce.Do(func() { close(c.release) })
}

type closeDeadlineDialer struct{ socket *closeDeadlineSocket }

func (d closeDeadlineDialer) ListenPacket(context.Context, string, string, string) (net.PacketConn, error) {
	return d.socket, nil
}

func closeDeadlineWait(t *testing.T, c <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not finish within 2s", name)
	}
}

func TestPacketCloseOwnsReadDeadline(t *testing.T) {
	for _, afterDone := range []bool{false, true} {
		name := "rearm_during_session_close"
		if afterDone {
			name = "rearm_after_owner_close"
		}
		t.Run(name, func(t *testing.T) {
			sink, err := net.ListenPacket("udp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { sink.Close() })
			raw, err := net.ListenPacket("udp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			countBefore := runtime.NumGoroutine()
			p := &closeDeadlineSocket{PacketConn: raw, start: time.Now(),
				idle: make(chan struct{}), release: make(chan struct{}), wake: make(chan struct{}),
				read: make(chan struct{}), closed: make(chan struct{})}
			t.Cleanup(func() {
				p.unblock()
				p.mu.Lock()
				for _, e := range p.trace {
					t.Log(e)
				}
				p.mu.Unlock()
				count := runtime.NumGoroutine()
				stack := make([]byte, 512*1024)
				n := runtime.Stack(stack, true)
				t.Logf("separate samples goroutines_before=%d goroutines_after=%d full_stack_bytes=%d truncated=%v\n%s", countBefore, count, n, n == len(stack), stack[:n])
				// Only after collecting the verdict can the fixture rescue an old
				// receiver that would otherwise wait for the idle deadline.
				raw.Close()
			})
			key := cipher.HashPassword([]byte("local-close-fixture"), []byte("close-observer"))
			m := NewMux(true).SetClientUserNamePassword("close-observer", key).
				SetPacketDialer(closeDeadlineDialer{p}).
				SetEndpoints([]UnderlayProperties{NewUnderlayProperties(1400, common.PacketTransport, nil, sink.LocalAddr())})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			conn, err := m.DialContext(ctx)
			if err != nil {
				t.Fatal(err)
			}
			s := conn.(*Session)
			u := s.conn.(*PacketUnderlay)
			t.Logf("native=%s/%s go=%s GOMAXPROCS=%d mux=%p underlay=%p owner_sid=%d local=%s queue=%d/%d mux_queue=%d/%d",
				runtime.GOOS, runtime.GOARCH, runtime.Version(), runtime.GOMAXPROCS(0), m, u, s.id, raw.LocalAddr(),
				len(u.readySessions), cap(u.readySessions), len(m.chAccept), cap(m.chAccept))
			closed := make(chan struct{})
			var closeErr error
			t.Cleanup(func() {
				p.unblock()
				select {
				case <-m.done:
				default:
					m.Close()
				}
			})
			closeDeadlineWait(t, p.idle, "idle deadline barrier")
			if !afterDone {
				s.oLock.Lock()
			}
			go func() {
				p.event("Mux Close entered")
				closeErr = m.Close()
				p.event("Mux Close returned err=%v", closeErr)
				close(closed)
			}()
			if afterDone {
				closeDeadlineWait(t, closed, "Mux Close")
				p.unblock()
			} else {
				// Hold the real close path at the Session output lock until the
				// receiver has replaced Close's first wake deadline.
				func() {
					defer s.oLock.Unlock()
					closeDeadlineWait(t, p.wake, "owner wake deadline")
					select {
					case <-u.Done():
						t.Fatal("Underlay finished before releasing the Session")
					default:
					}
					p.unblock()
					closeDeadlineWait(t, p.read, "receiver entered rearmed ReadFrom")
				}()
				closeDeadlineWait(t, closed, "Mux Close")
			}
			if closeErr != nil {
				t.Fatal(closeErr)
			}
			closeDeadlineWait(t, s.closedChan, "Session Close")
			closeDeadlineWait(t, u.Done(), "Underlay Close")
			closeDeadlineWait(t, m.closeDone, "Mux Close completion")
			workers := make(chan struct{})
			go func() { s.wg.Wait(); close(workers) }()
			closeDeadlineWait(t, workers, "Session workers")
			closeDeadlineWait(t, p.closed, "receiver-owned Socket Close")
			if err := raw.SetReadDeadline(time.Now()); !errors.Is(err, net.ErrClosed) {
				t.Fatalf("Socket Close signal had no matching native close: %v", err)
			}
			t.Logf("session_created=1 session_closed=1 worker_starts=2 worker_joins=2 receiver_socket_closed=true underlay_done=true mux_done=true session_map_after_socket_close=%d",
				u.SessionCount())
		})
	}
}
