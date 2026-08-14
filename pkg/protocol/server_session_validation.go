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

import "fmt"

// validateNewServerSessionSegment enforces the only protocol that may create a
// server session. Callers run this after payload authentication and padding
// validation, immediately before initial protocol dispatch.
func validateNewServerSessionSegment(seg *segment) error {
	if seg == nil || seg.metadata == nil {
		return fmt.Errorf("new server session segment is nil")
	}
	ss, ok := seg.metadata.(*sessionStruct)
	if !ok || ss.Protocol() != openSessionRequest {
		return fmt.Errorf("protocol %v can't create a server session", seg.metadata.Protocol())
	}
	if ss.sessionID == 0 {
		return fmt.Errorf("reserved session ID %d is used", ss.sessionID)
	}
	return nil
}

// validateServerSegmentDirection rejects protocols that can only be sent by a
// server. Unknown-session UDP data and close messages in the client-to-server
// direction remain dispatchable so the existing close-session behavior is
// preserved, but they never create a source-user association.
func validateServerSegmentDirection(seg *segment) error {
	if seg == nil || seg.metadata == nil {
		return fmt.Errorf("server segment is nil")
	}
	switch seg.metadata.Protocol() {
	case openSessionRequest,
		closeSessionRequest,
		closeSessionResponse,
		dataClientToServer,
		dataClientToServerLowEntropy,
		ackClientToServer:
		return nil
	default:
		return fmt.Errorf("protocol %v has the wrong direction for a server", seg.metadata.Protocol())
	}
}
