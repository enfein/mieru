// Copyright (C) 2026  mieru authors
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
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher/noise"
	"github.com/enfein/mieru/v3/pkg/log"
	noiselib "github.com/flynn/noise"
)

// Noise-over-UDP wire format.
//
// Every datagram exchanged between a Noise client and a Noise server
// starts with a single type byte that distinguishes a handshake
// message from a transport (data) message:
//
//	0x00: handshake
//	  layout: type(1) | step(1) | noise handshake message bytes
//
//	0x01: transport
//	  layout: type(1) | counter(8) | AEAD(plaintext, counter)
//	          where AEAD is the cipher suite negotiated in the handshake
//
// "counter" is the 64-bit nonce consumed by noise.StatelessCipher and
// is re-created on the receiver via noise.UnsafeNewCipherState(suite,
// key, counter) so that out-of-order delivery works without per-packet
// state.
//
// Handshake retry: client retransmits its last outgoing handshake
// message every noiseUDPHandshakeRetry interval up to
// noiseUDPHandshakeMaxAttempts times. The server is stateless wrt
// retransmits — it runs its state machine forward on every fresh
// handshake packet from a known client address.
const (
	noiseUDPTypeHandshake = 0x00
	noiseUDPTypeData      = 0x01

	noiseUDPHandshakeRetry       = 1 * time.Second
	noiseUDPHandshakeMaxAttempts = 5
	noiseUDPHandshakeTotalDeadline = 15 * time.Second

	// noiseUDPSessionIdleTimeout governs when a server-side session
	// that has not seen traffic is evicted. 5 minutes matches mieru's
	// usual session lifetime and bounds memory usage under attack.
	noiseUDPSessionIdleTimeout = 5 * time.Minute
)

// --- Client wrapper --------------------------------------------------

// noiseUDPClientConn wraps a net.PacketConn and performs a Noise
// handshake against a single remote peer on Dial. After the handshake
// every Read/Write goes through the resulting StatelessCiphers.
//
// It implements net.PacketConn so it can be handed to the existing
// PacketUnderlay unchanged.
type noiseUDPClientConn struct {
	inner      net.PacketConn
	remoteAddr net.Addr

	sendCipher *noise.StatelessCipher
	recvCipher *noise.StatelessCipher
	suite      noiselib.CipherSuite

	closeOnce sync.Once
}

