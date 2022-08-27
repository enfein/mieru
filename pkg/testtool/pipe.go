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
)

// BufPipe is like net.Pipe() but with an internal buffer.
func BufPipe() (io.ReadWriteCloser, io.ReadWriteCloser) {
	var buf1, buf2 bytes.Buffer
	ep1 := &ioEndpoint{
		direction: forward,
		buf1:      &buf1,
		buf2:      &buf2,
	}
	ep2 := &ioEndpoint{
		direction: backward,
		buf1:      &buf1,
		buf2:      &buf2,
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
}

func (e *ioEndpoint) Read(b []byte) (int, error) {
	if e.direction == forward {
		return e.buf2.Read(b)
	}
	return e.buf1.Read(b)
}

func (e *ioEndpoint) Write(b []byte) (int, error) {
	if e.direction == forward {
		return e.buf1.Write(b)
	}
	return e.buf2.Write(b)
}

func (e *ioEndpoint) Close() error {
	return nil
}
