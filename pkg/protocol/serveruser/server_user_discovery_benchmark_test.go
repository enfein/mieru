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
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
)

var (
	benchUser      string
	benchPlaintext []byte
)

// userDiscoveryBench is used to benchmark finding the correct user with
// or without user hint.
type userDiscoveryBench struct {
	users                      []*appctlpb.User
	credentials                [][]byte
	validHintCiphertext        []byte
	noUsableHintCiphertext     []byte // can not decrypt
	fullTraversalEndCiphertext []byte // no hint
}

func newUserDiscoveryBench(b *testing.B, userCount int) *userDiscoveryBench {
	b.Helper()
	fixture := &userDiscoveryBench{
		users:       make([]*appctlpb.User, userCount),
		credentials: make([][]byte, userCount),
	}
	usersByName := make(map[string]*appctlpb.User, userCount)
	for i := 0; i < userCount; i++ {
		name := fmt.Sprintf("baseline-user-%05d", i)
		credential := cipher.HashPassword([]byte(fmt.Sprintf("baseline-password-%05d", i)), []byte(name))
		fixture.users[i] = makeTestUser(name, credential)
		fixture.credentials[i] = credential
		usersByName[name] = fixture.users[i]
	}

	plaintext := newDummyMetadata()
	target := userCount - 1
	targetName := fixture.users[target].GetName()
	fixture.validHintCiphertext = encryptDiscoveryMetadata(
		b, fixture.credentials[target], targetName, usersByName, true, plaintext,
	)
	fixture.fullTraversalEndCiphertext = encryptDiscoveryMetadata(
		b, fixture.credentials[target], "", usersByName, false, plaintext,
	)
	unknownCredential := cipher.HashPassword([]byte("unknown-baseline-password"), []byte(fmt.Sprintf("users-%d", userCount)))
	fixture.noUsableHintCiphertext = encryptDiscoveryMetadata(
		b, unknownCredential, "", usersByName, false, plaintext,
	)
	fixture.warmCipherCache(b)
	return fixture
}

func (f *userDiscoveryBench) warmCipherCache(b *testing.B) {
	b.Helper()
	for _, credential := range f.credentials {
		if _, err := cipher.BlockCipherListFromPassword(credential, true); err != nil {
			b.Fatalf("BlockCipherListFromPassword() failed: %v", err)
		}
	}
}

// baselineUserDiscovery mirrors the baseline TCP and UDP user discovery logic,
// but accepts an ordered slice instead of a map to have deterministic results.
func baselineUserDiscovery(users []*appctlpb.User, encryptedMeta []byte, hintMandatory bool) ([]byte, string, error) {
	nonce := encryptedMeta[:cipher.DefaultNonceSize]
	var hintUsers []*appctlpb.User
	for _, user := range users {
		if cipher.CheckUserFromHint([]byte(user.GetName()), nonce) {
			hintUsers = append(hintUsers, user)
		}
	}
	for _, user := range hintUsers {
		credential, err := hex.DecodeString(user.GetHashedPassword())
		if err != nil {
			continue
		}
		if len(credential) == 0 {
			credential = cipher.HashPassword([]byte(user.GetPassword()), []byte(user.GetName()))
		}
		block, plaintext, err := cipher.TryDecrypt(encryptedMeta, credential, true)
		if err == nil {
			block.SetBlockContext(cipher.BlockContext{UserName: user.GetName()})
			return plaintext, user.GetName(), nil
		}
	}
	if !hintMandatory {
		for _, user := range users {
			credential, err := hex.DecodeString(user.GetHashedPassword())
			if err != nil {
				continue
			}
			if len(credential) == 0 {
				credential = cipher.HashPassword([]byte(user.GetPassword()), []byte(user.GetName()))
			}
			block, plaintext, err := cipher.TryDecrypt(encryptedMeta, credential, true)
			if err == nil {
				block.SetBlockContext(cipher.BlockContext{UserName: user.GetName()})
				return plaintext, user.GetName(), nil
			}
		}
	}
	return nil, "", fmt.Errorf("cipher.TryDecrypt() failed for all users")
}

func BenchmarkUserDiscovery(b *testing.B) {
	for _, userCount := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("Users_%d", userCount), func(b *testing.B) {
			fixture := newUserDiscoveryBench(b, userCount)
			benchmarks := []struct {
				name          string
				encryptedMeta []byte
				wantSuccess   bool
			}{
				{name: "ValidHint", encryptedMeta: fixture.validHintCiphertext, wantSuccess: true},
				{name: "NoUsableHint", encryptedMeta: fixture.noUsableHintCiphertext, wantSuccess: false},
				{name: "FullTraversalEnd", encryptedMeta: fixture.fullTraversalEndCiphertext, wantSuccess: true},
			}
			for _, benchmark := range benchmarks {
				b.Run(benchmark.name, func(b *testing.B) {
					fixture.warmCipherCache(b)
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						plaintext, user, err := baselineUserDiscovery(fixture.users, benchmark.encryptedMeta, false)
						if (err == nil) != benchmark.wantSuccess {
							b.Fatalf("discovery error = %v, want success %t", err, benchmark.wantSuccess)
						}
						benchPlaintext = plaintext
						benchUser = user
					}
				})
			}
		})
	}
}

