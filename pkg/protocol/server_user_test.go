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
	"sync"
	"sync/atomic"
	"testing"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/enfein/mieru/v3/pkg/metrics"
	"github.com/enfein/mieru/v3/pkg/protocol/serveruser"
	"google.golang.org/protobuf/proto"
)

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
				if !mux.serverUsers.HasUsers() {
					select {
					case errCh <- fmt.Errorf("reader observed no usable server users"):
					default:
					}
					return
				}
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
	if _, authentication, err := underlay.serverInitRecvBlockCipherAndDecryptMetadata(encrypted); err != nil {
		t.Fatalf("TCP discovery after reload failed: %v", err)
	} else {
		underlay.serverUserPolicy = authentication.Policy()
	}
	if got := underlay.recv.BlockContext().UserName; got != newUser {
		t.Fatalf("TCP discovery user = %q, want %q", got, newUser)
	}
	sourceUser.Quotas[0].Days = proto.Int32(1)
	mux.SetServerUsers(rawUserMap("later-user", "later-password"))
	session := newSessionWithServerUserPolicy(1, false, 1400, underlay.serverUserPolicy, nil, nil)
	retained := session.userPolicy.Load()
	if retained == nil || retained.Name() != newUser || len(retained.Quotas()) != 1 || retained.Quotas()[0].Days() != 5 || retained.Quotas()[0].Megabytes() != 11 {
		t.Fatalf("established TCP policy changed after caller mutation or reload: %+v", retained)
	}
}

func TestTCPAcceptedBeforeReloadRejectsStaleCredentials(t *testing.T) {
	tests := []struct {
		name        string
		currentUser string
		currentPass string
	}{
		{
			name:        "password changed",
			currentUser: "reload-user",
			currentPass: "new-password",
		},
		{
			name:        "user removed",
			currentUser: "replacement-user",
			currentPass: "replacement-password",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const (
				oldUser = "reload-user"
				oldPass = "old-password"
			)
			oldUsers := rawUserMap(oldUser, oldPass)
			mux := NewMux(false).SetServerUsers(oldUsers)
			t.Cleanup(func() { _ = mux.Close() })
			underlay := mux.serverWrapTCPConn(nil, 1400, nil).(*StreamUnderlay)
			t.Cleanup(func() { underlay.sessionCleanTicker.Stop() })

			currentUsers := rawUserMap(test.currentUser, test.currentPass)
			mux.SetServerUsers(currentUsers)
			oldCredential := cipher.HashPassword([]byte(oldPass), []byte(oldUser))
			oldEncrypted := encryptDiscoveryMetadata(t, oldCredential, oldUser, oldUsers, true, newDummyMetadata())
			if _, authentication, err := underlay.serverInitRecvBlockCipherAndDecryptMetadata(oldEncrypted); err == nil {
				t.Fatal("TCP discovery accepted a credential from the retired generation")
			} else if authentication.Valid() || underlay.recv != nil {
				t.Fatal("failed stale discovery retained authentication state or a receive cipher")
			}

			currentCredential := cipher.HashPassword([]byte(test.currentPass), []byte(test.currentUser))
			currentEncrypted := encryptDiscoveryMetadata(t, currentCredential, test.currentUser, currentUsers, true, newDummyMetadata())
			if _, authentication, err := underlay.serverInitRecvBlockCipherAndDecryptMetadata(currentEncrypted); err != nil {
				t.Fatalf("TCP discovery with the current credential failed: %v", err)
			} else if authentication.Policy().Name() != test.currentUser || underlay.recv.BlockContext().UserName != test.currentUser {
				t.Fatalf("TCP discovery selected policy %q and cipher user %q, want %q", authentication.Policy().Name(), underlay.recv.BlockContext().UserName, test.currentUser)
			}
		})
	}
}

