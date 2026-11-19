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
// along with this program.  If not, see <https://www.gnu.org/licenses/>

package protocol

import (
	"errors"
	"io"
	"net"

	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/log"
)

// auditServerAuth writes identity only after the complete first segment has
// been validated and dispatched. Rejections deliberately have no username:
// an encrypted, unauthenticated user hint is not proof of identity.
func auditServerAuth(username, reason string, local, remote net.Addr, sessionID uint32) {
	if !log.IsLevelEnabled(log.DebugLevel) || local == nil || remote == nil {
		return
	}
	if reason != "" {
		log.Debugf("TCP authentication failed from %s to %s (%s)", remote.String(), local.String(), reason)
		return
	}
	log.Debugf("TCP authentication succeeded for user %q from %s to %s, session %d", username, remote.String(), local.String(), sessionID)
}

// auditServerTraffic records the final byte counts after the socket is closed.
func (t *StreamUnderlay) auditServerTraffic() {
	username := t.serverUserPolicy.Name()
	if t.isClient || username == "" || t.serverSourceIP == "" || !log.IsLevelEnabled(log.DebugLevel) {
		return
	}
	conn, ok := t.conn.(*common.IdleTrackingConn)
	if !ok {
		return
	}
	// Let any in-flight write record its last bytes before taking the snapshot.
	t.sendMutex.Lock()
	upload, download := conn.ByteCounts()
	t.sendMutex.Unlock()
	log.Debugf("TCP connection closed for user %q from %s, uploaded %d bytes, downloaded %d bytes", username, t.serverSourceIP, upload, download)
}

// isEmptyHandshakeEOF distinguishes a bare TCP probe from an interrupted handshake.
func (t *StreamUnderlay) isEmptyHandshakeEOF(err error) bool {
	conn, ok := t.conn.(*common.IdleTrackingConn)
	if !ok || !errors.Is(err, io.EOF) {
		return false
	}
	read, _ := conn.ByteCounts()
	return read == 0
}
