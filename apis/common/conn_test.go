// Copyright (C) 2025  mieru authors
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

package common_test

import (
	"net"
	"testing"

	"github.com/enfein/mieru/v3/apis/common"
)

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
	parent := common.WrapHierarchyConn(counterCloser{Conn: nil, Counter: &counter})
	parent.Add(counterCloser{Conn: nil, Counter: &counter})
	parent.Close()
	if counter != 2 {
		t.Errorf("counter = %d, want %d", counter, 2)
	}
}
