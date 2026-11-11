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
	"encoding/json"
	"net"
	"os"
	"time"

	"github.com/enfein/mieru/v3/pkg/log"
)

// auditServerAuth writes identity only after the complete first segment has
// been validated and dispatched. Rejections deliberately have no username:
// an encrypted, unauthenticated user hint is not proof of identity.
func auditServerAuth(event, username, reason string, local, remote net.Addr, sessionID uint32) {
	if os.Getenv("MITA_AUTH_AUDIT") != "1" || !log.IsLevelEnabled(log.InfoLevel) {
		return
	}
	if local == nil || remote == nil {
		return
	}
	sourceIP, sourcePort, err := net.SplitHostPort(remote.String())
	if err != nil {
		return
	}
	if ip := net.ParseIP(sourceIP); ip != nil {
		sourceIP = ip.String()
	}
	localIP, localPort, err := net.SplitHostPort(local.String())
	if err != nil {
		return
	}
	fields := map[string]interface{}{
		"event": event, "time_utc": time.Now().UTC().Format(time.RFC3339Nano),
		"protocol": "TCP", "source_ip": sourceIP, "source_port": sourcePort,
		"local_ip": localIP, "local_port": localPort,
	}
	if username != "" {
		fields["username"] = username
	}
	if sessionID != 0 {
		fields["session_id"] = sessionID
	}
	if reason != "" {
		fields["reason"] = reason
	}
	encoded, err := json.Marshal(fields)
	if err == nil {
		log.Infof("AUTH_AUDIT %s", encoded)
	}
}
