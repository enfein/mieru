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
	"bytes"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
	"google.golang.org/protobuf/proto"
)

type discoveryTransport struct {
	name     string
	discover func(map[string]*appctlpb.User, bool, []byte) ([]byte, string, error)
}

var discoveryTransports = []discoveryTransport{
	{
		name: "CurrentGeneration",
		discover: func(users map[string]*appctlpb.User, hintMandatory bool, encryptedMeta []byte) ([]byte, string, error) {
			registry := &Registry{}
			registry.SetUsers(users)
			registry.SetHintMandatory(hintMandatory)
			block, decryptedMeta, _, err := registry.Discover(encryptedMeta, Source{}, true)
			if err != nil {
				return nil, "", err
			}
			return decryptedMeta, block.BlockContext().UserName, nil
		},
	},
	{
		name: "SnapshotGeneration",
		discover: func(users map[string]*appctlpb.User, hintMandatory bool, encryptedMeta []byte) ([]byte, string, error) {
			registry := &Registry{}
			registry.SetUsers(users)
			registry.SetHintMandatory(hintMandatory)
			block, decryptedMeta, _, err := registry.Discover(encryptedMeta, Source{}, false)
			if err != nil {
				return nil, "", err
			}
			return decryptedMeta, block.BlockContext().UserName, nil
		},
	},
}

func testServerUserPublisher(users map[string]*appctlpb.User, hintMandatory bool) (*atomic.Pointer[serverUserState], *atomic.Bool) {
	publisher := &atomic.Pointer[serverUserState]{}
	publisher.Store(buildServerUserState(users, &sourceUserCacheStats{}))
	mandatory := &atomic.Bool{}
	mandatory.Store(hintMandatory)
	return publisher, mandatory
}

// TestUserDiscoveryRequiresAuthentication records the authentication invariant:
// a nonce hint proposes an identity (user name), but it cannot select that identity
// unless the associated credential can decrypt the metadata.
func TestUserDiscoveryRequiresAuthentication(t *testing.T) {
	const userName = "hinted-user"
	registeredCredential := cipher.HashPassword([]byte(t.Name()), []byte(userName))
	users := userMap(makeTestUser(userName, registeredCredential))
	unknownCredential := cipher.HashPassword([]byte("not-the-registered-password"), []byte(userName))
	plaintext := newDummyMetadata()
	encryptedMetadataWithHint := encryptDiscoveryMetadata(t, unknownCredential, userName, users, true, plaintext)

	for _, transport := range discoveryTransports {
		for _, hintMandatory := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/MandatoryHint_%t", transport.name, hintMandatory), func(t *testing.T) {
				decryptedMetadata, matchedUser, err := transport.discover(users, hintMandatory, encryptedMetadataWithHint)
				if err == nil {
					t.Fatalf("discovery succeeded as user %q without authenticated decryption", matchedUser)
				}
				if decryptedMetadata != nil || matchedUser != "" {
					t.Fatalf("discovery returned unexpected result (metadata=%x, user=%q)", decryptedMetadata, matchedUser)
				}
			})
		}
	}
}

// TestUserDiscoveryMandatoryHint records that a mandatory hint cannot be
// bypassed. With optional hints, the same authenticated message must fall
// back safely. Hash collision are optimization misses and must preserve
// this full fallback.
func TestUserDiscoveryMandatoryHint(t *testing.T) {
	const userName = "fallback-user"
	credential := cipher.HashPassword([]byte(t.Name()), []byte(userName))
	users := userMap(makeTestUser(userName, credential))
	plaintext := newDummyMetadata()
	encryptedMetadataNoHint := encryptDiscoveryMetadata(t, credential, "", users, false, plaintext)

	for _, transport := range discoveryTransports {
		t.Run(transport.name+"/OptionalHintFallsBack", func(t *testing.T) {
			decryptedMetadata, matchedUser, err := transport.discover(users, false, encryptedMetadataNoHint)
			if err != nil {
				t.Fatalf("optional-hint discovery failed: %v", err)
			}
			if !bytes.Equal(decryptedMetadata, plaintext) || matchedUser != userName {
				t.Fatalf("discovery = (%x, %q), want (%x, %q)", decryptedMetadata, matchedUser, plaintext, userName)
			}
		})
		t.Run(transport.name+"/MandatoryHintRejectsFallback", func(t *testing.T) {
			decryptedMetadata, matchedUser, err := transport.discover(users, true, encryptedMetadataNoHint)
			if err == nil {
				t.Fatalf("mandatory-hint discovery selected user %q without a usable hint", matchedUser)
			}
			if decryptedMetadata != nil || matchedUser != "" {
				t.Fatalf("discovery returned unexpected result (metadata=%x, user=%q)", decryptedMetadata, matchedUser)
			}
		})
	}
}

