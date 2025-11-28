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
// along with this program.  If not, see <https://www.gnu.org/licenses/>

package protocol

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/metrics"
	"github.com/enfein/mieru/v3/pkg/stderror"
)

const (
	// Number of ready sessions before they are consumed by Accept().
	sessionChanCapacity = 64

	sessionCleanInterval = 5 * time.Second
)

// baseUnderlay contains a partial implementation of underlay.
type baseUnderlay struct {
	isClient bool
	mtu      int
	done     chan struct{} // if the underlay is closed

	sessionMap    sync.Map      // Map<sessionID, *Session>
	readySessions chan *Session // sessions that completed handshake and ready for consume

	sendMutex  sync.Mutex // protect writing data to the connection
	closeMutex sync.Mutex // protect closing the connection

	inBytes  atomic.Int64
	outBytes atomic.Int64

	// ---- client fields ----
	scheduler *ScheduleController
}

var (
	_ Underlay = &baseUnderlay{}
)

func newBaseUnderlay(isClient bool, mtu int) *baseUnderlay {
	return &baseUnderlay{
		isClient:      isClient,
		mtu:           mtu,
		done:          make(chan struct{}),
		readySessions: make(chan *Session, sessionChanCapacity),
		scheduler:     &ScheduleController{},
	}
}

// Accept implements net.Listener interface.
func (b *baseUnderlay) Accept() (net.Conn, error) {
	select {
	case session := <-b.readySessions:
		return session, nil
	case <-b.done:
		return nil, io.ErrClosedPipe
	}
}

// Close implements net.Listener interface. The caller must hold closeMutex lock.
func (b *baseUnderlay) Close() error {
	select {
	case <-b.done:
		return nil
	default:
	}

	b.sessionMap.Range(func(k, v any) bool {
		s := v.(*Session)
		s.Close()
		s.wg.Wait()
		s.conn = nil
		s = nil
		return true
	})
	close(b.done)
	UnderlayCurrEstablished.Add(-1)
	return nil
}

// Addr implements net.Listener interface.
func (b *baseUnderlay) Addr() net.Addr {
	return common.NilNetAddr()
}

func (b *baseUnderlay) MTU() int {
	return b.mtu
}

func (b *baseUnderlay) TransportProtocol() common.TransportProtocol {
	return common.UnknownTransport
}

func (b *baseUnderlay) LocalAddr() net.Addr {
	return common.NilNetAddr()
}

func (b *baseUnderlay) RemoteAddr() net.Addr {
	return common.NilNetAddr()
}

func (b *baseUnderlay) AddSession(s *Session, remoteAddr net.Addr) error {
	if s == nil {
		return stderror.ErrNullPointer
	}
	if s.id == 0 {
		return fmt.Errorf("session ID can't be 0")
	}
	if s.isStateAfter(sessionAttached, true) {
		return fmt.Errorf("session %d is already attached to a underlay", s.id)
	}
	if b.isClient && !s.isClient {
		return fmt.Errorf("can't add a server session to a client underlay")
	}
	if !b.isClient && s.isClient {
		return fmt.Errorf("can't add a client session to a server underlay")
	}
	if _, loaded := b.sessionMap.LoadOrStore(s.id, s); loaded {
		return stderror.ErrAlreadyExist
	}
	s.conn = b
	s.remoteAddr = remoteAddr

	if s.isClient {
		metrics.ActiveOpens.Add(1)
	} else {
		metrics.PassiveOpens.Add(1)
	}
	currEst := metrics.CurrEstablished.Add(1)
	maxConn := metrics.MaxConn.Load()
	if currEst > maxConn {
		metrics.MaxConn.Store(currEst)
	}
	return nil
}

func (b *baseUnderlay) RemoveSession(s *Session) error {
	if s == nil {
		return stderror.ErrNullPointer
	}
	if s.isStateBefore(sessionAttached, false) {
		return fmt.Errorf("session %d is not attached to this underlay", s.id)
	}

	b.sessionMap.Delete(s.id)
	s.Close()
	s.wg.Wait()
	s.conn = nil
	s = nil
	return nil
}

func (b *baseUnderlay) SessionCount() int {
	n := 0
	b.sessionMap.Range(func(k, v any) bool {
		n++
		return true
	})
	return n
}

func (b *baseUnderlay) SessionInfos() []*appctlpb.SessionInfo {
	res := make([]*appctlpb.SessionInfo, 0)
	b.sessionMap.Range(func(k, v any) bool {
		s := v.(*Session)
		res = append(res, s.ToSessionInfo())
		return true
	})
	return res
}

func (b *baseUnderlay) InBytes() int64 {
	return b.inBytes.Load()
}

func (b *baseUnderlay) OutBytes() int64 {
	return b.outBytes.Load()
}

func (b *baseUnderlay) RunEventLoop(ctx context.Context) error {
	return stderror.ErrUnsupported
}

func (b *baseUnderlay) Scheduler() *ScheduleController {
	return b.scheduler
}

func (b *baseUnderlay) Done() chan struct{} {
	return b.done
}
