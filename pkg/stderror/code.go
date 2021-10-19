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

package stderror

import (
	"fmt"
	"strings"
)

var (
	ErrAlreadyExist     = fmt.Errorf("ALREADY EXIST")
	ErrFileNotExist     = fmt.Errorf("FILE NOT EXIST")
	ErrIDNotMatch       = fmt.Errorf("ID NOT MATCH")
	ErrInternal         = fmt.Errorf("INTERNAL")
	ErrInvalidArgument  = fmt.Errorf("INVALID ARGUMENT")
	ErrInvalidOperation = fmt.Errorf("INVALID OPERATION")
	ErrNoEnoughData     = fmt.Errorf("NO ENOUGH DATA")
	ErrNotFound         = fmt.Errorf("NOT FOUND")
	ErrNotRunning       = fmt.Errorf("NOT RUNNING")
	ErrOutOfRange       = fmt.Errorf("OUT OF RANGE")
	ErrTimeout          = fmt.Errorf("TIMEOUT")
	ErrUnknownCommand   = fmt.Errorf("UNKNOWN COMMAND")
)

// IsConnRefused returns true if the cause of error is connection refused.
func IsConnRefused(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "connection refused") || strings.Contains(s, "no connection could be made because the target machine actively refused it")
}

// IsPermissionDenied returns true if the cause of error is permission denied.
func IsPermissionDenied(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "permission denied")
}