// TestUserDiscoveryHintPrecedence records selected identity semantics
// when credentials are shared. Even if a nonmatching user somehow has
// the same credential as the hinted user, the hinted user must
// retain precedence.
func TestUserDiscoveryHintPrecedence(t *testing.T) {
	const (
		hintedUser      = "hinted-user"
		nonmatchingUser = "cached-nonmatching-user"
	)
	sharedCredential := cipher.HashPassword([]byte(t.Name()), []byte("shared-credential"))
	users := userMap(
		makeTestUser(nonmatchingUser, sharedCredential),
		makeTestUser(hintedUser, sharedCredential),
	)
	plaintext := newDummyMetadata()
	encryptedMetadata := encryptDiscoveryMetadata(t, sharedCredential, hintedUser, users, true, plaintext)

	for _, transport := range discoveryTransports {
		for _, hintMandatory := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/MandatoryHint_%t", transport.name, hintMandatory), func(t *testing.T) {
				decryptedMetadata, matchedUser, err := transport.discover(users, hintMandatory, encryptedMetadata)
				if err != nil {
					t.Fatalf("discovery failed: %v", err)
				}
				if !bytes.Equal(decryptedMetadata, plaintext) || matchedUser != hintedUser {
					t.Fatalf("discovery = (%x, %q), want (%x, %q)", decryptedMetadata, matchedUser, plaintext, hintedUser)
				}
			})
		}
	}
}

func TestServerUserDiscoveryWarmCachedHintMatch(t *testing.T) {
	const target = "warm-target"
	credential := cipher.HashPassword([]byte(t.Name()), []byte(target))
	users := userMap(
		makeTestUser("registry-user-a", cipher.HashPassword([]byte("a"), []byte("registry-user-a"))),
		makeTestUser("registry-user-b", cipher.HashPassword([]byte("b"), []byte("registry-user-b"))),
		makeTestUser(target, credential),
	)
	publisher, mandatory := testServerUserPublisher(users, false)
	state := publisher.Load()
	source := serverUserDiscoverySource{key: sourceUserCacheTestKey(301), valid: true}
	state.cache.recordAuthenticated(source.key, testServerUserID(t, state, target))
	encrypted := encryptDiscoveryMetadata(t, credential, target, users, true, newDummyMetadata())

	result, err := discoverServerUser(publisher, mandatory, encrypted, source, false, nil)
	if err != nil {
		t.Fatalf("discoverServerUser() failed: %v", err)
	}
	assertServerUserDiscovery(t, result, target, serverUserMatchCachedHint, 1)
}

func TestServerUserDiscoveryOptionalCachedFallback(t *testing.T) {
	const target = "cached-fallback"
	credential := cipher.HashPassword([]byte(t.Name()), []byte(target))
	users := userMap(makeTestUser(target, credential))
	publisher, mandatory := testServerUserPublisher(users, false)
	state := publisher.Load()
	source := serverUserDiscoverySource{key: sourceUserCacheTestKey(302), valid: true}
	state.cache.recordAuthenticated(source.key, testServerUserID(t, state, target))
	encrypted := encryptDiscoveryMetadata(t, credential, "", users, false, newDummyMetadata())

	result, err := discoverServerUser(publisher, mandatory, encrypted, source, false, nil)
	if err != nil {
		t.Fatalf("discoverServerUser() failed: %v", err)
	}
	assertServerUserDiscovery(t, result, target, serverUserMatchCachedFallback, 1)
}

func TestServerUserDiscoveryRegistryHintPrecedesCachedSharedCredential(t *testing.T) {
	const (
		cached = "cached-nonmatching"
		hinted = "hinted-registry-user"
	)
	credential := cipher.HashPassword([]byte(t.Name()), []byte("shared"))
	users := userMap(makeTestUser(cached, credential), makeTestUser(hinted, credential))
	publisher, mandatory := testServerUserPublisher(users, false)
	state := publisher.Load()
	source := serverUserDiscoverySource{key: sourceUserCacheTestKey(303), valid: true}
	state.cache.recordAuthenticated(source.key, testServerUserID(t, state, cached))
	encrypted := encryptDiscoveryMetadata(t, credential, hinted, users, true, newDummyMetadata())

	result, err := discoverServerUser(publisher, mandatory, encrypted, source, false, nil)
	if err != nil {
		t.Fatalf("discoverServerUser() failed: %v", err)
	}
	assertServerUserDiscovery(t, result, hinted, serverUserMatchRegistryHint, 1)
}

