// Copyright (C) 2022  mieru authors
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

package common

import (
	"bytes"
	crand "crypto/rand"
	"errors"
	"io"
	mrand "math/rand"
	"net"
	"os"
	"testing"
	"time"
)

func TestWriteTimeoutConnStopsBlockedWrite(t *testing.T) {
	conn, peer := net.Pipe()
	defer conn.Close()
	defer peer.Close()

	timeout := 10 * time.Millisecond
	wrapped := NewWriteTimeoutConn(conn, timeout)
	started := time.Now()
	_, err := wrapped.Write([]byte("blocked"))
	if err == nil {
		t.Fatal("Write() succeeded while the peer was not reading")
	}
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("Write() error = %v, want a timeout", err)
	}
	if elapsed := time.Since(started); elapsed < timeout {
		t.Fatalf("Write() returned after %v, before timeout %v", elapsed, timeout)
	}
}

func TestIdleTrackingConnActivity(t *testing.T) {
	for _, test := range []struct {
		name  string
		write bool
		n     int
		err   error
	}{
		{name: "read", n: 1},
		{name: "write", write: true, n: 1},
		{name: "partial read with error", n: 1, err: io.EOF},
		{name: "partial write with error", write: true, n: 1, err: io.ErrClosedPipe},
		{name: "empty read"},
		{name: "empty write", write: true},
		{name: "read deadline", err: os.ErrDeadlineExceeded},
		{name: "write deadline", write: true, err: os.ErrDeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := &idleResultConn{n: test.n, err: test.err}
			conn := &IdleTrackingConn{Conn: raw, lastActivity: time.Now().Add(-10 * time.Minute)}
			var n int
			var err error
			if test.write {
				n, err = conn.Write([]byte{1})
			} else {
				n, err = conn.Read(make([]byte, 1))
			}
			if n != test.n || !errors.Is(err, test.err) {
				t.Fatalf("I/O = (%d, %v), want (%d, %v)", n, err, test.n, test.err)
			}
			wantClosed := test.n == 0
			if got := conn.CloseIfIdle(5 * time.Minute); got != wantClosed || raw.closed != wantClosed {
				t.Fatalf("CloseIfIdle() = %v, closed = %v, want %v", got, raw.closed, wantClosed)
			}
		})
	}
}

func TestReadAllAndDiscard(t *testing.T) {
	n := mrand.Int63n(1024*1024) + 1
	buf := bytes.NewBuffer(make([]byte, n))
	if _, err := io.CopyN(buf, crand.Reader, n); err != nil {
		t.Fatalf("Generating random data failed: %v", err)
	}

	ReadAllAndDiscard(buf)

	if buf.Len() != 0 {
		t.Errorf("buf.Len() = %d, want 0", buf.Len())
	}
}

type idleResultConn struct {
	net.Conn
	n      int
	err    error
	closed bool
}

func (c *idleResultConn) Read([]byte) (int, error)  { return c.n, c.err }
func (c *idleResultConn) Write([]byte) (int, error) { return c.n, c.err }
func (c *idleResultConn) Close() error {
	c.closed = true
	return nil
}
