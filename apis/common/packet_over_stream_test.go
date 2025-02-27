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
	"testing"

	"github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/pkg/testtool"
)

func TestUDPAssociateTunnelConn(t *testing.T) {
	in, out := testtool.BufPipe()
	inConn := common.WrapPacketOverStream(in)
	outConn := common.WrapPacketOverStream(out)

	data := []byte{8, 9, 6, 4}
	n, err := inConn.Write(data)
	if err != nil {
		t.Fatalf("Write() failed: %v", err)
	}
	if n != 4 {
		t.Fatalf("Write() returns %d, want %d", n, 4)
	}

	buf := make([]byte, 16)
	n, err = outConn.Read(buf)
	if err != nil {
		t.Fatalf("Read() failed: %v", err)
	}
	if n != 4 {
		t.Fatalf("Read() returns %d, want %d", n, 4)
	}

	if err = inConn.Close(); err != nil {
		t.Errorf("Close() failed: %v", err)
	}
	if err = outConn.Close(); err != nil {
		t.Errorf("Close() failed: %v", err)
	}
}
