// Copyright (C) 2023  mieru authors
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

package protocolv2

import (
	"bytes"
	"context"
	crand "crypto/rand"
	mrand "math/rand"
	"sync"
	"testing"
)

type simpleInMemoryUnderlay struct {
	baseUnderlay
}

var (
	_ Underlay = &simpleInMemoryUnderlay{}
)

func (u *simpleInMemoryUnderlay) RunEventLoop(ctx context.Context) error {
	var wg sync.WaitGroup
	for _, session := range u.sessionMap {
		wg.Add(1)
		go func(s *Session) {
			for {
				select {
				case <-s.done:
					wg.Done()
					return
				default:
				}
				seg := s.sendQueue.DeleteMinBlocking()
				s.recvQueue.InsertBlocking(seg)
			}
		}(session)
	}
	wg.Wait()
	return nil
}

func TestSessionWriteRead(t *testing.T) {
	underlay := &simpleInMemoryUnderlay{
		baseUnderlay: *newBaseUnderlay(true, 4096),
	}
	var id uint32
	for {
		id = mrand.Uint32()
		if id != 0 {
			break
		}
	}
	session := NewSession(id, true, underlay.MTU())
	if err := underlay.AddSession(session); err != nil {
		t.Fatalf("AddSession() failed: %v", err)
	}
	go underlay.RunEventLoop(context.Background())

	smallData := make([]byte, 256)
	if _, err := crand.Read(smallData); err != nil {
		t.Fatalf("rand.Read() failed: %v", err)
	}
	n, err := session.Write(smallData)
	if err != nil {
		t.Fatalf("Write() failed: %v", err)
	}
	if n != 256 {
		t.Fatalf("Write %d bytes, expect %d", n, 256)
	}
	smallRecv := make([]byte, 256)
	n, err = session.Read(smallRecv)
	if err != nil {
		t.Fatalf("Read() failed: %v", err)
	}
	if n != 256 {
		t.Fatalf("Read %d bytes, expect %d", n, 256)
	}
	if !bytes.Equal(smallData, smallRecv) {
		t.Fatalf("Data not equal")
	}

	largeData := make([]byte, MaxPDU)
	if _, err := crand.Read(largeData); err != nil {
		t.Fatalf("rand.Read() failed: %v", err)
	}
	n, err = session.Write(largeData)
	if err != nil {
		t.Fatalf("Write() failed: %v", err)
	}
	if n != MaxPDU {
		t.Fatalf("Write %d bytes, expect %d", n, MaxPDU)
	}
	_, err = session.Read(smallRecv)
	if err == nil {
		t.Fatalf("Read with short buffer is not rejected")
	}
	largeRecv := make([]byte, MaxPDU)
	n, err = session.Read(largeRecv)
	if err != nil {
		t.Fatalf("Read() failed: %v", err)
	}
	if n != MaxPDU {
		t.Fatalf("Read %d bytes, expect %d", n, MaxPDU)
	}
	if !bytes.Equal(largeData, largeRecv) {
		t.Fatalf("Data not equal")
	}

	hugeData := make([]byte, MaxPDU+1)
	if _, err := crand.Read(hugeData); err != nil {
		t.Fatalf("rand.Read() failed: %v", err)
	}
	_, err = session.Write(hugeData)
	if err == nil {
		t.Fatalf("Write %d bytes is not rejected", MaxPDU+1)
	}

	underlay.Close()
}
