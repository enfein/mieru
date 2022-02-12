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

package cipher

import (
	"fmt"
	"sync"
	"time"
)

const cacheValidInterval = 1 * time.Minute

type cachedCiphers struct {
	cipherList []BlockCipher
	createTime time.Time
}

var blockCipherCache = sync.Map{}

func getBlockCipherList(password []byte) ([]BlockCipher, error) {
	pw := string(password)
	c, ok := blockCipherCache.Load(pw)
	if ok {
		// Check if the cached entry is expired.
		if c.(cachedCiphers).createTime.Add(cacheValidInterval).Before(time.Now()) {
			ok = false
		}
	}
	if ok {
		return c.(cachedCiphers).cipherList, nil
	}

	// Otherwise, generate the []BlockCipher and insert it into the cache.
	t := time.Now()
	salts := saltFromTime(t)
	blockCiphers := make([]BlockCipher, 0, 3)
	for i := 0; i < 3; i++ {
		keygen := pbkdf2Gen{
			Salt: salts[i],
			Iter: defaultIter,
		}
		cipherKey, err := keygen.NewKey(password, DefaultKeyLen)
		if err != nil {
			return nil, fmt.Errorf("NewKey() failed: %w", err)
		}
		blockCipher, err := newAESGCMBlockCipher(cipherKey)
		if err != nil {
			return nil, fmt.Errorf("NewAESGCMBlockCipher() failed: %w", err)
		}
		blockCiphers = append(blockCiphers, blockCipher)
	}
	entry := cachedCiphers{
		cipherList: blockCiphers,
		createTime: t,
	}
	blockCipherCache.Store(pw, entry)
	return entry.cipherList, nil
}
