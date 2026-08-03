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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync/atomic"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/enfein/mieru/v3/pkg/log"
)

// This limit matches with pkg/appctl/appctlcommon/server.go and pkg/appctl/appctlcommon/client.go
const maxUserNameLen = 64

// serverUser is the immutable authentication record used by server side user discovery.
// IDs are dense within one generation and start at 1. ID 0 is reserved for an
// empty source-user cache slot.
type serverUser struct {
	id         uint32
	name       string
	credential [sha256.Size]byte
	policy     serverUserPolicy
}

// serverUserPolicy contains the value-based user settings retained after
// authentication. It must not contain caller-owned maps or protobuf messages.
type serverUserPolicy struct {
	name            string
	quotas          []serverUserQuota
	allowPrivateIP  bool
	allowLoopbackIP bool
}

type serverUserQuota struct {
	days      int32
	megabytes int32
}

// serverUserState is immutable after publication, except for its explicitly
// concurrent cache. A cache belongs to exactly one state generation.
type serverUserState struct {
	users []serverUser
	cache *sourceUserCache
}

// sourceUserCache owns an atomically detachable table. Cache lookup and update
// operations load table once; retirement prevents later operations from
// starting while allowing an operation that already loaded the table to finish.
type sourceUserCache struct {
	table atomic.Pointer[sourceUserCacheTable]
	stats *sourceUserCacheStats
}

// sourceUserCacheStats has Mux lifetime rather than generation lifetime.
type sourceUserCacheStats struct {
	lookups   atomic.Uint64
	hits      atomic.Uint64
	misses    atomic.Uint64
	records   atomic.Uint64
	evictions atomic.Uint64
}

const (
	sourceUserCacheBucketCount = 16384
	sourceUserCacheWays        = 4
	sourceUserCacheUsers       = 10
)

type sourceUserCacheTable struct {
	buckets [sourceUserCacheBucketCount]sourceUserCacheBucket
}

type sourceUserCacheBucket struct {
	ways [sourceUserCacheWays]atomic.Pointer[sourceUserCacheEntry]
}

type sourceUserCacheEntry struct {
	key        [16]byte
	lastActive atomic.Uint32
	users      [sourceUserCacheUsers]atomic.Uint64
}

func newSourceUserCache(stats *sourceUserCacheStats) *sourceUserCache {
	c := &sourceUserCache{stats: stats}
	c.table.Store(&sourceUserCacheTable{})
	return c
}

func (c *sourceUserCache) loadTable() *sourceUserCacheTable {
	if c == nil {
		return nil
	}
	return c.table.Load()
}

func (c *sourceUserCache) retire() {
	if c != nil {
		c.table.Swap(nil)
	}
}

func buildServerUserState(users map[string]*appctlpb.User, stats *sourceUserCacheStats) *serverUserState {
	type inputUser struct {
		mapKey string
		user   *appctlpb.User
		name   string
	}

	inputs := make([]inputUser, 0, len(users))
	nameCounts := make(map[string]int, len(users))
	for mapKey, user := range users {
		name := ""
		if user != nil {
			name = user.GetName()
		}
		inputs = append(inputs, inputUser{mapKey: mapKey, user: user, name: name})
		nameCounts[name]++
	}

	// Sort users by name.
	sort.Slice(inputs, func(i, j int) bool {
		if inputs[i].name == inputs[j].name {
			return inputs[i].mapKey < inputs[j].mapKey
		}
		return inputs[i].name < inputs[j].name
	})

	compiled := make([]serverUser, 0, len(inputs))
	loggedDuplicate := make(map[string]struct{})
	loggedEmptyName := false
	for _, input := range inputs {
		if input.name == "" {
			if !loggedEmptyName {
				log.Warnf("Skipping server user with empty name")
				loggedEmptyName = true
			}
			continue
		}
		if nameCounts[input.name] > 1 {
			if _, ok := loggedDuplicate[input.name]; !ok {
				log.Warnf("Skipping duplicate server user name %q", input.name)
				loggedDuplicate[input.name] = struct{}{}
			}
			continue
		}
		if len(input.name) > maxUserNameLen {
			log.Warnf("Skipping server user %q: name exceeds %d bytes", input.name, maxUserNameLen)
			continue
		}

		credential, err := buildServerUserCredential(input.user, input.name)
		if err != nil {
			log.Warnf("Skipping server user %q: %v", input.name, err)
			continue
		}
		compiled = append(compiled, serverUser{
			id:         uint32(len(compiled) + 1),
			name:       input.name,
			credential: credential,
			policy:     buildServerUserPolicy(input.user),
		})
	}

	return &serverUserState{
		users: compiled,
		cache: newSourceUserCache(stats),
	}
}

