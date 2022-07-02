// Copyright (C) 2022  mieru authors
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

import "strings"

type ConfigFileType int

const (
	INVALID_CONFIG_FILE_TYPE ConfigFileType = iota
	PROTOBUF_CONFIG_FILE_TYPE
	JSON_CONFIG_FILE_TYPE
)

// FindConfigFileType returns the type of configuration file.
// Similar to Windows, it uses file extension name to make the decision.
func FindConfigFileType(fileName string) ConfigFileType {
	if strings.HasSuffix(fileName, ".json") {
		return JSON_CONFIG_FILE_TYPE
	}
	return PROTOBUF_CONFIG_FILE_TYPE
}