// dialNoiseUDP performs the full client-side Noise handshake on a
// freshly opened packet connection.
func dialNoiseUDP(inner net.PacketConn, remote net.Addr, cfg *appctlpb.NoiseConfig) (*noiseUDPClientConn, error) {
	nc, err := noise.ConfigFromProto(cfg, noise.RoleInitiator)
	if err != nil {
		return nil, fmt.Errorf("noise udp: config: %w", err)
	}
	suite, err := noise.SuiteForConfig(nc)
	if err != nil {
		return nil, fmt.Errorf("noise udp: suite: %w", err)
	}
	hs, err := noise.NewHandshake(nc)
	if err != nil {
		return nil, fmt.Errorf("noise udp: init: %w", err)
	}

	deadline := time.Now().Add(noiseUDPHandshakeTotalDeadline)
	readBuf := make([]byte, 65536)

	// Client (initiator) drives the XX/IK/etc. state machine by
	// alternating writes and reads. For every write we retransmit up
	// to noiseUDPHandshakeMaxAttempts times while waiting for the
	// peer's reply.
	var transport *noise.TransportCiphers
	isMyTurn := true // initiator writes first in every Noise pattern mieru supports
	var lastOutgoing []byte
	for time.Now().Before(deadline) {
		if isMyTurn {
			msg, done, werr := hs.WriteHandshakeMessage(nil)
			if werr != nil {
				return nil, fmt.Errorf("noise udp: write: %w", werr)
			}
			lastOutgoing = make([]byte, 2+len(msg))
			lastOutgoing[0] = noiseUDPTypeHandshake
			lastOutgoing[1] = 0 // step byte, informational only
			copy(lastOutgoing[2:], msg)
			if _, werr := inner.WriteTo(lastOutgoing, remote); werr != nil {
				return nil, fmt.Errorf("noise udp: write udp: %w", werr)
			}
			if done {
				t2, ferr := hs.Finish()
				if ferr != nil {
					return nil, fmt.Errorf("noise udp: finish: %w", ferr)
				}
				transport = t2
				break
			}
			isMyTurn = false
			continue
		}

		// Waiting for the peer. Read with per-attempt timeout; on
		// timeout retransmit our last handshake message and try again.
		var peerReply []byte
		var peerErr error
		for attempt := 0; attempt < noiseUDPHandshakeMaxAttempts; attempt++ {
			_ = inner.SetReadDeadline(time.Now().Add(noiseUDPHandshakeRetry))
			n, _, rerr := inner.ReadFrom(readBuf)
			if rerr != nil {
				var nerr net.Error
				if errors.As(rerr, &nerr) && nerr.Timeout() {
					// Retransmit the last outgoing message.
					if lastOutgoing != nil {
						_, _ = inner.WriteTo(lastOutgoing, remote)
					}
					peerErr = rerr
					continue
				}
				return nil, fmt.Errorf("noise udp: read udp: %w", rerr)
			}
			if n < 2 || readBuf[0] != noiseUDPTypeHandshake {
				peerErr = fmt.Errorf("noise udp: expected handshake packet, got type=0x%02x len=%d", readBuf[0], n)
				continue
			}
			peerReply = append([]byte(nil), readBuf[2:n]...)
			peerErr = nil
			break
		}
		_ = inner.SetReadDeadline(time.Time{})
		if peerReply == nil {
			return nil, fmt.Errorf("noise udp: no response after %d attempts: %w", noiseUDPHandshakeMaxAttempts, peerErr)
		}
		_, done2, rerr := hs.ReadHandshakeMessage(peerReply)
		if rerr != nil {
			return nil, fmt.Errorf("noise udp: peer msg: %w", rerr)
		}
		if done2 {
			t2, ferr := hs.Finish()
			if ferr != nil {
				return nil, fmt.Errorf("noise udp: finish: %w", ferr)
			}
			transport = t2
			break
		}
		isMyTurn = true
	}
	if transport == nil {
		return nil, fmt.Errorf("noise udp: handshake did not finish before deadline")
	}

	sendSC, err := noise.StatelessCipherFromTransport(transport.Send, suite)
	if err != nil {
		return nil, fmt.Errorf("noise udp: send cipher: %w", err)
	}
	recvSC, err := noise.StatelessCipherFromTransport(transport.Recv, suite)
	if err != nil {
		return nil, fmt.Errorf("noise udp: recv cipher: %w", err)
	}

	log.Debugf("Noise handshake over UDP finished: %s, remote=%v", nc.FullName(), remote)
	return &noiseUDPClientConn{
		inner:      inner,
		remoteAddr: remote,
		sendCipher: sendSC,
		recvCipher: recvSC,
		suite:      suite,
	}, nil
}

// ReadFrom waits for a data packet from the remote peer, decrypts it
// with recvCipher, and returns the plaintext. Any stray handshake
// packets from the server (after our own handshake completed) are
// silently dropped — they can occur during packet reordering.
func (c *noiseUDPClientConn) ReadFrom(p []byte) (int, net.Addr, error) {
	buf := make([]byte, 65536)
	for {
		n, addr, err := c.inner.ReadFrom(buf)
		if err != nil {
			return 0, nil, err
		}
		if n < 1 {
			continue
		}
		switch buf[0] {
		case noiseUDPTypeHandshake:
			continue
		case noiseUDPTypeData:
			pt, _, derr := c.recvCipher.Open(buf[1:n], nil)
			if derr != nil {
				log.Debugf("noise udp client: decrypt failed from %v: %v", addr, derr)
				continue
			}
			copied := copy(p, pt)
			return copied, addr, nil
		default:
			continue
		}
	}
}