func TestTCPRepeatedReloadsUseFinalGeneration(t *testing.T) {
	const reloads = 8
	mux := NewMux(false).SetServerUsers(rawUserMap("reload-user-00", "reload-password-00"))
	t.Cleanup(func() { _ = mux.Close() })

	// These sockets are deliberately created before any reload and must not
	// retain the generation that was current when they were accepted.
	idleUnderlays := make([]*StreamUnderlay, 3)
	for i := range idleUnderlays {
		idleUnderlays[i] = mux.serverWrapTCPConn(nil, 1400, nil).(*StreamUnderlay)
		defer idleUnderlays[i].sessionCleanTicker.Stop()
	}

	var finalUsers map[string]*appctlpb.User
	var finalUser, finalPassword string
	for i := 1; i <= reloads; i++ {
		finalUser = fmt.Sprintf("reload-user-%02d", i)
		finalPassword = fmt.Sprintf("reload-password-%02d", i)
		finalUsers = rawUserMap(finalUser, finalPassword)
		mux.SetServerUsers(finalUsers)
	}

	finalCredential := cipher.HashPassword([]byte(finalPassword), []byte(finalUser))
	finalEncrypted := encryptDiscoveryMetadata(t, finalCredential, finalUser, finalUsers, true, newDummyMetadata())
	for i, underlay := range idleUnderlays {
		_, authentication, err := underlay.serverInitRecvBlockCipherAndDecryptMetadata(finalEncrypted)
		if err != nil {
			t.Fatalf("idle TCP underlay %d failed final-generation discovery: %v", i, err)
		}
		if authentication.Policy().Name() != finalUser || underlay.recv.BlockContext().UserName != finalUser {
			t.Fatalf("idle TCP underlay %d installed a noncurrent generation or receive cipher", i)
		}
		authentication.Record()
	}
}

func TestUDPDiscoveryUsesCurrentGenerationAndRetainsPolicySnapshot(t *testing.T) {
	mux := NewMux(false).SetServerUsers(rawUserMap("old-user", "old-password"))
	t.Cleanup(func() { _ = mux.Close() })
	underlay := &PacketUnderlay{
		serverUsers: &mux.serverUsers,
	}

	const newUser = "new-user"
	newCredential := cipher.HashPassword([]byte("new-password"), []byte(newUser))
	sourceUser := makeTestUser(newUser, newCredential)
	sourceUser.Quotas = []*appctlpb.Quota{{Days: proto.Int32(3), Megabytes: proto.Int32(9)}}
	newUsers := userMap(sourceUser)
	mux.SetServerUsers(newUsers)

	encrypted := encryptDiscoveryMetadata(t, newCredential, newUser, newUsers, true, newDummyMetadata())
	block, _, authentication, err := underlay.serverTryDecryptMetadataForNewSession(encrypted, serveruser.Source{})
	if err != nil {
		t.Fatalf("UDP discovery after reload failed: %v", err)
	}
	policy := authentication.Policy()
	if block.BlockContext().UserName != newUser || policy.Name() != newUser {
		t.Fatalf("UDP discovery selected block user %q and policy %q, want %q", block.BlockContext().UserName, policy.Name(), newUser)
	}

	session := newSessionWithServerUserPolicy(1, false, 1400, policy, nil, nil)
	sourceUser.Name = proto.String("mutated-user")
	sourceUser.Quotas[0].Days = proto.Int32(1)
	sourceUser.Quotas[0].Megabytes = proto.Int32(1)
	mux.SetServerUsers(rawUserMap("later-user", "later-password"))
	retained := session.userPolicy.Load()
	if retained == nil || retained.Name() != newUser || len(retained.Quotas()) != 1 || retained.Quotas()[0].Days() != 3 || retained.Quotas()[0].Megabytes() != 9 {
		t.Fatalf("established UDP session policy changed after mutation or reload: %+v", retained)
	}
	if session.pendingServerUserPolicies != nil {
		t.Fatal("established UDP session retained a full user policy registry")
	}
}

func TestMuxCloseFlushesServerUserCacheMetrics(t *testing.T) {
	metricGroup := metrics.GetMetricGroupByName(serveruser.SourceUserCacheMetricGroupName)
	if metricGroup == nil {
		t.Fatal("server user cache metric group is not registered")
	}
	insertions, found := metricGroup.GetMetric("Insertions")
	if !found {
		t.Fatal("server user cache insertion metric is not registered")
	}
	before := insertions.Load()

	const userName = "close-flush-user"
	credential := cipher.HashPassword([]byte(t.Name()), []byte(userName))
	users := userMap(makeTestUser(userName, credential))
	mux := NewMux(false).SetServerUsers(users)
	encrypted := encryptDiscoveryMetadata(t, credential, userName, users, true, newDummyMetadata())
	_, _, authentication, err := mux.serverUsers.Discover(
		encrypted,
		serveruser.SourceFromAddr(&net.TCPAddr{IP: net.ParseIP("192.0.2.80"), Port: 18001}),
		false,
	)
	if err != nil {
		_ = mux.Close()
		t.Fatalf("server user discovery failed: %v", err)
	}
	authentication.Record()
	if err := mux.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}
	if got := insertions.Load(); got != before+1 {
		t.Fatalf("published cache insertions = %d, want %d", got, before+1)
	}
}

func rawUserMap(name, password string) map[string]*appctlpb.User {
	return map[string]*appctlpb.User{name: {
		Name:     proto.String(name),
		Password: proto.String(password),
	}}
}
