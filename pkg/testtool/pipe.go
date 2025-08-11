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
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/enfein/mieru/v3/pkg/common"
)

// BufPipe is like net.Pipe() but with an internal buffer.
func BufPipe() (net.Conn, net.Conn) {
	var buf1, buf2 bytes.Buffer
	var lock1, lock2 sync.Mutex
	cond1 := sync.NewCond(&lock1) // endpoint 1 has data to read
	cond2 := sync.NewCond(&lock2) // endpoint 2 has data to read

	ep1 := &ioEndpoint{
		direction: forward,
		buf1:      &buf1,
		buf2:      &buf2,
		lock1:     &lock1,
		lock2:     &lock2,
		cond1:     cond1,
		cond2:     cond2,
	}
	ep2 := &ioEndpoint{
		direction: backward,
		buf1:      &buf1,
		buf2:      &buf2,
		lock1:     &lock1,
		lock2:     &lock2,
		cond1:     cond1,
		cond2:     cond2,
	}
	ep1.peer = ep2
	ep2.peer = ep1
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
	cond1     *sync.Cond
	cond2     *sync.Cond
	closed    atomic.Bool
	peer      *ioEndpoint
}

var _ net.Conn = &ioEndpoint{}

func (e *ioEndpoint) Read(b []byte) (n int, err error) {
	if e.closed.Load() {
		return 0, io.EOF
	}

	var buffer *bytes.Buffer
	var lock *sync.Mutex
	var cond *sync.Cond

	if e.direction == forward {
		buffer = e.buf2
		lock = e.lock2
		cond = e.cond2
	} else {
		buffer = e.buf1
		lock = e.lock1
		cond = e.cond1
	}

	lock.Lock()
	defer lock.Unlock()

	for buffer.Len() == 0 {
		if e.closed.Load() || e.peer.closed.Load() {
			return 0, io.EOF
		}
		cond.Wait()
	}

	return buffer.Read(b)
}

func (e *ioEndpoint) Write(b []byte) (n int, err error) {
	if e.closed.Load() {
		return 0, io.ErrClosedPipe
	}

	var buffer *bytes.Buffer
	var lock *sync.Mutex
	var cond *sync.Cond

	if e.direction == forward {
		buffer = e.buf1
		lock = e.lock1
		cond = e.cond1
	} else {
		buffer = e.buf2
		lock = e.lock2
		cond = e.cond2
	}

	lock.Lock()
	defer lock.Unlock()

	if e.peer.closed.Load() {
		return 0, io.ErrClosedPipe
	}

	n, err = buffer.Write(b)
	cond.Signal()
	return
}

func (e *ioEndpoint) Close() error {
	if e.closed.Swap(true) {
		return nil
	}
	e.cond1.Broadcast()
	e.cond2.Broadcast()
	return nil
}

func (e *ioEndpoint) LocalAddr() net.Addr {
	return common.NilNetAddr()
}

func (e *ioEndpoint) RemoteAddr() net.Addr {
	return common.NilNetAddr()
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