func buildServerUserCredential(user *appctlpb.User, name string) ([sha256.Size]byte, error) {
	var credential [sha256.Size]byte
	if user == nil {
		return credential, fmt.Errorf("user record is nil")
	}
	if user.GetHashedPassword() != "" {
		decoded, err := hex.DecodeString(user.GetHashedPassword())
		if err != nil {
			return credential, fmt.Errorf("hashed credential is not valid hexadecimal")
		}
		if len(decoded) != len(credential) {
			return credential, fmt.Errorf("hashed credential decodes to %d bytes, want %d", len(decoded), len(credential))
		}
		copy(credential[:], decoded)
		return credential, nil
	}
	if user.GetPassword() == "" {
		return credential, fmt.Errorf("credential is empty")
	}
	copy(credential[:], cipher.HashPassword([]byte(user.GetPassword()), []byte(name)))
	return credential, nil
}

func buildServerUserPolicy(user *appctlpb.User) serverUserPolicy {
	policy := serverUserPolicy{
		name:            user.GetName(),
		allowPrivateIP:  user.GetAllowPrivateIP(),
		allowLoopbackIP: user.GetAllowLoopbackIP(),
	}
	if len(user.GetQuotas()) > 0 {
		policy.quotas = make([]serverUserQuota, 0, len(user.GetQuotas()))
		for _, quota := range user.GetQuotas() {
			policy.quotas = append(policy.quotas, serverUserQuota{
				days:      quota.GetDays(),
				megabytes: quota.GetMegabytes(),
			})
		}
	}
	return policy
}

func buildServerUserPolicies(users map[string]*appctlpb.User) map[string]serverUserPolicy {
	if len(users) == 0 {
		return nil
	}
	policies := make(map[string]serverUserPolicy, len(users))
	for _, user := range users {
		if user == nil || user.GetName() == "" {
			continue
		}
		policies[user.GetName()] = buildServerUserPolicy(user)
	}
	return policies
}

// discoverServerUser tries one immutable state generation. When requireCurrent
// is true, a concurrent publication forces the complete discovery to retry
// before the result can be installed.
func discoverServerUser(
	publisher *atomic.Pointer[serverUserState],
	hintMandatory *atomic.Bool,
	encryptedMetadata []byte,
	requireCurrent bool,
	afterAttempt func(*serverUserState),
) (cipher.BlockCipher, []byte, serverUserPolicy, error) {
	if len(encryptedMetadata) < cipher.DefaultNonceSize {
		return nil, nil, serverUserPolicy{}, fmt.Errorf("encrypted metadata is shorter than nonce")
	}
	if publisher == nil {
		return nil, nil, serverUserPolicy{}, fmt.Errorf("server user publisher is nil")
	}

	for {
		state := publisher.Load()
		if state == nil || len(state.users) == 0 {
			return nil, nil, serverUserPolicy{}, fmt.Errorf("no server user found")
		}
		mandatory := false
		if hintMandatory != nil {
			mandatory = hintMandatory.Load()
		}

		block, plaintext, policy := tryServerUserState(state, encryptedMetadata, mandatory)
		if afterAttempt != nil {
			afterAttempt(state)
		}
		if requireCurrent && publisher.Load() != state {
			continue
		}
		if block == nil {
			return nil, nil, serverUserPolicy{}, fmt.Errorf("cipher.TryDecrypt() failed for all users")
		}
		return block, plaintext, policy, nil
	}
}

func tryServerUserState(state *serverUserState, encryptedMeta []byte, hintMandatory bool) (cipher.BlockCipher, []byte, serverUserPolicy) {
	nonce := encryptedMeta[:cipher.DefaultNonceSize]
	for i := range state.users {
		user := &state.users[i]
		if !checkServerUserHint(user.name, nonce) {
			continue
		}
		cipher.ServerHintMatchDecrypt.Add(1)
		block, plaintext, err := cipher.TryDecrypt(encryptedMeta, user.credential[:], true)
		if err == nil {
			return block, plaintext, user.policy
		}
		cipher.ServerFailedHintMatchDecrypt.Add(1)
	}

	if !hintMandatory {
		for i := range state.users {
			user := &state.users[i]
			block, plaintext, err := cipher.TryDecrypt(encryptedMeta, user.credential[:], true)
			if err == nil {
				return block, plaintext, user.policy
			}
		}
	}
	return nil, nil, serverUserPolicy{}
}

// checkServerUserHint is a faster implementation of
// cipher.CheckUserFromHint function.
func checkServerUserHint(name string, nonce []byte) bool {
	if len(nonce) < cipher.NoncePrefixLenForUserHint+cipher.NonceSuffixLenForUserHint {
		return false
	}
	var input [maxUserNameLen + cipher.NoncePrefixLenForUserHint]byte
	n := copy(input[:], name)
	n += copy(input[n:], nonce[:cipher.NoncePrefixLenForUserHint])
	output := sha256.Sum256(input[:n])
	return bytes.Equal(output[:cipher.NonceSuffixLenForUserHint], nonce[len(nonce)-cipher.NonceSuffixLenForUserHint:])
}
