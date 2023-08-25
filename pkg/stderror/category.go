// Copyright (C) 2023  mieru authors
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
	"errors"
	"io"
	"strings"
)

// IsClosed returns true if the cause of error is connection close.
func IsClosed(err error) bool {
	if errors.Is(err, io.ErrClosedPipe) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "read/write on closed pipe") || strings.Contains(s, "use of closed network connection")
}

// IsConnRefused returns true if the cause of error is connection refused.
func IsConnRefused(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "connection refused") || strings.Contains(s, "no connection could be made because the target machine actively refused it")
}

// IsEOF returns true if the cause of error is EOF.
func IsEOF(err error) bool {
	return errors.Is(err, io.EOF)
}

// IsPermissionDenied returns true if the cause of error is permission denied.
func IsPermissionDenied(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "permission denied")
}

// ShouldRetry returns true if the caller should retry the same operation again.
func ShouldRetry(err error) bool {
	return errors.Is(err, ErrNotReady)
}