func BenchmarkUserDiscoveryParallel10K(b *testing.B) {
	fixture := newUserDiscoveryBench(b, 10000)
	b.Run("FullTraversalEnd", func(b *testing.B) {
		fixture.warmCipherCache(b)
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				plaintext, user, err := baselineUserDiscovery(fixture.users, fixture.fullTraversalEndCiphertext, false)
				if err != nil || len(plaintext) == 0 || user == "" {
					b.Errorf("parallel discovery = (%d bytes, %q, %v)", len(plaintext), user, err)
				}
			}
		})
	})
	b.Run("UserHintLookup", func(b *testing.B) {
		nonce := fixture.validHintCiphertext[:cipher.DefaultNonceSize]
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				matches := 0
				for _, user := range fixture.users {
					if cipher.CheckUserFromHint([]byte(user.GetName()), nonce) {
						matches++
					}
				}
				if matches != 1 {
					b.Errorf("hint matches = %d, want 1", matches)
				}
			}
		})
	})
	b.Run("CipherCacheLookup", func(b *testing.B) {
		credential := fixture.credentials[len(fixture.credentials)-1]
		fixture.warmCipherCache(b)
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				blocks, err := cipher.BlockCipherListFromPassword(credential, true)
				if err != nil || len(blocks) == 0 {
					b.Errorf("cipher cache lookup = (%d blocks, %v)", len(blocks), err)
				}
			}
		})
	})
	b.Run("StatelessDecrypt", func(b *testing.B) {
		wrongBlock, err := cipher.BlockCipherFromPassword(fixture.credentials[0], true)
		if err != nil {
			b.Fatalf("BlockCipherFromPassword() failed: %v", err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if _, err := wrongBlock.Decrypt(fixture.fullTraversalEndCiphertext); err == nil {
					b.Error("wrong cipher decrypted metadata")
				}
			}
		})
	})
}

func BenchmarkServerUserDiscoveryWarmCache(b *testing.B) {
	for _, userCount := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("Users_%d", userCount), func(b *testing.B) {
			fixture := newUserDiscoveryBench(b, userCount)
			users := make(map[string]*appctlpb.User, len(fixture.users))
			for _, user := range fixture.users {
				users[user.GetName()] = user
			}
			state := buildServerUserState(users, &sourceUserCacheStats{})
			targetID := uint32(len(state.users))
			for _, candidateCount := range []int{1, sourceUserCacheUsers} {
				b.Run(fmt.Sprintf("Candidates_%d", candidateCount), func(b *testing.B) {
					source := serverUserDiscoverySource{key: sourceUserCacheTestKey(uint64(400 + candidateCount)), valid: true}
					for userID := uint32(1); userID < uint32(candidateCount); userID++ {
						state.cache.recordAuthenticated(source.key, userID)
					}
					state.cache.recordAuthenticated(source.key, targetID)

					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						result := tryServerUserState(state, fixture.validHintCiphertext, source, false)
						if result.block == nil || result.origin != serverUserMatchCachedHint || result.userID != targetID {
							b.Fatalf("warm discovery = (block nil=%t, origin=%d, userID=%d), want cached hint for %d", result.block == nil, result.origin, result.userID, targetID)
						}
					}
				})
			}
		})
	}
}

func BenchmarkServerUserDiscoveryColdSourceCache(b *testing.B) {
	for _, userCount := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("Users_%d", userCount), func(b *testing.B) {
			fixture := newUserDiscoveryBench(b, userCount)
			users := make(map[string]*appctlpb.User, len(fixture.users))
			for _, user := range fixture.users {
				users[user.GetName()] = user
			}
			state := buildServerUserState(users, &sourceUserCacheStats{})
			benchmarks := []struct {
				name          string
				encryptedMeta []byte
			}{
				{name: "ValidHint", encryptedMeta: fixture.validHintCiphertext},
				{name: "FullTraversalEnd", encryptedMeta: fixture.fullTraversalEndCiphertext},
			}
			for _, benchmark := range benchmarks {
				b.Run(benchmark.name, func(b *testing.B) {
					// Populate each prepared decryptor lazily without recording a
					// source association. The timed path therefore measures a source
					// cache miss with current key-epoch material already available.
					if result := tryServerUserState(state, benchmark.encryptedMeta, serverUserDiscoverySource{}, false); result.block == nil {
						b.Fatal("failed to warm cipher trial material")
					}

					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						result := tryServerUserState(state, benchmark.encryptedMeta, serverUserDiscoverySource{}, false)
						if result.block == nil || result.userID != uint32(len(state.users)) {
							b.Fatalf("cold source discovery = (block nil=%t, userID=%d), want user %d", result.block == nil, result.userID, len(state.users))
						}
					}
				})
			}
		})
	}
}

func BenchmarkServerUserDiscoveryWarmCacheParallel10K(b *testing.B) {
	fixture := newUserDiscoveryBench(b, 10000)
	users := make(map[string]*appctlpb.User, len(fixture.users))
	for _, user := range fixture.users {
		users[user.GetName()] = user
	}
	state := buildServerUserState(users, &sourceUserCacheStats{})
	targetID := uint32(len(state.users))
	source := serverUserDiscoverySource{key: sourceUserCacheTestKey(500), valid: true}
	state.cache.recordAuthenticated(source.key, targetID)
	if result := tryServerUserState(state, fixture.validHintCiphertext, source, false); result.block == nil {
		b.Fatal("failed to warm server user discovery")
	}

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			result := tryServerUserState(state, fixture.validHintCiphertext, source, false)
			if result.block == nil || result.origin != serverUserMatchCachedHint || result.userID != targetID {
				b.Errorf("parallel warm discovery = (block nil=%t, origin=%d, userID=%d), want cached hint for %d", result.block == nil, result.origin, result.userID, targetID)
			}
		}
	})
}
