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
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
	"google.golang.org/protobuf/proto"
)

func TestBuildServerUserStateSorted(t *testing.T) {
	users := map[string]*appctlpb.User{
		"map-key-z": {Name: proto.String("z-user"), Password: proto.String("z-password")},
		"map-key-a": {Name: proto.String("a-user"), Password: proto.String("a-password")},
		"wrong-key": {Name: proto.String("m-user"), Password: proto.String("m-password")},
	}
	state := buildServerUserState(users, &sourceUserCacheStats{})
	if len(state.users) != 3 {
		t.Fatalf("compiled user count = %d, want 3", len(state.users))
	}

	// Verify user is sorted.
	for i, wantName := range []string{"a-user", "m-user", "z-user"} {
		user := state.users[i]
		if user.id != uint32(i+1) || user.name != wantName || user.policy.name != wantName {
			t.Fatalf("compiled user %d = (ID %d, name %q, policy %q), want (ID %d, name %q, policy %q)", i, user.id, user.name, user.policy.name, i+1, wantName, wantName)
		}
	}
}

func TestBuildServerUserStateRawAndHashedCredential(t *testing.T) {
	const (
		name     = "equivalent-user"
		password = "equivalent-password"
	)
	prepared := cipher.HashPassword([]byte(password), []byte(name))
	rawState := buildServerUserState(userMap(&appctlpb.User{
		Name:     proto.String(name),
		Password: proto.String(password),
	}), &sourceUserCacheStats{})
	hashedState := buildServerUserState(userMap(makeTestUser(name, prepared)), &sourceUserCacheStats{})
	if len(rawState.users) != 1 || len(hashedState.users) != 1 {
		t.Fatalf("compiled user counts = (%d, %d), want (1, 1)", len(rawState.users), len(hashedState.users))
	}
	if rawState.users[0].credential != hashedState.users[0].credential {
		t.Fatal("raw and hashed credentials compiled to different prepared values")
	}
}

func TestBuildServerUserStateRejectsMalformedCredentials(t *testing.T) {
	const (
		malformedSecret = "raw-secret-that-must-not-be-used"
		malformedHash   = "hashed-secret-that-is-not-hex"
		wrongLenSecret  = "wrong-length-secret"
	)
	state := buildServerUserState(map[string]*appctlpb.User{
		"malformed": {
			Name:           proto.String("malformed-user"),
			Password:       proto.String(malformedSecret),
			HashedPassword: proto.String(malformedHash),
		},
		"wrong-length": {
			Name:           proto.String("wrong-length-user"),
			HashedPassword: proto.String(hex.EncodeToString([]byte(wrongLenSecret))),
		},
	}, &sourceUserCacheStats{})
	if len(state.users) != 0 {
		t.Fatalf("compiled user count = %d, want 0", len(state.users))
	}
}

func TestBuildServerUserStateRejectsInvalidNames(t *testing.T) {
	duplicateA := &appctlpb.User{Name: proto.String("duplicate"), Password: proto.String("password-a")}
	duplicateB := &appctlpb.User{Name: proto.String("duplicate"), Password: proto.String("password-b")}
	state := buildServerUserState(map[string]*appctlpb.User{
		"empty":      {Password: proto.String("password")},
		"long":       {Name: proto.String(strings.Repeat("x", maxUserNameLen+1)), Password: proto.String("password")},
		"duplicate1": duplicateA,
		"duplicate2": duplicateB,
		"valid-key":  {Name: proto.String("valid"), Password: proto.String("password")},
	}, &sourceUserCacheStats{})
	if len(state.users) != 1 || state.users[0].name != "valid" || state.users[0].id != 1 {
		t.Fatalf("compiled users = %+v, want only valid user with ID 1", state.users)
	}
}

