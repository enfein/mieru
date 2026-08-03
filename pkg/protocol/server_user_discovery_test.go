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
		name: "TCP",
		discover: func(users map[string]*appctlpb.User, hintMandatory bool, encryptedMeta []byte) ([]byte, string, error) {
			publisher, mandatory := testServerUserPublisher(users, hintMandatory)
			underlay := &StreamUnderlay{
				serverUsers:               publisher,
				serverUserHintIsMandatory: mandatory,
			}
			decryptedMeta, err := underlay.serverInitRecvBlockCipherAndDecryptMetadata(encryptedMeta)
			if err != nil {
				if underlay.recv != nil {
					return nil, "", fmt.Errorf("failed discovery retained a receive cipher")
				}
				return nil, "", err
			}
			return decryptedMeta, underlay.recv.BlockContext().UserName, nil
		},
	},
	{
		name: "UDP",
		discover: func(users map[string]*appctlpb.User, hintMandatory bool, encryptedMeta []byte) ([]byte, string, error) {
			publisher, mandatory := testServerUserPublisher(users, hintMandatory)
			underlay := &PacketUnderlay{
				serverUsers:               publisher,
				serverUserHintIsMandatory: mandatory,
			}
			block, decryptedMeta, _, err := underlay.serverTryDecryptMetadataForNewSession(
				encryptedMeta,
				encryptedMeta[:cipher.DefaultNonceSize],
			)
			if err != nil {
				if block != nil {
					return nil, "", fmt.Errorf("failed discovery returned a block cipher")
				}
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
	metadata := make([]byte, MetadataLength)
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
		block = block.Clone()
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
