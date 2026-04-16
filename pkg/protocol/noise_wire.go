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
	"fmt"
	"net"
	"time"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/enfein/mieru/v3/pkg/cipher/noise"
	"github.com/enfein/mieru/v3/pkg/log"
)

// noiseHandshakeDeadline is the total wall-clock time allowed for a
// single Noise handshake on a fresh connection. It is kept short so a
// stuck client cannot tie up a server indefinitely.
const noiseHandshakeDeadline = 15 * time.Second

// noiseHandshakeStream performs a full Noise handshake on conn using
// the given proto config and role, and returns a pair of BlockCiphers
// bound to the resulting transport CipherStates.
//
// The caller is expected to hand sendCipher to a StreamUnderlay as the
// outbound cipher and recvCipher as the inbound cipher. Both ciphers
// are stateful and must not be cloned by the caller; the protocol
// layer already bypasses the clone path when they are pre-populated
// (see StreamUnderlay.maybeInitSendBlockCipher).
//
// On any error the connection is left intact so the caller can close
// it and propagate the failure up.
func noiseHandshakeStream(conn net.Conn, cfg *appctlpb.NoiseConfig, role noise.Role) (sendCipher, recvCipher cipher.BlockCipher, err error) {
	if cfg == nil {
		return nil, nil, fmt.Errorf("noise handshake: nil NoiseConfig")
	}
	nc, err := noise.ConfigFromProto(cfg, role)
	if err != nil {
		return nil, nil, fmt.Errorf("noise handshake: bad config: %w", err)
	}

	// Bound the overall handshake duration. SetDeadline applies to both
	// reads and writes; on completion we clear it so normal mieru
	// traffic can use its own timeouts.
	deadline := time.Now().Add(noiseHandshakeDeadline)
	_ = conn.SetDeadline(deadline)
	defer func() {
		_ = conn.SetDeadline(time.Time{})
	}()

	hs, err := noise.NewHandshake(nc)
	if err != nil {
		return nil, nil, fmt.Errorf("noise handshake: init: %w", err)
	}

	transport, err := hs.Run(conn, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("noise handshake: run: %w", err)
	}
	if transport == nil || transport.Send == nil || transport.Recv == nil {
		return nil, nil, fmt.Errorf("noise handshake: incomplete transport state")
	}

	log.Debugf("Noise handshake finished: %s, remote=%v", nc.FullName(), conn.RemoteAddr())
	return cipher.NewNoiseBlockCipher(transport.Send),
		cipher.NewNoiseBlockCipher(transport.Recv),
		nil
}

// noiseValidateUDP rejects NOISE encryption on UDP transports until a
// proper datagram-aware handshake exists. It is called from the mux's
// newUnderlay() before attempting to dial a UDP peer.
func noiseValidateUDP() error {
	return fmt.Errorf("noise encryption is not supported over UDP yet; use a TCP port binding or switch encryption to XCHACHA20_POLY1305")
}

// noiseInitiatorRole and noiseResponderRole re-export the role
// constants through the protocol package so callers do not need to
// import pkg/cipher/noise directly for a trivial enum.
func noiseInitiatorRole() noise.Role { return noise.RoleInitiator }
func noiseResponderRole() noise.Role { return noise.RoleResponder }
