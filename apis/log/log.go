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

package log

import (
	"strings"

	"github.com/enfein/mieru/v3/pkg/log"
)

// As libraries, mieru client API and server API disable mieru internal logging.
// We recommend use a callback function to collect internal log messages.

type Message = log.LogMessage
type Callback = log.Callback

// GetLevel returns the current log level.
func GetLevel() string {
	return strings.ToUpper(log.GetLevel().String())
}

// SetLevel sets the log level. It returns true if successful.
//
// This function determines which log messages are passed to the callback function.
func SetLevel(level string) (ok bool) {
	return log.SetLevel(level)
}

// SetCallback registers a callback function that is invoked
// when a log message is produced.
//
// Set the callback to nil to clear the existing callback function.
func SetCallback(callback Callback) {
	log.SetCallback(callback)
}
