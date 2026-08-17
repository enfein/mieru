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
	"testing"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/enfein/mieru/v3/pkg/protocol/serveruser"
	"google.golang.org/protobuf/proto"
)

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

func testServerUserRegistry(users map[string]*appctlpb.User, hintMandatory bool) *serveruser.Registry {
	registry := &serveruser.Registry{}
	registry.SetUsers(users)
	registry.SetHintMandatory(hintMandatory)
	return registry
}

func newDummyMetadata() []byte {
	metadata := make([]byte, MetadataLength)
	for i := range metadata {
		metadata[i] = byte(i)
	}
	return metadata
}

func encryptDiscoveryMetadata(tb testing.TB, credential []byte, hintUser string, users map[string]*appctlpb.User, wantHint bool, plaintext []byte) []byte {
	tb.Helper()
	for attempt := 0; attempt < 10; attempt++ {
		block, err := cipher.BlockCipherFromPassword(credential, true)
		if err != nil {
			tb.Fatalf("BlockCipherFromPassword() failed: %v", err)
		}
		block.SetBlockContext(cipher.BlockContext{UserName: hintUser})
		encryptedMeta := make([]byte, block.NonceSize()+len(plaintext)+block.Overhead())
		if err := block.Encrypt(encryptedMeta[:0], plaintext); err != nil {
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
