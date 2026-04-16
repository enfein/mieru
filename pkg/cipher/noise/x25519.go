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

package noise

import "golang.org/x/crypto/curve25519"

// x25519BasepointDefault computes priv * basepoint on Curve25519.
//
// It is the real implementation behind the x25519Basepoint variable in
// proto.go, split out so tests can swap in a stub without pulling in
// curve25519.
func x25519BasepointDefault(priv []byte) []byte {
	pub, err := curve25519.X25519(priv, curve25519.Basepoint)
	if err != nil {
		// curve25519.X25519 can only fail when priv is the wrong
		// length; the caller already checks for 32 bytes.
		return nil
	}
	return pub
}
