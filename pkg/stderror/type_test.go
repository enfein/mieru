// Copyright (C) 2024  mieru authors
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
	"testing"
)

func TestErrorType(t *testing.T) {
	testCases := []struct {
		name    string
		err     error
		errType ErrorType
	}{
		{
			name:    "nil error",
			err:     nil,
			errType: NO_ERROR,
		},
		{
			name:    "unknown error",
			err:     fmt.Errorf("unknown error"),
			errType: UNKNOWN_ERROR,
		},
		{
			name:    "protocol error",
			err:     fmt.Errorf("protocol error"),
			errType: PROTOCOL_ERROR,
		},
		{
			name:    "network error",
			err:     fmt.Errorf("network error"),
			errType: NETWORK_ERROR,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			wrappedErr := WrapErrorWithType(tc.err, tc.errType)
			actualType := GetErrorType(wrappedErr)
			if actualType != tc.errType {
				t.Errorf("got error type %v, want %v", actualType, tc.errType)
			}

			if tc.err != nil {
				if wrappedErr.Error() != tc.err.Error() {
					t.Errorf("got error string %q, want %q", wrappedErr.Error(), tc.err.Error())
				}
			} else if wrappedErr.Error() != "" {
				t.Errorf("got non empty error string %q for nil error", wrappedErr.Error())
			}
		})
	}
}
