// Copyright (C) 2026  mieru authors
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

package mathext

import "golang.org/x/sys/cpu"

func init() {
	if cpu.X86.HasBMI2 {
		pdepImpl = pdepBMI2
		pextImpl = pextBMI2
	}
}

//go:noescape
func pdepBMI2(x, mask uint64) uint64

//go:noescape
func pextBMI2(x, mask uint64) uint64