// WriteTo seals p with sendCipher and writes a data packet to remote.
// addr is ignored — the wrapper is bound to a single peer.
func (c *noiseUDPClientConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	ct, err := c.sendCipher.Seal(p, nil)
	if err != nil {
		return 0, err
	}
	out := make([]byte, 1+len(ct))
	out[0] = noiseUDPTypeData
	copy(out[1:], ct)
	if _, err := c.inner.WriteTo(out, c.remoteAddr); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *noiseUDPClientConn) Close() error {
	var err error
	c.closeOnce.Do(func() { err = c.inner.Close() })
	return err
}
func (c *noiseUDPClientConn) LocalAddr() net.Addr                { return c.inner.LocalAddr() }
func (c *noiseUDPClientConn) SetDeadline(t time.Time) error      { return c.inner.SetDeadline(t) }
func (c *noiseUDPClientConn) SetReadDeadline(t time.Time) error  { return c.inner.SetReadDeadline(t) }
func (c *noiseUDPClientConn) SetWriteDeadline(t time.Time) error { return c.inner.SetWriteDeadline(t) }

// --- Server wrapper --------------------------------------------------

// noiseUDPServerConn wraps a listening net.PacketConn, runs Noise
// handshakes per client address, and then serves data packets as if
// it were the original UDP listener.
//
// Concurrency: handshakes run synchronously inside ReadFrom — if a
// handshake datagram arrives, we service it and loop back to reading
// the next packet. Data packets from addresses whose handshake has
// completed are returned to the caller. Data packets from unknown
// addresses are dropped.
type noiseUDPServerConn struct {
	inner net.PacketConn
	cfg   *appctlpb.NoiseConfig

	mu       sync.Mutex
	sessions map[string]*noiseUDPServerSession
	suite    noiselib.CipherSuite

	closeOnce sync.Once
}

type noiseUDPServerSession struct {
	mu         sync.Mutex
	handshake  *noise.Handshake
	sendCipher *noise.StatelessCipher
	recvCipher *noise.StatelessCipher
	addr       net.Addr
	lastSeen   time.Time
	established bool
}

func newNoiseUDPServerConn(inner net.PacketConn, cfg *appctlpb.NoiseConfig) (*noiseUDPServerConn, error) {
	// Validate the config up front so listener setup fails loudly.
	nc, err := noise.ConfigFromProto(cfg, noise.RoleResponder)
	if err != nil {
		return nil, fmt.Errorf("noise udp server: bad config: %w", err)
	}
	suite, err := noise.SuiteForConfig(nc)
	if err != nil {
		return nil, fmt.Errorf("noise udp server: suite: %w", err)
	}
	return &noiseUDPServerConn{
		inner:    inner,
		cfg:      cfg,
		sessions: make(map[string]*noiseUDPServerSession),
		suite:    suite,
	}, nil
}

// sessionFor returns (or creates) the session state for a given addr.
// Sessions that have been idle for longer than noiseUDPSessionIdleTimeout
// are garbage-collected opportunistically.
func (s *noiseUDPServerConn) sessionFor(addr net.Addr) (*noiseUDPServerSession, error) {
	key := addr.String()
	s.mu.Lock()
	defer s.mu.Unlock()
	// Opportunistic cleanup — cheap, bounded by session count.
	now := time.Now()
	for k, sess := range s.sessions {
		sess.mu.Lock()
		idle := now.Sub(sess.lastSeen)
		sess.mu.Unlock()
		if idle > noiseUDPSessionIdleTimeout {
			delete(s.sessions, k)
		}
	}
	if sess, ok := s.sessions[key]; ok {
		return sess, nil
	}
	nc, err := noise.ConfigFromProto(s.cfg, noise.RoleResponder)
	if err != nil {
		return nil, err
	}
	hs, err := noise.NewHandshake(nc)
	if err != nil {
		return nil, err
	}
	sess := &noiseUDPServerSession{
		handshake: hs,
		addr:      addr,
		lastSeen:  now,
	}
	s.sessions[key] = sess
	return sess, nil
}

