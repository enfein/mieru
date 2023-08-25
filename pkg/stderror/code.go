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
)

var (
	ErrAlreadyExist     = fmt.Errorf("ALREADY EXIST")
	ErrAlreadyStarted   = fmt.Errorf("ALREADY STARTED")
	ErrDisconnected     = fmt.Errorf("DISCONNECTED")
	ErrEmpty            = fmt.Errorf("EMPTY")
	ErrFileNotExist     = fmt.Errorf("FILE NOT EXIST")
	ErrFull             = fmt.Errorf("FULL")
	ErrIDNotMatch       = fmt.Errorf("ID NOT MATCH")
	ErrInternal         = fmt.Errorf("INTERNAL")
	ErrInvalidArgument  = fmt.Errorf("INVALID ARGUMENT")
	ErrInvalidOperation = fmt.Errorf("INVALID OPERATION")
	ErrNoEnoughData     = fmt.Errorf("NO ENOUGH DATA")
	ErrNotFound         = fmt.Errorf("NOT FOUND")
	ErrNotReady         = fmt.Errorf("NOT READY")
	ErrNotRunning       = fmt.Errorf("NOT RUNNING")
	ErrNullPointer      = fmt.Errorf("NULL POINTER")
	ErrOutOfRange       = fmt.Errorf("OUT OF RANGE")
	ErrTimeout          = fmt.Errorf("TIMEOUT")
	ErrUnknownCommand   = fmt.Errorf("UNKNOWN COMMAND")
	ErrUnsupported      = fmt.Errorf("UNSUPPORTED")
)