func TestSetServerUsersAndMandatoryHintAtomicUpdates(t *testing.T) {
	mux := NewMux(false)
	t.Cleanup(func() { _ = mux.Close() })
	mux.SetServerUsers(rawUserMap("user-00", "password-00"))

	var stop atomic.Bool
	errCh := make(chan error, 1)
	var readers sync.WaitGroup
	for i := 0; i < 8; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for !stop.Load() {
				state := mux.serverUsers.Load()
				if state == nil || len(state.users) != 1 {
					select {
					case errCh <- fmt.Errorf("reader observed incomplete state: %+v", state):
					default:
					}
					return
				}
				user := state.users[0]
				if user.id != 1 || user.name != user.policy.name {
					select {
					case errCh <- fmt.Errorf("reader observed inconsistent user: %+v", user):
					default:
					}
					return
				}
				_ = mux.serverUserHintIsMandatory.Load()
			}
		}()
	}
	for i := 1; i <= 32; i++ {
		name := fmt.Sprintf("user-%02d", i)
		mux.SetServerUsers(rawUserMap(name, fmt.Sprintf("password-%02d", i)))
		mux.SetServerUserHintIsMandatory(i%2 == 0)
	}
	stop.Store(true)
	readers.Wait()
	select {
	case err := <-errCh:
		t.Fatal(err)
	default:
	}
}

func TestTCPUnderlayAcceptedBeforeReloadUsesCurrentGeneration(t *testing.T) {
	mux := NewMux(false).SetServerUsers(rawUserMap("old-user", "old-password"))
	t.Cleanup(func() { _ = mux.Close() })
	underlay := mux.serverWrapTCPConn(nil, 1400, nil).(*StreamUnderlay)
	t.Cleanup(func() { underlay.sessionCleanTicker.Stop() })

	const newUser = "new-user"
	newCredential := cipher.HashPassword([]byte("new-password"), []byte(newUser))
	sourceUser := makeTestUser(newUser, newCredential)
	sourceUser.Quotas = []*appctlpb.Quota{{Days: proto.Int32(5), Megabytes: proto.Int32(11)}}
	newUsers := userMap(sourceUser)
	mux.SetServerUsers(newUsers)

	encrypted := encryptDiscoveryMetadata(t, newCredential, newUser, newUsers, true, newDummyMetadata())
	if _, err := underlay.serverInitRecvBlockCipherAndDecryptMetadata(encrypted); err != nil {
		t.Fatalf("TCP discovery after reload failed: %v", err)
	}
	if got := underlay.recv.BlockContext().UserName; got != newUser {
		t.Fatalf("TCP discovery user = %q, want %q", got, newUser)
	}
	sourceUser.Quotas[0].Days = proto.Int32(1)
	mux.SetServerUsers(rawUserMap("later-user", "later-password"))
	session := newSession(1, false, 1400, underlay.serverUserPolicy, nil, nil)
	retained := session.userPolicy.Load()
	if retained == nil || retained.name != newUser || len(retained.quotas) != 1 || retained.quotas[0] != (serverUserQuota{days: 5, megabytes: 11}) {
		t.Fatalf("established TCP policy changed after caller mutation or reload: %+v", retained)
	}
}

func TestTCPDiscoveryRetriesWhenGenerationChanges(t *testing.T) {
	mux := NewMux(false).SetServerUsers(rawUserMap("old-user", "old-password"))
	t.Cleanup(func() { _ = mux.Close() })

	const newUser = "new-user"
	newCredential := cipher.HashPassword([]byte("new-password"), []byte(newUser))
	newUsers := userMap(makeTestUser(newUser, newCredential))
	encrypted := encryptDiscoveryMetadata(t, newCredential, newUser, newUsers, true, newDummyMetadata())
	var reload sync.Once
	block, plaintext, policy, err := discoverServerUser(
		&mux.serverUsers,
		&mux.serverUserHintIsMandatory,
		encrypted,
		true,
		func(*serverUserState) { reload.Do(func() { mux.SetServerUsers(newUsers) }) },
	)
	if err != nil {
		t.Fatalf("TCP discovery with concurrent reload failed: %v", err)
	}
	if block == nil || !bytes.Equal(plaintext, newDummyMetadata()) || policy.name != newUser {
		t.Fatalf("TCP discovery = (block nil=%t, plaintext=%x, policy=%q), want authenticated %q", block == nil, plaintext, policy.name, newUser)
	}
}