func TestServerUserDiscoveryMandatoryHintRejectsCachedFallback(t *testing.T) {
	const target = "cached-but-not-hinted"
	credential := cipher.HashPassword([]byte(t.Name()), []byte(target))
	users := userMap(makeTestUser(target, credential))
	publisher, _ := testServerUserPublisher(users, true)
	state := publisher.Load()
	source := serverUserDiscoverySource{key: sourceUserCacheTestKey(304), valid: true}
	state.cache.recordAuthenticated(source.key, testServerUserID(t, state, target))
	encrypted := encryptDiscoveryMetadata(t, credential, "unregistered-hint", users, false, newDummyMetadata())

	result := tryServerUserState(state, encrypted, source, true)
	if result.block != nil || result.attempts != 0 {
		t.Fatalf("mandatory discovery = (block nil=%t, attempts=%d), want rejection without cached fallback", result.block == nil, result.attempts)
	}
}

func TestServerUserDiscoveryCacheMissPreservesRegistryFallback(t *testing.T) {
	const target = "z-registry-fallback"
	credential := cipher.HashPassword([]byte(t.Name()), []byte(target))
	users := userMap(
		makeTestUser("a-wrong-user", cipher.HashPassword([]byte("wrong"), []byte("a-wrong-user"))),
		makeTestUser(target, credential),
	)
	publisher, mandatory := testServerUserPublisher(users, false)
	source := serverUserDiscoverySource{key: sourceUserCacheTestKey(305), valid: true}
	encrypted := encryptDiscoveryMetadata(t, credential, "", users, false, newDummyMetadata())

	result, err := discoverServerUser(publisher, mandatory, encrypted, source, false, nil)
	if err != nil {
		t.Fatalf("discoverServerUser() failed: %v", err)
	}
	assertServerUserDiscovery(t, result, target, serverUserMatchRegistryFallback, 2)
}

func TestServerUserDiscoveryMultipleCachedUsers(t *testing.T) {
	const (
		first  = "first-cached-user"
		target = "second-cached-user"
	)
	firstCredential := cipher.HashPassword([]byte("first"), []byte(first))
	targetCredential := cipher.HashPassword([]byte(t.Name()), []byte(target))
	users := userMap(makeTestUser(first, firstCredential), makeTestUser(target, targetCredential))
	publisher, mandatory := testServerUserPublisher(users, false)
	state := publisher.Load()
	source := serverUserDiscoverySource{key: sourceUserCacheTestKey(306), valid: true}
	state.cache.recordAuthenticated(source.key, testServerUserID(t, state, first))
	state.cache.recordAuthenticated(source.key, testServerUserID(t, state, target))
	encrypted := encryptDiscoveryMetadata(t, targetCredential, "", users, false, newDummyMetadata())

	result, err := discoverServerUser(publisher, mandatory, encrypted, source, false, nil)
	if err != nil {
		t.Fatalf("discoverServerUser() failed: %v", err)
	}
	assertServerUserDiscovery(t, result, target, serverUserMatchCachedFallback, 2)
}

func TestServerUserDiscoverySkipsInvalidCachedID(t *testing.T) {
	const target = "valid-cached-user"
	credential := cipher.HashPassword([]byte(t.Name()), []byte(target))
	users := userMap(makeTestUser(target, credential))
	publisher, mandatory := testServerUserPublisher(users, false)
	state := publisher.Load()
	source := serverUserDiscoverySource{key: sourceUserCacheTestKey(307), valid: true}
	state.cache.recordAuthenticated(source.key, uint32(len(state.users)+100))
	state.cache.recordAuthenticated(source.key, testServerUserID(t, state, target))
	encrypted := encryptDiscoveryMetadata(t, credential, "", users, false, newDummyMetadata())

	result, err := discoverServerUser(publisher, mandatory, encrypted, source, false, nil)
	if err != nil {
		t.Fatalf("discoverServerUser() failed: %v", err)
	}
	assertServerUserDiscovery(t, result, target, serverUserMatchCachedFallback, 1)
}

