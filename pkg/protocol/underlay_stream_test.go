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

func TestStreamUnderlayEventLoopTerminalTimeout(t *testing.T) {
	conn := &readErrorConn{readErr: terminalTimeoutError{}}
	underlay := &StreamUnderlay{
		baseUnderlay:       *newBaseUnderlay(true, 1400, nil),
		conn:               conn,
		sessionCleanTicker: time.NewTicker(time.Hour),
	}
	defer underlay.sessionCleanTicker.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	loopDone := make(chan error, 1)
	go func() {
		loopDone <- underlay.RunEventLoop(ctx)
	}()

	select {
	case err := <-loopDone:
		if !errors.Is(err, net.ErrClosed) {
			t.Errorf("RunEventLoop() error = %v, want error wrapping net.ErrClosed", err)
		}
		if got := conn.readCalls.Load(); got != 1 {
			t.Errorf("Read() calls = %d, want 1; terminal timeouts must not be retried", got)
		}
		if !conn.closed.Load() {
			t.Error("RunEventLoop() did not close the connection")
		}
	case <-time.After(time.Second):
		// Stop a loop that incorrectly retries the terminal timeout.
		cancel()
		select {
		case <-loopDone:
		case <-time.After(time.Second):
			t.Error("RunEventLoop() did not stop after cancellation")
		}
		t.Fatalf("RunEventLoop() did not stop after terminal timeout; Read() calls = %d", conn.readCalls.Load())
	}
}

func TestStreamUnderlayReadDeadline(t *testing.T) {
	reader, writer := net.Pipe()
	defer reader.Close()
	defer writer.Close()
	underlay := &StreamUnderlay{conn: shortReadDeadlineConn{reader}}

	// With no data available, a read deadline lets the event loop retry.
	segment, err := underlay.readOneSegment()
	if segment != nil || err != nil {
		t.Fatalf("readOneSegment() = (%v, %v), want (nil, nil) so the event loop can retry", segment, err)
	}

	// A subsequent read must still report the peer closing the connection.
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() failed: %v", err)
	}
	segment, err = underlay.readOneSegment()
	if segment != nil {
		t.Errorf("readOneSegment() segment = %v, want nil", segment)
	}
	if !errors.Is(err, io.EOF) {
		t.Errorf("readOneSegment() error = %v, want error wrapping io.EOF", err)
	}
}

func TestStreamUnderlayTimeoutClassification(t *testing.T) {
	tests := []struct {
		name      string
		readErr   error
		readBytes int
		wantRetry bool
	}{
		{
			name:    "terminal timeout",
			readErr: terminalTimeoutError{},
		},
		{
			name:    "wrapped terminal timeout",
			readErr: &net.OpError{Op: "read", Net: "quic", Err: terminalTimeoutError{}},
		},
		{
			name:      "read deadline",
			readErr:   os.ErrDeadlineExceeded,
			wantRetry: true,
		},
		{
			name:      "wrapped read deadline",
			readErr:   &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded},
			wantRetry: true,
		},
		{
			// Retrying after consuming metadata would lose segment framing.
			name:      "partial metadata timeout",
			readErr:   os.ErrDeadlineExceeded,
			readBytes: 1,
		},
		{
			name:    "closed connection",
			readErr: net.ErrClosed,
		},
		{
			name:    "end of stream",
			readErr: io.EOF,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conn := &readErrorConn{readErr: test.readErr, readBytes: test.readBytes}
			underlay := &StreamUnderlay{conn: conn}
			segment, err := underlay.readOneSegment()
			if segment != nil {
				t.Fatalf("readOneSegment() segment = %v, want nil", segment)
			}
			if test.wantRetry {
				if err != nil {
					t.Fatalf("readOneSegment() error = %v, want nil so the event loop can retry", err)
				}
				return
			}
			if !errors.Is(err, test.readErr) {
				t.Errorf("readOneSegment() error = %v, want error wrapping %v", err, test.readErr)
			}
			if got := stderror.GetErrorType(err); got != stderror.NETWORK_ERROR {
				t.Errorf("readOneSegment() error type = %v, want NETWORK_ERROR", got)
			}
		})
	}
}

// terminalTimeoutError models a transport timeout that also closes the
// connection.
type terminalTimeoutError struct{}

func (terminalTimeoutError) Error() string   { return "terminal transport timeout" }
func (terminalTimeoutError) Timeout() bool   { return true }
func (terminalTimeoutError) Temporary() bool { return false }
func (terminalTimeoutError) Unwrap() error   { return net.ErrClosed }

// readErrorConn returns a configured read result and records event-loop cleanup.
// Only Read, SetReadDeadline, and Close are supported; other methods are unused.
type readErrorConn struct {
	net.Conn
	readErr   error
	readBytes int
	readCalls atomic.Int64
	closed    atomic.Bool
}

func (c *readErrorConn) Read([]byte) (int, error) {
	c.readCalls.Add(1)
	return c.readBytes, c.readErr
}

func (c *readErrorConn) SetReadDeadline(time.Time) error { return nil }

func (c *readErrorConn) Close() error {
	c.closed.Store(true)
	return nil
}

// shortReadDeadlineConn exercises a real net.Conn deadline without waiting for
// the production read timeout. Zero deadlines still clear the timeout normally.
type shortReadDeadlineConn struct{ net.Conn }

func (c shortReadDeadlineConn) SetReadDeadline(deadline time.Time) error {
	if !deadline.IsZero() {
		deadline = time.Now().Add(100 * time.Millisecond)
	}
	return c.Conn.SetReadDeadline(deadline)
}
