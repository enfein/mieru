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

package cli

import (
	"fmt"

	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/version"
)

var versionFunc = func(s []string) error {
	log.Infof(version.AppVersion)
	return nil
}

var checkUpdateFunc = func(s []string) error {
	_, msg, err := version.CheckUpdate()
	if err != nil {
		return fmt.Errorf("check update failed: %w", err)
	}
	log.Infof("%s", msg)
	return nil
}
