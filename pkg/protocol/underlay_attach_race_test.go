package protocol

// The teardown-vs-attach race the audit located (Q04):
//
// Mux.DialContext picks an existing underlay (maybePickExistingUnderlay) and then calls
// AddSession on the reference it was handed. A teardown can run in between; Close closes the
// transport, but the caller still holds the object. Before this guard, AddSession attached the new
// session anyway, and the flow failed later as a SOCKS EOF instead of being refused at the hand-out.
//
// The contract pinned here: once an underlay is closed, AddSession refuses; and an attach that wins
// the race is fully visible to the teardown that follows, so the session is closed rather than left
// attached to a dead transport.

import (
	"errors"
	"io"
	"sync"
	"testing"
)

func TestAddSessionRejectsAClosedUnderlay(t *testing.T) {
	underlay := newBaseUnderlay(true, 1280, nil)
	underlay.closeMutex.Lock()
	if err := underlay.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}
	underlay.closeMutex.Unlock()

	session := NewSession(1, true, underlay.MTU(), nil, nil)
	err := underlay.AddSession(session, nil)
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("AddSession() on a closed underlay returned %v, want io.ErrClosedPipe", err)
	}
	if got := underlay.SessionCount(); got != 0 {
		t.Fatalf("a refused session was left attached: SessionCount()=%d", got)
	}
}

func TestAddSessionAcceptsAnOpenUnderlay(t *testing.T) {
	underlay := newBaseUnderlay(true, 1280, nil)
	session := NewSession(2, true, underlay.MTU(), nil, nil)
	if err := underlay.AddSession(session, nil); err != nil {
		t.Fatalf("AddSession() on an open underlay: %v", err)
	}
	defer func() {
		underlay.closeMutex.Lock()
		_ = underlay.Close()
		underlay.closeMutex.Unlock()
	}()
	if got := underlay.SessionCount(); got != 1 {
		t.Fatalf("SessionCount()=%d, want 1", got)
	}
}

// TestAttachAndTeardownAreSerialised is the race itself, made deterministic in both outcomes: for
// every order the attach either loses (and is refused) or wins (and the teardown closes the session
// it can now see). No interleaving may leave a session attached to a closed underlay.
func TestAttachAndTeardownAreSerialised(t *testing.T) {
	for iteration := 0; iteration < 500; iteration++ {
		underlay := newBaseUnderlay(true, 1280, nil)
		session := NewSession(uint32(iteration+1), true, underlay.MTU(), nil, nil)
		var wait sync.WaitGroup
		var attachErr error
		wait.Add(2)
		go func() {
			defer wait.Done()
			attachErr = underlay.AddSession(session, nil)
		}()
		go func() {
			defer wait.Done()
			underlay.closeMutex.Lock()
			_ = underlay.Close()
			underlay.closeMutex.Unlock()
		}()
		wait.Wait()
		if attachErr == nil {
			if !session.isStateAfter(sessionClosed, true) {
				t.Fatalf("iteration %d: a session attached to a torn-down underlay was not closed", iteration)
			}
			continue
		}
		if !errors.Is(attachErr, io.ErrClosedPipe) {
			t.Fatalf("iteration %d: unexpected attach error: %v", iteration, attachErr)
		}
	}
}
