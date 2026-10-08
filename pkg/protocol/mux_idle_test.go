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

package protocol

import (
	"net"
	"testing"
	"time"
)

// newTestServerUnderlay returns a server side stream underlay that is not
// connected to any peer. This is enough to exercise the idle reclamation
// decision in cleanUnderlay().
func newTestServerUnderlay(t *testing.T) *StreamUnderlay {
	t.Helper()
	local, remote := net.Pipe()
	t.Cleanup(func() {
		local.Close()
		remote.Close()
	})
	underlay := &StreamUnderlay{
		baseUnderlay:       *newBaseUnderlay(false, 1400, nil),
		conn:               local,
		sessionCleanTicker: time.NewTicker(sessionCleanInterval),
	}
	UnderlayCurrEstablished.Add(1)
	return underlay
}

func TestActiveSessionCount(t *testing.T) {
	tests := []struct {
		name     string
		sessions []bool // true if the session is already closed
		want     int
	}{
		{
			name: "no session",
			want: 0,
		},
		{
			name:     "one active session",
			sessions: []bool{false},
			want:     1,
		},
		{
			name:     "one closed session",
			sessions: []bool{true},
			want:     0,
		},
		{
			name:     "mixed sessions",
			sessions: []bool{false, true, false, true},
			want:     2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			underlay := &baseUnderlay{
				done:          make(chan struct{}),
				readySessions: make(chan *Session, sessionChanCapacity),
			}
			for i, closed := range tc.sessions {
				session := &Session{id: uint32(i + 1), closedChan: make(chan struct{})}
				if closed {
					close(session.closedChan)
				}
				underlay.sessionMap.Store(session.id, session)
			}
			if got := underlay.ActiveSessionCount(); got != tc.want {
				t.Fatalf("ActiveSessionCount() = %d, want %d", got, tc.want)
			}
			if got := underlay.SessionCount(); got != len(tc.sessions) {
				t.Fatalf("SessionCount() = %d, want %d", got, len(tc.sessions))
			}
		})
	}
}

func TestCleanUnderlayClosesIdleServerUnderlay(t *testing.T) {
	const idleTimeout = 2 * time.Minute

	tests := []struct {
		name          string
		isClient      bool
		idleTimeout   time.Duration
		idleFor       time.Duration
		sessions      int
		wantRemaining int
	}{
		{
			name:          "reclamation is disabled by default",
			idleTimeout:   0,
			idleFor:       10 * time.Minute,
			wantRemaining: 1,
		},
		{
			name:          "recent activity is kept",
			idleTimeout:   idleTimeout,
			idleFor:       30 * time.Second,
			wantRemaining: 1,
		},
		{
			name:          "negative timeout disables reclamation",
			idleTimeout:   -1,
			idleFor:       10 * time.Minute,
			wantRemaining: 1,
		},
		{
			name:          "idle without session is reclaimed",
			idleTimeout:   idleTimeout,
			idleFor:       5 * time.Minute,
			wantRemaining: 0,
		},
		{
			name:          "idle with session is kept",
			idleTimeout:   idleTimeout,
			idleFor:       5 * time.Minute,
			sessions:      1,
			wantRemaining: 1,
		},
		{
			name:          "client mux never reclaims",
			isClient:      true,
			idleTimeout:   idleTimeout,
			idleFor:       5 * time.Minute,
			wantRemaining: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Do not use NewMux() here, so the background underlay cleaner
			// does not run concurrently with this test.
			mux := &Mux{
				isClient:                  tc.isClient,
				done:                      make(chan struct{}),
				serverUnderlayIdleTimeout: tc.idleTimeout,
			}
			underlay := newTestServerUnderlay(t)
			underlay.lastActive.Store(time.Now().Add(-tc.idleFor).UnixNano())
			for i := 0; i < tc.sessions; i++ {
				session := &Session{id: uint32(i + 1)}
				underlay.sessionMap.Store(session.id, session)
			}
			mux.underlays = append(mux.underlays, underlay)

			mux.mu.Lock()
			mux.cleanUnderlay(false)
			mux.mu.Unlock()

			if got := len(mux.underlays); got != tc.wantRemaining {
				t.Fatalf("after cleanUnderlay(), mux has %d underlays, want %d", got, tc.wantRemaining)
			}
			if tc.wantRemaining == 0 {
				select {
				case <-underlay.Done():
				default:
					t.Fatalf("reclaimed underlay is still open")
				}
			}
			UnderlayCurrEstablished.Add(-1)
		})
	}
}