func TestServerUserGenerationRetirementDetachesCache(t *testing.T) {
	mux := NewMux(false).SetServerUsers(rawUserMap("old-user", "old-password"))
	t.Cleanup(func() { _ = mux.Close() })
	old := mux.serverUsers.Load()
	inFlightTable := old.cache.loadTable()
	if inFlightTable == nil {
		t.Fatal("old generation cache table is nil before retirement")
	}
	entry := &sourceUserCacheEntry{key: [16]byte{15: 1}}
	inFlightTable.buckets[0].ways[0].Store(entry)

	mux.SetServerUsers(rawUserMap("new-user", "new-password"))
	current := mux.serverUsers.Load()
	if old.cache.loadTable() != nil {
		t.Fatal("retired generation still publishes a cache table")
	}
	if got := inFlightTable.buckets[0].ways[0].Load(); got != entry {
		t.Fatal("in-flight operation lost its already-loaded cache table")
	}
	if current.cache.loadTable() == nil {
		t.Fatal("new generation has no cache table")
	}
	if old.cache.stats != &mux.serverUserCacheStats || current.cache.stats != &mux.serverUserCacheStats {
		t.Fatal("cache statistics are not shared across Mux user generations")
	}
}

func TestUDPDiscoveryUsesCurrentGenerationAndRetainsPolicySnapshot(t *testing.T) {
	mux := NewMux(false).SetServerUsers(rawUserMap("old-user", "old-password"))
	t.Cleanup(func() { _ = mux.Close() })
	underlay := &PacketUnderlay{
		serverUsers:               &mux.serverUsers,
		serverUserHintIsMandatory: &mux.serverUserHintIsMandatory,
	}

	const newUser = "new-user"
	newCredential := cipher.HashPassword([]byte("new-password"), []byte(newUser))
	sourceUser := makeTestUser(newUser, newCredential)
	sourceUser.Quotas = []*appctlpb.Quota{{Days: proto.Int32(3), Megabytes: proto.Int32(9)}}
	newUsers := userMap(sourceUser)
	mux.SetServerUsers(newUsers)

	encrypted := encryptDiscoveryMetadata(t, newCredential, newUser, newUsers, true, newDummyMetadata())
	block, _, policy, err := underlay.serverTryDecryptMetadataForNewSession(encrypted, encrypted[:cipher.DefaultNonceSize])
	if err != nil {
		t.Fatalf("UDP discovery after reload failed: %v", err)
	}
	if block.BlockContext().UserName != newUser || policy.name != newUser {
		t.Fatalf("UDP discovery selected block user %q and policy %q, want %q", block.BlockContext().UserName, policy.name, newUser)
	}

	session := newSession(1, false, 1400, policy, nil, nil)
	sourceUser.Name = proto.String("mutated-user")
	sourceUser.Quotas[0].Days = proto.Int32(1)
	sourceUser.Quotas[0].Megabytes = proto.Int32(1)
	mux.SetServerUsers(rawUserMap("later-user", "later-password"))
	retained := session.userPolicy.Load()
	if retained == nil || retained.name != newUser || len(retained.quotas) != 1 || retained.quotas[0] != (serverUserQuota{days: 3, megabytes: 9}) {
		t.Fatalf("established UDP session policy changed after mutation or reload: %+v", retained)
	}
	if session.pendingServerUserPolicies != nil {
		t.Fatal("established UDP session retained a full user policy registry")
	}
}

func rawUserMap(name, password string) map[string]*appctlpb.User {
	return map[string]*appctlpb.User{name: {
		Name:     proto.String(name),
		Password: proto.String(password),
	}}
}
