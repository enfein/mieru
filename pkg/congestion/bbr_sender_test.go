// Copyright (C) 2024  mieru authors
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

package congestion_test

import (
	"context"
	"encoding/binary"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/enfein/mieru/v3/pkg/congestion"
	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/testtool"
)

type sender struct {
	ctx      context.Context
	rwc      io.ReadWriteCloser
	nextSend int64
	nextAck  int64
	bbr      *congestion.BBRSender
	mu       sync.Mutex
}

func (s *sender) NextSend() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nextSend
}

func (s *sender) NextAck() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nextAck
}

type receiver struct {
	ctx     context.Context
	rwc     io.ReadWriteCloser
	ackSend uint64
	mu      sync.Mutex
}

func (r *receiver) AckSend() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ackSend
}

func (s *sender) Run(t *testing.T) {
	t.Helper()
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
	loop:
		for {
			select {
			case <-s.ctx.Done():
				wg.Done()
				break loop
			default:
				s.mu.Lock()
				inFlight := (s.nextSend - s.nextAck) * 8
				if !s.bbr.CanSend(inFlight, 8) {
					s.mu.Unlock()
					time.Sleep(time.Millisecond)
					continue
				}
				packetToSend := s.nextSend
				s.nextSend++
				s.mu.Unlock()

				b := make([]byte, 8)
				binary.BigEndian.PutUint64(b, uint64(packetToSend))
				if _, err := s.rwc.Write(b); err != nil {
					t.Logf("error write data: %v", err)
					time.Sleep(time.Millisecond)
					continue
				}

				s.mu.Lock()
				s.bbr.OnPacketSent(time.Now(), inFlight, packetToSend, 8, true)
				s.mu.Unlock()
			}
		}
	}()

	go func() {
	loop:
		for {
			select {
			case <-s.ctx.Done():
				wg.Done()
				break loop
			default:
				b := make([]byte, 8)
				if _, err := io.ReadFull(s.rwc, b); err != nil {
					t.Logf("error read ack: %v", err)
					time.Sleep(time.Millisecond)
					continue
				}
				now := time.Now()
				s.mu.Lock()
				s.nextAck = int64(binary.BigEndian.Uint64(b))
				inFlight := (s.nextSend - s.nextAck) * 8
				s.bbr.OnCongestionEvent(inFlight, now, []congestion.AckedPacketInfo{{
					PacketNumber:     s.nextAck - 1,
					BytesAcked:       8,
					ReceiveTimestamp: now,
				}}, nil)
				s.mu.Unlock()
			}
		}
	}()

	wg.Wait()
}

func (r *receiver) Start(t *testing.T) {
	t.Helper()
	go func() {
		for {
			select {
			case <-r.ctx.Done():
				return
			default:
				b := make([]byte, 8)
				if _, err := io.ReadFull(r.rwc, b); err != nil {
					t.Logf("error read data: %v", err)
					time.Sleep(time.Millisecond)
					continue
				}
				r.mu.Lock()
				r.ackSend = binary.BigEndian.Uint64(b)
				binary.BigEndian.PutUint64(b, r.ackSend+1)
				r.mu.Unlock()
				if _, err := r.rwc.Write(b); err != nil {
					t.Logf("error write ack: %v", err)
					return
				}
			}
		}
	}()
}

func TestBBRSender(t *testing.T) {
	log.SetOutputToTest(t)
	log.SetLevel("DEBUG")

	e1, e2 := testtool.BufPipe()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second))
	defer cancel()
	s := &sender{
		ctx: ctx,
		rwc: e1,
		bbr: congestion.NewBBRSender("Test", nil),
	}
	r := &receiver{
		ctx: ctx,
		rwc: e2,
	}
	r.Start(t)
	s.Run(t)
	t.Logf("nextSend: %v", s.NextSend())
	t.Logf("nextAck: %v", s.NextAck())
	t.Logf("ackSend: %v", r.AckSend())
	t.Logf("Estimated bandwidth: %d B/s", s.bbr.BandwidthEstimate())
}