func TestServerUserDiscoveryDoesNotRetryCachedHintFailure(t *testing.T) {
	const (
		cachedWrong = "a-cached-wrong"
		target      = "z-correct-user"
	)
	wrongCredential := cipher.HashPassword([]byte("wrong"), []byte(cachedWrong))
	targetCredential := cipher.HashPassword([]byte(t.Name()), []byte(target))
	users := userMap(makeTestUser(cachedWrong, wrongCredential), makeTestUser(target, targetCredential))
	publisher, mandatory := testServerUserPublisher(users, false)
	state := publisher.Load()
	source := serverUserDiscoverySource{key: sourceUserCacheTestKey(308), valid: true}
	state.cache.recordAuthenticated(source.key, testServerUserID(t, state, cachedWrong))
	encrypted := encryptDiscoveryMetadata(t, targetCredential, cachedWrong, users, true, newDummyMetadata())
	hintBefore := cipher.ServerHintMatchDecrypt.Load()
	failedHintBefore := cipher.ServerFailedHintMatchDecrypt.Load()

	result, err := discoverServerUser(publisher, mandatory, encrypted, source, false, nil)
	if err != nil {
		t.Fatalf("discoverServerUser() failed: %v", err)
	}
	assertServerUserDiscovery(t, result, target, serverUserMatchRegistryFallback, 2)
	if got := cipher.ServerHintMatchDecrypt.Load() - hintBefore; got != 1 {
		t.Fatalf("hint-match metric delta = %d, want 1", got)
	}
	if got := cipher.ServerFailedHintMatchDecrypt.Load() - failedHintBefore; got != 1 {
		t.Fatalf("failed-hint metric delta = %d, want 1", got)
	}
}

func TestServerUserDiscoveryCacheCounterClassification(t *testing.T) {
	const (
		cachedUser   = "cached-counter-user"
		registryUser = "registry-counter-user"
	)
	cachedCredential := cipher.HashPassword([]byte(t.Name()+"-cached"), []byte(cachedUser))
	registryCredential := cipher.HashPassword([]byte(t.Name()+"-registry"), []byte(registryUser))
	users := userMap(
		makeTestUser(cachedUser, cachedCredential),
		makeTestUser(registryUser, registryCredential),
	)
	publisher, mandatory := testServerUserPublisher(users, false)
	state := publisher.Load()
	source := serverUserDiscoverySource{key: sourceUserCacheTestKey(309), valid: true}

	// A cold query is a source miss and enters a full-registry phase.
	cold := encryptDiscoveryMetadata(t, cachedCredential, cachedUser, users, true, newDummyMetadata())
	if _, err := discoverServerUser(publisher, mandatory, cold, source, false, nil); err != nil {
		t.Fatalf("cold discoverServerUser() failed: %v", err)
	}
	state.cache.recordAuthenticated(source.key, testServerUserID(t, state, cachedUser))

	// A cached hint winner is both a source and authentication hit, and it
	// returns before entering a full-registry phase.
	if _, err := discoverServerUser(publisher, mandatory, cold, source, false, nil); err != nil {
		t.Fatalf("warm hinted discoverServerUser() failed: %v", err)
	}

	// With no hint, discovery scans the registry hint phase before the cached
	// optional fallback wins. Authentication hit and full fallback therefore
	// intentionally overlap.
	noHint := encryptDiscoveryMetadata(t, cachedCredential, "", users, false, newDummyMetadata())
	if _, err := discoverServerUser(publisher, mandatory, noHint, source, false, nil); err != nil {
		t.Fatalf("warm no-hint discoverServerUser() failed: %v", err)
	}

	// An exact source hit whose cached user does not win is not an
	// authentication hit.
	registry := encryptDiscoveryMetadata(t, registryCredential, registryUser, users, true, newDummyMetadata())
	if _, err := discoverServerUser(publisher, mandatory, registry, source, false, nil); err != nil {
		t.Fatalf("registry discoverServerUser() failed: %v", err)
	}

	want := sourceUserCacheStatsSnapshot{
		lookups:            4,
		sourceHits:         3,
		sourceMisses:       1,
		authenticationHits: 2,
		fullFallbacks:      3,
		insertions:         1,
	}
	if got := state.cache.stats.load(); got != want {
		t.Fatalf("discovery stats = %+v, want %+v", got, want)
	}
}

