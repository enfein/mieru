// Copyright (C) 2021  mieru authors
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

package appctl

import (
	"encoding/hex"

	pb "github.com/enfein/mieru/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/pkg/cipher"
	"google.golang.org/protobuf/proto"
)

// UserListToMap convert a slice of User to a map of <name, User>.
func UserListToMap(users []*pb.User) map[string]*pb.User {
	m := map[string]*pb.User{}
	for i := 0; i < len(users); i++ {
		u := users[i]
		m[u.GetName()] = u
	}
	return m
}

// HashUserPassword replaces user's password with hashed password.
func HashUserPassword(user *pb.User, keepPlaintext bool) *pb.User {
	if user == nil || user.GetPassword() == "" {
		return user
	}
	user.HashedPassword = proto.String(hex.EncodeToString(cipher.HashPassword([]byte(user.GetPassword()), []byte(user.GetName()))))
	if !keepPlaintext {
		user.Password = proto.String("")
	}
	return user
}

// HashUserPasswords replaces user's password with hashed password for a slice of users.
func HashUserPasswords(users []*pb.User, keepPlaintext bool) []*pb.User {
	for i := 0; i < len(users); i++ {
		users[i] = HashUserPassword(users[i], keepPlaintext)
	}
	return users
}
