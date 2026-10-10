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
	"context"
	"errors"
	"io"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/enfein/mieru/v3/pkg/stderror"
)

// Stand-in for a terminal net.Error, with no dependency on a QUIC implementation.
type terminalTimeoutError struct{}

func (terminalTimeoutError) Error() string   { return "terminal transport timeout" }
func (terminalTimeoutError) Timeout() bool   { return true }
func (terminalTimeoutError) Temporary() bool { return false }
func (terminalTimeoutError) Unwrap() error   { return net.ErrClosed }

type timeoutTestConn struct {
	net.Conn
	err    error
	n      int
	reads  atomic.Int64
	closed atomic.Bool
}

func (c *timeoutTestConn) Read([]byte) (int, error) {
	c.reads.Add(1)
	return c.n, c.err
}
func (c *timeoutTestConn) SetReadDeadline(time.Time) error { return nil }
func (c *timeoutTestConn) Close() error                    { c.closed.Store(true); return nil }

func TestStreamUnderlayEventLoopTerminalTimeout(t *testing.T) {
	c := &timeoutTestConn{err: terminalTimeoutError{}}
	u := &StreamUnderlay{baseUnderlay: *newBaseUnderlay(true, 1400, nil), conn: c, sessionCleanTicker: time.NewTicker(time.Hour)}
	defer u.sessionCleanTicker.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- u.RunEventLoop(ctx) }()
	select {
	case err := <-result:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("terminal error lost: %v", err)
		}
		if c.reads.Load() != 1 || !c.closed.Load() {
			t.Fatalf("reads=%d closed=%v", c.reads.Load(), c.closed.Load())
		}
	case <-time.After(time.Second):
		cancel()
		<-result
		t.Fatalf("terminal timeout did not terminate: reads=%d", c.reads.Load())
	}
}

// Real TCP-style read deadline must remain retryable. Clamp only the test socket's deadline.
type deadlineTestPipe struct{ net.Conn }

func (c deadlineTestPipe) SetReadDeadline(d time.Time) error {
	if !d.IsZero() {
		d = time.Now().Add(100 * time.Millisecond)
	}
	return c.Conn.SetReadDeadline(d)
}
func TestStreamUnderlayReadDeadline(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	u := &StreamUnderlay{conn: deadlineTestPipe{a}}
	seg, err := u.readOneSegment()
	if seg != nil || err != nil {
		t.Fatalf("deadline should retry: %v", err)
	}
	b.Close()
	_, err = u.readOneSegment()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("EOF lost: %v", err)
	}
}

func TestStreamUnderlayTimeoutClassification(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		n     int
		retry bool
	}{
		{"terminal timeout", terminalTimeoutError{}, 0, false},
		{"wrapped terminal timeout", &net.OpError{Op: "read", Net: "quic", Err: terminalTimeoutError{}}, 0, false},
		{"read deadline", os.ErrDeadlineExceeded, 0, true},
		{"wrapped read deadline", &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}, 0, true},
		{"partial metadata timeout", os.ErrDeadlineExceeded, 1, false},
		{"closed connection", net.ErrClosed, 0, false},
		{"end of stream", io.EOF, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			underlay := &StreamUnderlay{conn: &timeoutTestConn{err: tc.err, n: tc.n}}
			segment, err := underlay.readOneSegment()
			if segment != nil {
				t.Fatal("unexpected segment")
			}
			if tc.retry {
				if err != nil {
					t.Fatalf("read deadline should be retryable: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.err) || stderror.GetErrorType(err) != stderror.NETWORK_ERROR {
				t.Fatalf("got %v, want network error wrapping %v", err, tc.err)
			}
		})
	}
}
