// Copyright (C) 2025  mieru authors
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

package appctlcommon

import (
	"fmt"

	pb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
)

// ValidateServerConfigSingleUser validates
// a single server config user.
//
// It validates
// 1. user name is not empty
// 2. user has either a password or a hashed password
// 3. for each quota
// 3.1. number of days is valid
// 3.2. traffic volume in megabyte is valid
func ValidateServerConfigSingleUser(user *pb.User) error {
	if user.GetName() == "" {
		return fmt.Errorf("user name is not set")
	}
	if user.GetPassword() == "" && user.GetHashedPassword() == "" {
		return fmt.Errorf("user password is not set")
	}
	for _, quota := range user.GetQuotas() {
		if quota.GetDays() <= 0 {
			return fmt.Errorf("quota: number of days %d is invalid", quota.GetDays())
		}
		if quota.GetMegabytes() <= 0 {
			return fmt.Errorf("quota: traffic volume in megabyte %d is invalid", quota.GetMegabytes())
		}
	}
	return nil
}
