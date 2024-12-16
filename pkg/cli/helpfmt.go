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

import "github.com/enfein/mieru/v3/pkg/log"

type helpFormatter struct {
	appName  string
	entries  []helpCmdEntry
	advanced []helpCmdEntry
}

type helpCmdEntry struct {
	cmd  string
	help []string
}

func (m helpFormatter) print() {
	if m.appName != "" {
		log.Infof("Usage: %s <COMMAND> [<ARGS>]", m.appName)
		log.Infof("")
	}
	if len(m.entries) != 0 {
		log.Infof("Commands:")
		for _, entry := range m.entries {
			log.Infof("  %s", entry.cmd)
			for _, line := range entry.help {
				log.Infof("        %s", line)
			}
			log.Infof("")
		}
	}
	if len(m.advanced) != 0 {
		log.Infof("Commands for developers and experienced users:")
		for _, entry := range m.advanced {
			log.Infof("  %s", entry.cmd)
			for _, line := range entry.help {
				log.Infof("        %s", line)
			}
			log.Infof("")
		}
	}
}