// ReadFrom returns the plaintext of the next data packet whose sender
// has a completed handshake. Handshake packets from any address are
// consumed internally (triggering replies) and do not surface.
func (s *noiseUDPServerConn) ReadFrom(p []byte) (int, net.Addr, error) {
	buf := make([]byte, 65536)
	for {
		n, addr, err := s.inner.ReadFrom(buf)
		if err != nil {
			return 0, nil, err
		}
		if n < 1 {
			continue
		}
		switch buf[0] {
		case noiseUDPTypeHandshake:
			if err := s.handleHandshakePacket(buf[2:n], addr); err != nil {
				log.Debugf("noise udp server: handshake from %v dropped: %v", addr, err)
			}
			continue
		case noiseUDPTypeData:
			sess, err := s.sessionFor(addr)
			if err != nil {
				log.Debugf("noise udp server: sessionFor(%v) failed: %v", addr, err)
				continue
			}
			sess.mu.Lock()
			if !sess.established || sess.recvCipher == nil {
				sess.mu.Unlock()
				log.Debugf("noise udp server: data packet from unestablished %v; dropping", addr)
				continue
			}
			recv := sess.recvCipher
			sess.lastSeen = time.Now()
			sess.mu.Unlock()
			pt, _, derr := recv.Open(buf[1:n], nil)
			if derr != nil {
				log.Debugf("noise udp server: decrypt failed from %v: %v", addr, derr)
				continue
			}
			return copy(p, pt), addr, nil
		default:
			continue
		}
	}
}

