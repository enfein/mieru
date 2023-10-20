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
// along with this program.  If not, see <https://www.gnu.org/licenses/>

package protocolv2

import (
	crand "crypto/rand"
	"fmt"
	mrand "math/rand"

	"github.com/enfein/mieru/pkg/rng"
	"github.com/enfein/mieru/pkg/util"
)

var (
	recommendedConsecutiveASCIILen = 32 + rng.FixedInt(33)
)

type paddingOpts struct {
	// The maxinum length of padding.
	maxLen int

	// The mininum length of consecutive ASCII characters.
	// This implies the mininum length of padding.
	minConsecutiveASCIILen int
}

func newPadding(opts paddingOpts) []byte {
	if opts.maxLen < opts.minConsecutiveASCIILen {
		panic(fmt.Sprintf("Invalid padding options: maxLen %d is smaller than minConsecutiveASCIILen %d", opts.maxLen, opts.minConsecutiveASCIILen))
	}
	length := rng.Intn(opts.maxLen-opts.minConsecutiveASCIILen+1) + opts.minConsecutiveASCIILen
	p := make([]byte, length)
	for {
		if _, err := crand.Read(p); err == nil {
			break
		}
	}
	beginIdx := 0
	if length > opts.minConsecutiveASCIILen {
		beginIdx = mrand.Intn(length - opts.minConsecutiveASCIILen)
	}
	util.ToPrintableChar(p, beginIdx, beginIdx+opts.minConsecutiveASCIILen)
	return p
}
