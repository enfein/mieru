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
	"strings"
	"sync"
	"testing"

	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
	"google.golang.org/protobuf/proto"
)

func TestBuildStateSorted(t *testing.T) {
	users := map[string]*appctlpb.User{
		"map-key-z": {Name: proto.String("z-user"), Password: proto.String("z-password")},
		"map-key-a": {Name: proto.String("a-user"), Password: proto.String("a-password")},
		"wrong-key": {Name: proto.String("m-user"), Password: proto.String("m-password")},
	}
	state := buildState(users, &sourceUserCacheStats{})
	if len(state.users) != 3 {
		t.Fatalf("compiled user count = %d, want 3", len(state.users))
	}
	for i, wantName := range []string{"a-user", "m-user", "z-user"} {
		user := state.users[i]
		if user.id != uint32(i+1) || user.name != wantName || user.policy.name != wantName {
			t.Fatalf("compiled user %d = (ID %d, name %q, policy %q), want (ID %d, name %q, policy %q)", i, user.id, user.name, user.policy.name, i+1, wantName, wantName)
		}
	}
}

func TestBuildStateRawAndHashedCredential(t *testing.T) {
	const name, password = "equivalent-user", "equivalent-password"
	prepared := cipher.HashPassword([]byte(password), []byte(name))
	rawState := buildState(userMap(&appctlpb.User{Name: proto.String(name), Password: proto.String(password)}), &sourceUserCacheStats{})
	hashedState := buildState(userMap(makeTestUser(name, prepared)), &sourceUserCacheStats{})
	if len(rawState.users) != 1 || len(hashedState.users) != 1 {
		t.Fatalf("compiled user counts = (%d, %d), want (1, 1)", len(rawState.users), len(hashedState.users))
	}
	if rawState.users[0].credential != hashedState.users[0].credential {
		t.Fatal("raw and hashed credentials compiled to different prepared values")
	}
}

func TestBuildStateRejectsMalformedCredentials(t *testing.T) {
	state := buildState(map[string]*appctlpb.User{
		"malformed": {
			Name:           proto.String("malformed-user"),
			Password:       proto.String("raw-secret-that-must-not-be-used"),
			HashedPassword: proto.String("hashed-secret-that-is-not-hex"),
		},
		"wrong-length": {
			Name:           proto.String("wrong-length-user"),
			HashedPassword: proto.String(hex.EncodeToString([]byte("wrong-length-secret"))),
		},
	}, &sourceUserCacheStats{})
	if len(state.users) != 0 {
		t.Fatalf("compiled user count = %d, want 0", len(state.users))
	}
}

func TestBuildStateRejectsInvalidNames(t *testing.T) {
	duplicateA := &appctlpb.User{Name: proto.String("duplicate"), Password: proto.String("password-a")}
	duplicateB := &appctlpb.User{Name: proto.String("duplicate"), Password: proto.String("password-b")}
	state := buildState(map[string]*appctlpb.User{
		"empty":      {Password: proto.String("password")},
		"long":       {Name: proto.String(strings.Repeat("x", constant.MaxUserNameLen+1)), Password: proto.String("password")},
		"duplicate1": duplicateA,
		"duplicate2": duplicateB,
		"valid-key":  {Name: proto.String("valid"), Password: proto.String("password")},
	}, &sourceUserCacheStats{})
	if len(state.users) != 1 || state.users[0].name != "valid" || state.users[0].id != 1 {
		t.Fatalf("compiled users = %+v, want only valid user with ID 1", state.users)
	}
}

func TestDiscoveryRetriesWhenGenerationChanges(t *testing.T) {
	registry := &Registry{}
	registry.SetUsers(rawUserMap("old-user", "old-password"))

	const newUser = "new-user"
	newCredential := cipher.HashPassword([]byte("new-password"), []byte(newUser))
	newUsers := userMap(makeTestUser(newUser, newCredential))
	encrypted := encryptDiscoveryMetadata(t, newCredential, newUser, newUsers, true, newDummyMetadata())
	var reload sync.Once
	result, err := discoverUser(
		&registry.users,
		&registry.hintMandatory,
		encrypted,
		Source{},
		true,
		func(*state) { reload.Do(func() { registry.SetUsers(newUsers) }) },
	)
	if err != nil {
		t.Fatalf("discovery with concurrent reload failed: %v", err)
	}
	if result.block == nil || !bytes.Equal(result.decryptedMetadata, newDummyMetadata()) || result.policy.name != newUser {
		t.Fatalf("discovery = (block nil=%t, plaintext=%x, policy=%q), want authenticated %q", result.block == nil, result.decryptedMetadata, result.policy.name, newUser)
	}
}

func TestDiscoverySurvivesRepeatedReloads(t *testing.T) {
	const reloads = 8
	registry := &Registry{}
	registry.SetUsers(rawUserMap("in-progress-user-00", "in-progress-password-00"))

	reloadUsers := make([]map[string]*appctlpb.User, reloads)
	for i := range reloadUsers {
		reloadUsers[i] = rawUserMap(
			fmt.Sprintf("in-progress-user-%02d", i+1),
			fmt.Sprintf("in-progress-password-%02d", i+1),
		)
	}
	finalUser := fmt.Sprintf("in-progress-user-%02d", reloads)
	finalPassword := fmt.Sprintf("in-progress-password-%02d", reloads)
	finalCredential := cipher.HashPassword([]byte(finalPassword), []byte(finalUser))
	finalEncrypted := encryptDiscoveryMetadata(t, finalCredential, finalUser, reloadUsers[len(reloadUsers)-1], true, newDummyMetadata())

	retired := make([]*state, 0, reloads)
	nextReload := 0
	result, err := discoverUser(
		&registry.users,
		&registry.hintMandatory,
		finalEncrypted,
		Source{},
		true,
		func(attempted *state) {
			if nextReload < len(reloadUsers) {
				retired = append(retired, attempted)
				registry.SetUsers(reloadUsers[nextReload])
				nextReload++
			}
		},
	)
	if err != nil {
		t.Fatalf("discovery with repeated concurrent reloads failed: %v", err)
	}
	current := registry.users.Load()
	if result.generation != current || result.policy.name != finalUser || result.userContext.UserName != finalUser {
		t.Fatalf("discovery returned a stale generation, policy, or cipher context: %+v", result)
	}
	for i := range retired {
		if retired[i].cache.loadTable() != nil {
			t.Fatalf("retired generation %d still publishes a cache table", i)
		}
	}
}

func rawUserMap(name, password string) map[string]*appctlpb.User {
	return map[string]*appctlpb.User{name: {
		Name:     proto.String(name),
		Password: proto.String(password),
	}}
}
