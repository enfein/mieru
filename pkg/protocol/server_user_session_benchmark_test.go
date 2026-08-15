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
	"testing"

	"github.com/enfein/mieru/v3/pkg/cipher"
)

type udpSessionBench struct {
	underlay          *PacketUnderlay
	remoteAddr        net.Addr
	encryptedMetadata []byte
}

func newUDPSessionBench(b *testing.B, sessionCount int, sameSource bool) *udpSessionBench {
	b.Helper()
	remoteAddr := &net.UDPAddr{IP: net.IPv4(192, 168, 1, 1), Port: 20000}
	wrongCredential := cipher.HashPassword([]byte("wrong-password"), []byte("wrong-session-user"))
	wrongBlock, err := cipher.BlockCipherFromPassword(wrongCredential, true)
	if err != nil {
		b.Fatalf("BlockCipherFromPassword(wrong) failed: %v", err)
	}
	correctCredential := cipher.HashPassword([]byte("correct-password"), []byte("correct-user"))
	users := userMap(makeTestUser("correct-user", correctCredential))
	encryptedMetadata := encryptDiscoveryMetadata(b, correctCredential, "correct-user", users, true, newDummyMetadata())

	underlay := &PacketUnderlay{baseUnderlay: *newBaseUnderlay(false, 1400, nil)}
	for i := 0; i < sessionCount; i++ {
		sessionAddr := net.Addr(remoteAddr)
		if !sameSource {
			sessionAddr = &net.UDPAddr{
				IP:   net.IPv4(198, 51, byte(i>>8), byte(i)),
				Port: 30000 + i,
			}
		}
		session := &Session{remoteAddr: sessionAddr}
		block := wrongBlock
		session.block.Store(&block)
		underlay.sessionMap.Store(uint32(i+1), session)
	}
	return &udpSessionBench{underlay: underlay, remoteAddr: remoteAddr, encryptedMetadata: encryptedMetadata}
}

func BenchmarkUDPSessionLookup(b *testing.B) {
	for _, sessionCount := range []int{100, 10000} {
		b.Run(fmt.Sprintf("Sessions_%d", sessionCount), func(b *testing.B) {
			b.Run("AddressMiss", func(b *testing.B) {
				fixture := newUDPSessionBench(b, sessionCount, false)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, _, _, decrypted := fixture.underlay.tryDecryptExistingSession(fixture.encryptedMetadata, fixture.remoteAddr); decrypted {
						b.Fatal("unexpected existing-session match")
					}
				}
			})
			b.Run("SameAddressDecryptMiss", func(b *testing.B) {
				fixture := newUDPSessionBench(b, sessionCount, true)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, _, _, decrypted := fixture.underlay.tryDecryptExistingSession(fixture.encryptedMetadata, fixture.remoteAddr); decrypted {
						b.Fatal("unexpected existing-session match")
					}
				}
			})
		})
	}
}
