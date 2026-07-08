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
	"io"
	"net"
	"time"
)

const DefaultIdleTimeout = 15 * time.Minute

type setReadDeadliner interface {
	SetReadDeadline(t time.Time) error
}

// timeoutReader wraps a reader and sets a read deadline on the underlying
// connection before each read. This prevents io.Copy from hanging forever
// when the remote end accepts the TCP connection but never sends data.
type timeoutReader struct {
	r       io.Reader
	timeout time.Duration
	conn    setReadDeadliner
}

func (tr *timeoutReader) Read(p []byte) (int, error) {
	tr.conn.SetReadDeadline(time.Now().Add(tr.timeout))
	return tr.r.Read(p)
}

// BidiCopy does bi-directional data copy.
// An idle timeout of DefaultIdleTimeout is applied to prevent hanging on
// connections that accept TCP but never send data.
// This timeout resets on each successful read, so active transfers are not
// affected.
func BidiCopy(conn1, conn2 io.ReadWriteCloser) error {
	return bidiCopyWithTimeout(conn1, conn2, DefaultIdleTimeout)
}

func bidiCopyWithTimeout(conn1, conn2 io.ReadWriteCloser, idleTimeout time.Duration) error {
	errCh := make(chan error, 2)

	var deadline1, deadline2 setReadDeadliner
	if idleTimeout > 0 {
		if c, ok := conn1.(net.Conn); ok {
			deadline1 = c
		}
		if c, ok := conn2.(net.Conn); ok {
			deadline2 = c
		}
	}

	go func() {
		r := io.Reader(conn2)
		if deadline2 != nil {
			r = &timeoutReader{r: conn2, timeout: idleTimeout, conn: deadline2}
		}
		_, err := io.Copy(conn1, r)
		conn1.Close()
		errCh <- err
	}()
	go func() {
		r := io.Reader(conn1)
		if deadline1 != nil {
			r = &timeoutReader{r: conn1, timeout: idleTimeout, conn: deadline1}
		}
		_, err := io.Copy(conn2, r)
		conn2.Close()
		errCh <- err
	}()

	err := <-errCh
	<-errCh
	return err
}
