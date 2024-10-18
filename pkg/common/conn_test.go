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
	"io"
	mrand "math/rand"
	"net"
	"testing"
)

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

type counterCloser struct {
	net.Conn
	Counter *int
}

func (cc counterCloser) Close() error {
	*cc.Counter = *cc.Counter + 1
	return nil
}

func TestHierarchyConn(t *testing.T) {
	counter := 0
	parent := WrapHierarchyConn(counterCloser{Conn: nil, Counter: &counter})
	parent.AddSubConnection(counterCloser{Conn: nil, Counter: &counter})
	parent.Close()
	if counter != 2 {
		t.Errorf("counter = %d, want %d", counter, 2)
	}
}
