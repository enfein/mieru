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
	"encoding/hex"
	"fmt"
	"net"
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

// udpSessionBench is used to benchmark finding the correct session in
// a single server PacketUnderlay.
type udpSessionBench struct {
	underlay          *PacketUnderlay
	remoteAddr        net.Addr
	encryptedMetadata []byte
}

// newUDPSessionBench creates multiple sessions in the same PacketUnderlay.
// The recommended max sessionCount is 10000.
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
	return &udpSessionBench{
		underlay:          underlay,
		remoteAddr:        remoteAddr,
		encryptedMetadata: encryptedMetadata,
	}
}

func baselineUDPSessionLookup(underlay *PacketUnderlay, remoteAddr net.Addr, encryptedMeta []byte) bool {
	decrypted := false
	underlay.sessionMap.Range(func(_, value any) bool {
		session := value.(*Session)
		if session.block.Load() != nil && session.RemoteAddr().String() == remoteAddr.String() {
			if _, err := (*session.block.Load()).Decrypt(encryptedMeta); err == nil {
				decrypted = true
				return false
			}
		}
		return true
	})
	return decrypted
}

func BenchmarkUDPSessionLookup(b *testing.B) {
	for _, sessionCount := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("Sessions_%d", sessionCount), func(b *testing.B) {
			b.Run("AddressMiss", func(b *testing.B) {
				fixture := newUDPSessionBench(b, sessionCount, false)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if baselineUDPSessionLookup(fixture.underlay, fixture.remoteAddr, fixture.encryptedMetadata) {
						b.Fatal("unexpected existing-session match")
					}
				}
			})
			b.Run("SameAddressDecryptMiss", func(b *testing.B) {
				fixture := newUDPSessionBench(b, sessionCount, true)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if baselineUDPSessionLookup(fixture.underlay, fixture.remoteAddr, fixture.encryptedMetadata) {
						b.Fatal("unexpected existing-session match")
					}
				}
			})
		})
	}
}
