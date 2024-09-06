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

package testtool

import (
	"bytes"
	"errors"
	"io"
	"net"
	"runtime"
	"sync"
	"time"

	"github.com/enfein/mieru/pkg/util"
)

// BufPipe is like net.Pipe() but with an internal buffer.
func BufPipe() (net.Conn, net.Conn) {
	var buf1, buf2 bytes.Buffer
	var lock1, lock2 sync.Mutex
	ep1 := &ioEndpoint{
		direction: forward,
		buf1:      &buf1,
		buf2:      &buf2,
		lock1:     &lock1,
		lock2:     &lock2,
	}
	ep2 := &ioEndpoint{
		direction: backward,
		buf1:      &buf1,
		buf2:      &buf2,
		lock1:     &lock1,
		lock2:     &lock2,
	}
	return ep1, ep2
}

type ioDirection int

const (
	forward ioDirection = iota
	backward
)

type ioEndpoint struct {
	direction ioDirection
	buf1      *bytes.Buffer // forward writes to here
	buf2      *bytes.Buffer // backward writes to here
	lock1     *sync.Mutex   // lock of buf1
	lock2     *sync.Mutex   // lock of buf2
	closed    bool
}

var _ net.Conn = &ioEndpoint{}

func (e *ioEndpoint) Read(b []byte) (n int, err error) {
	if e.closed {
		return 0, io.EOF
	}
	if e.direction == forward {
		e.lock2.Lock()
		n, err = e.buf2.Read(b)
		e.lock2.Unlock()
	} else {
		e.lock1.Lock()
		n, err = e.buf1.Read(b)
		e.lock1.Unlock()
	}
	if errors.Is(err, io.EOF) {
		// io.ReadFull() with partial result will not fail.
		err = nil
		// Allow the writer to catch up.
		runtime.Gosched()
	}
	return
}

func (e *ioEndpoint) Write(b []byte) (n int, err error) {
	if e.closed {
		return 0, io.ErrClosedPipe
	}
	if e.direction == forward {
		e.lock1.Lock()
		n, err = e.buf1.Write(b)
		e.lock1.Unlock()
	} else {
		e.lock2.Lock()
		n, err = e.buf2.Write(b)
		e.lock2.Unlock()
	}
	return
}

func (e *ioEndpoint) Close() error {
	e.closed = true
	return nil
}

func (e *ioEndpoint) LocalAddr() net.Addr {
	return util.NilNetAddr()
}

func (e *ioEndpoint) RemoteAddr() net.Addr {
	return util.NilNetAddr()
}

func (e *ioEndpoint) SetDeadline(t time.Time) error {
	return nil
}

func (e *ioEndpoint) SetReadDeadline(t time.Time) error {
	return nil
}

func (e *ioEndpoint) SetWriteDeadline(t time.Time) error {
	return nil
}