func TestServerUserDiscoverySharedCredentialContextsAreIndependent(t *testing.T) {
	const (
		userA = "shared-user-a"
		userB = "shared-user-b"
	)
	credential := cipher.HashPassword([]byte(t.Name()), []byte("shared"))
	users := userMap(makeTestUser(userA, credential), makeTestUser(userB, credential))
	publisher, mandatory := testServerUserPublisher(users, true)
	encrypted := map[string][]byte{
		userA: encryptDiscoveryMetadata(t, credential, userA, users, true, newDummyMetadata()),
		userB: encryptDiscoveryMetadata(t, credential, userB, users, true, newDummyMetadata()),
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 32)
	for i := 0; i < 32; i++ {
		want := userA
		if i%2 != 0 {
			want = userB
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := discoverServerUser(publisher, mandatory, encrypted[want], serverUserDiscoverySource{}, false, nil)
			if err != nil {
				errCh <- err
				return
			}
			result.block.SetBlockContext(result.userContext)
			if got := result.block.BlockContext().UserName; got != want {
				errCh <- fmt.Errorf("matched context = %q, want %q", got, want)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}

func assertServerUserDiscovery(t *testing.T, result serverUserDiscoveryResult, wantUser string, wantOrigin serverUserMatchOrigin, wantAttempts int) {
	t.Helper()
	if result.block == nil || !bytes.Equal(result.decryptedMetadata, newDummyMetadata()) {
		t.Fatalf("discovery returned block nil=%t, metadata=%x", result.block == nil, result.decryptedMetadata)
	}
	if result.userID == 0 || result.userContext.UserName != wantUser || result.policy.name != wantUser {
		t.Fatalf("discovery identity = (ID %d, context %q, policy %q), want %q", result.userID, result.userContext.UserName, result.policy.name, wantUser)
	}
	if result.origin != wantOrigin || result.attempts != wantAttempts {
		t.Fatalf("discovery path = (origin %d, attempts %d), want (%d, %d)", result.origin, result.attempts, wantOrigin, wantAttempts)
	}
	if result.generation == nil {
		t.Fatal("discovery did not return its user generation")
	}
}

func testServerUserID(t *testing.T, state *serverUserState, name string) uint32 {
	t.Helper()
	for i := range state.users {
		if state.users[i].name == name {
			return state.users[i].id
		}
	}
	t.Fatalf("compiled user %q not found", name)
	return 0
}

func makeTestUser(name string, credential []byte) *appctlpb.User {
	return &appctlpb.User{
		Name:           proto.String(name),
		HashedPassword: proto.String(hex.EncodeToString(credential)),
	}
}

func userMap(users ...*appctlpb.User) map[string]*appctlpb.User {
	result := make(map[string]*appctlpb.User, len(users))
	for _, user := range users {
		result[user.GetName()] = user
	}
	return result
}

func newDummyMetadata() []byte {
	metadata := make([]byte, metadataLength)
	for i := range metadata {
		metadata[i] = byte(i)
	}
	return metadata
}

// encryptDiscoveryMetadata retries the randomized nonce until it has exactly
// the requested hint relationship with users. This removes the small chance
// of a hint collision from policy tests and benchmarks. The probability of
// collision is very low so we don't need to retry a lot.
func encryptDiscoveryMetadata(tb testing.TB, credential []byte, hintUser string, users map[string]*appctlpb.User, wantHint bool, plaintext []byte) []byte {
	tb.Helper()
	for attempt := 0; attempt < 10; attempt++ {
		block, err := cipher.BlockCipherFromPassword(credential, true)
		if err != nil {
			tb.Fatalf("BlockCipherFromPassword() failed: %v", err)
		}
		block.SetBlockContext(cipher.BlockContext{UserName: hintUser})
		encryptedMeta, err := block.Encrypt(plaintext)
		if err != nil {
			tb.Fatalf("Encrypt() failed: %v", err)
		}

		matches := 0
		matchedUser := ""
		for _, user := range users {
			if cipher.CheckUserFromHint([]byte(user.GetName()), encryptedMeta[:cipher.DefaultNonceSize]) {
				matches++
				matchedUser = user.GetName()
			}
		}
		if wantHint && matches == 1 && matchedUser == hintUser {
			return encryptedMeta
		}
		if !wantHint && matches == 0 {
			return encryptedMeta
		}
	}
	tb.Fatalf("failed to generate metadata with requested hint relationship after multiple attempts")
	return nil
}