// handleHandshakePacket advances the per-addr handshake state and
// writes any outgoing message back to the peer.
func (s *noiseUDPServerConn) handleHandshakePacket(msg []byte, addr net.Addr) error {
	sess, err := s.sessionFor(addr)
	if err != nil {
		return err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()

	if sess.established {
		// Client retransmitting a completed handshake? Ignore.
		return nil
	}

	if _, done, rerr := sess.handshake.ReadHandshakeMessage(msg); rerr != nil {
		return fmt.Errorf("ReadHandshakeMessage: %w", rerr)
	} else if done {
		return s.finalizeHandshake(sess)
	}

	reply, done, werr := sess.handshake.WriteHandshakeMessage(nil)
	if werr != nil {
		return fmt.Errorf("WriteHandshakeMessage: %w", werr)
	}
	out := make([]byte, 2+len(reply))
	out[0] = noiseUDPTypeHandshake
	out[1] = 0 // step informational
	copy(out[2:], reply)
	if _, err := s.inner.WriteTo(out, addr); err != nil {
		return fmt.Errorf("WriteTo: %w", err)
	}
	if done {
		return s.finalizeHandshake(sess)
	}
	sess.lastSeen = time.Now()
	return nil
}

func (s *noiseUDPServerConn) finalizeHandshake(sess *noiseUDPServerSession) error {
	t, err := sess.handshake.Finish()
	if err != nil {
		return fmt.Errorf("Finish: %w", err)
	}
	sendSC, err := noise.StatelessCipherFromTransport(t.Send, s.suite)
	if err != nil {
		return err
	}
	recvSC, err := noise.StatelessCipherFromTransport(t.Recv, s.suite)
	if err != nil {
		return err
	}
	sess.sendCipher = sendSC
	sess.recvCipher = recvSC
	sess.established = true
	sess.lastSeen = time.Now()
	log.Debugf("noise udp server: handshake completed with %v", sess.addr)
	return nil
}

// WriteTo encrypts p with the send cipher for addr and writes a data
// packet. If the addr has no completed session the write fails.
func (s *noiseUDPServerConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	sess, err := s.sessionFor(addr)
	if err != nil {
		return 0, err
	}
	sess.mu.Lock()
	if !sess.established || sess.sendCipher == nil {
		sess.mu.Unlock()
		return 0, fmt.Errorf("noise udp server: cannot send to %v, handshake not complete", addr)
	}
	send := sess.sendCipher
	sess.lastSeen = time.Now()
	sess.mu.Unlock()

	ct, err := send.Seal(p, nil)
	if err != nil {
		return 0, err
	}
	out := make([]byte, 1+len(ct))
	out[0] = noiseUDPTypeData
	copy(out[1:], ct)
	if _, err := s.inner.WriteTo(out, addr); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (s *noiseUDPServerConn) Close() error {
	var err error
	s.closeOnce.Do(func() { err = s.inner.Close() })
	return err
}
func (s *noiseUDPServerConn) LocalAddr() net.Addr                { return s.inner.LocalAddr() }
func (s *noiseUDPServerConn) SetDeadline(t time.Time) error      { return s.inner.SetDeadline(t) }
func (s *noiseUDPServerConn) SetReadDeadline(t time.Time) error  { return s.inner.SetReadDeadline(t) }
func (s *noiseUDPServerConn) SetWriteDeadline(t time.Time) error { return s.inner.SetWriteDeadline(t) }

// --- Derived inner key for PacketUnderlay -------------------------

// noiseUDPInnerPassword derives a 32-byte value that is used as the
// "password" for mieru's regular XChaCha20-Poly1305 packet framing
// when running under a Noise UDP tunnel. Both peers compute it
// deterministically from the server's static public key (extracted
// from the NoiseConfig) so that the inner mieru layer has a shared
// symmetric key without introducing another handshake.
//
// Why two layers? mieru's PacketUnderlay requires a single symmetric
// BlockCipher for its existing user-iteration path. Rather than
// rewrite that path for noise, we leave it in place with a shared
// key derived from public noise material; the real confidentiality,
// integrity and forward secrecy come from the outer Noise wrapper.
// The inner cipher is ceremonial — its "secret" is derivable by
// anyone who knows the server's static public key. That is fine
// because the outer noise AEAD already authenticates and encrypts
// every packet.
func noiseUDPInnerPassword(cfg *appctlpb.NoiseConfig) []byte {
	h := sha256.New()
	h.Write([]byte("mieru-noise-udp-inner-v1|"))
	if cfg != nil {
		// We need a value that both sides can compute identically.
		// The server's static public key satisfies that: on the client
		// it sits in remoteStaticPublicKey; on the server it sits in
		// localStaticPublicKey. Pick whichever field is set. For
		// patterns that do not expose the server static key at all
		// (NN, IN) fall back to the prologue so the derivation remains
		// deterministic — there is still confidentiality from the
		// outer Noise AEAD.
		serverStatic := cfg.GetRemoteStaticPublicKey()
		if serverStatic == "" {
			serverStatic = cfg.GetLocalStaticPublicKey()
		}
		h.Write([]byte(serverStatic))
		h.Write([]byte{0})
		h.Write([]byte(cfg.GetPrologue()))
	}
	// No time component: mieru's own key derivation (KeyRefreshInterval)
	// already rotates the per-direction keys every two minutes. An extra
	// time component here would desynchronise the password between
	// client and server at rotation boundaries.
	return h.Sum(nil)
}

// noiseUDPInnerUserName is the synthetic user name registered on both
// sides for UDP+Noise sessions. Its password is the deterministic
// inner-password derived from the noise config, so both peers agree
// without any extra handshake. Security of the composite comes from
// the outer Noise AEAD; the inner layer exists only to preserve mieru's
// existing PacketUnderlay wire format.
const noiseUDPInnerUserName = "_noise-udp-default"

// buildNoiseUDPUser returns a users map with only the synthetic noise
// user registered. Server callers pass this to Mux.SetServerUsers when
// running UDP+NOISE to bypass the normal password-based user config.
func buildNoiseUDPUser(cfg *appctlpb.NoiseConfig) map[string]*appctlpb.User {
	name := noiseUDPInnerUserName
	pw := fmt.Sprintf("%x", noiseUDPInnerPassword(cfg))
	u := &appctlpb.User{
		Name:     &name,
		Password: &pw,
	}
	return map[string]*appctlpb.User{name: u}
}

// _ keep the io import from going unused while the file is under
// active development.
var _ = io.EOF
