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

package serveruser

import (
	"fmt"
	"testing"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
)

type userDiscoveryBench struct {
	users               []*appctlpb.User
	validHintCiphertext []byte
	fullScanCiphertext  []byte
}

func newUserDiscoveryBench(b *testing.B, userCount int) *userDiscoveryBench {
	b.Helper()
	fixture := &userDiscoveryBench{
		users: make([]*appctlpb.User, userCount),
	}
	credentials := make([][]byte, userCount)
	usersByName := make(map[string]*appctlpb.User, userCount)
	for i := 0; i < userCount; i++ {
		name := fmt.Sprintf("benchmark-user-%05d", i)
		credential := cipher.HashPassword([]byte(fmt.Sprintf("benchmark-password-%05d", i)), []byte(name))
		fixture.users[i] = makeTestUser(name, credential)
		credentials[i] = credential
		usersByName[name] = fixture.users[i]
	}

	plaintext := newDummyMetadata()
	target := userCount - 1
	targetName := fixture.users[target].GetName()
	fixture.validHintCiphertext = encryptDiscoveryMetadata(
		b, credentials[target], targetName, usersByName, true, plaintext,
	)
	fixture.fullScanCiphertext = encryptDiscoveryMetadata(
		b, credentials[target], "", usersByName, false, plaintext,
	)
	return fixture
}

func (f *userDiscoveryBench) state() *state {
	users := make(map[string]*appctlpb.User, len(f.users))
	for _, user := range f.users {
		users[user.GetName()] = user
	}
	return buildState(users, &sourceUserCacheStats{})
}

func BenchmarkDiscovery(b *testing.B) {
	const warmUserCount = 10000
	warmFixture := newUserDiscoveryBench(b, warmUserCount)
	warmState := warmFixture.state()
	warmTargetID := uint32(len(warmState.users))

	b.Run("Warm", func(b *testing.B) {
		for _, candidateCount := range []int{1, sourceUserCacheUsers} {
			b.Run(fmt.Sprintf("Candidates_%d", candidateCount), func(b *testing.B) {
				source := Source{key: sourceUserCacheTestKey(uint64(400 + candidateCount)), valid: true}
				for userID := uint32(1); userID < uint32(candidateCount); userID++ {
					warmState.cache.recordAuthenticated(source.key, userID)
				}
				warmState.cache.recordAuthenticated(source.key, warmTargetID)
				if result := tryState(warmState, warmFixture.validHintCiphertext, source, false); result.block == nil {
					b.Fatal("failed to warm server user discovery")
				}

				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					result := tryState(warmState, warmFixture.validHintCiphertext, source, false)
					if result.block == nil || result.origin != matchCachedHint || result.userID != warmTargetID {
						b.Fatalf("warm discovery = (block nil=%t, origin=%d, userID=%d), want cached hint for %d", result.block == nil, result.origin, result.userID, warmTargetID)
					}
				}
			})
		}
	})

	b.Run("RegistryScan", func(b *testing.B) {
		for _, userCount := range []int{100, 10000} {
			b.Run(fmt.Sprintf("Users_%d", userCount), func(b *testing.B) {
				fixture := newUserDiscoveryBench(b, userCount)
				state := fixture.state()
				benchmarks := []struct {
					name          string
					encryptedMeta []byte
				}{
					{name: "ValidHint", encryptedMeta: fixture.validHintCiphertext},
					{name: "FullScan", encryptedMeta: fixture.fullScanCiphertext},
				}
				for _, benchmark := range benchmarks {
					b.Run(benchmark.name, func(b *testing.B) {
						// Populate the prepared decryptors while leaving source lookup disabled.
						if result := tryState(state, benchmark.encryptedMeta, Source{}, false); result.block == nil {
							b.Fatal("failed to warm cipher trial material")
						}

						b.ReportAllocs()
						b.ResetTimer()
						for i := 0; i < b.N; i++ {
							result := tryState(state, benchmark.encryptedMeta, Source{}, false)
							if result.block == nil || result.userID != uint32(len(state.users)) {
								b.Fatalf("registry scan = (block nil=%t, userID=%d), want user %d", result.block == nil, result.userID, len(state.users))
							}
						}
					})
				}
			})
		}
	})

	b.Run("WarmParallel", func(b *testing.B) {
		b.Run("Candidates_1", func(b *testing.B) {
			source := Source{key: sourceUserCacheTestKey(500), valid: true}
			warmState.cache.recordAuthenticated(source.key, warmTargetID)
			if result := tryState(warmState, warmFixture.validHintCiphertext, source, false); result.block == nil {
				b.Fatal("failed to warm server user discovery")
			}

			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					result := tryState(warmState, warmFixture.validHintCiphertext, source, false)
					if result.block == nil || result.origin != matchCachedHint || result.userID != warmTargetID {
						b.Errorf("parallel warm discovery = (block nil=%t, origin=%d, userID=%d), want cached hint for %d", result.block == nil, result.origin, result.userID, warmTargetID)
					}
				}
			})
		})
	})
}
