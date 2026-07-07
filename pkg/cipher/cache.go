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
	mrand "math/rand"
	"sync"
	"sync/atomic"
	"time"
)

const (
	cacheValidInterval    = KeyRefreshInterval / 4
	cacheValidMaxJitterMs = 5000
	cacheMaxEntries       = 65536
)

type cachedCiphers struct {
	cipherList []BlockCipher
	createTime time.Time
}

var blockCipherCache = sync.Map{}
var cacheEntryCount atomic.Int32

// getBlockCipherList returns three BlockCipher.
// It uses cache so it doesn't need to generate BlockCipher each time.
func getBlockCipherList(password []byte, stateless bool) ([]BlockCipher, error) {
	pw := string(password)

	// Try to find []BlockCipher from cache.
	c, ok := blockCipherCache.Load(pw)
	if ok {
		// Check if the cached entry is expired.
		jitter := time.Duration(mrand.Intn(cacheValidMaxJitterMs)) * time.Millisecond
		if c.(cachedCiphers).createTime.Add(cacheValidInterval - jitter).Before(time.Now()) {
			blockCipherCache.Delete(pw)
			cacheEntryCount.Add(-1)
			ok = false
		}
	}
	if ok {
		if stateless {
			return c.(cachedCiphers).cipherList, nil
		} else {
			blocks := CloneBlockCiphers(c.(cachedCiphers).cipherList)
			for i := 0; i < len(blocks); i++ {
				blocks[i].SetImplicitNonceMode(true)
			}
			return blocks, nil
		}
	}

	// If not found, generate the stateless []BlockCipher.
	blockCiphers, t, err := newBlockCipherList(password, true)
	if err != nil {
		return nil, fmt.Errorf("newBlockCipherList() failed: %v", err)
	}

	// Evict oldest if at capacity.
	if cacheEntryCount.Load() >= cacheMaxEntries {
		var oldestKey string
		var oldestTime time.Time
		first := true
		blockCipherCache.Range(func(k, v any) bool {
			if first || v.(cachedCiphers).createTime.Before(oldestTime) {
				oldestKey = k.(string)
				oldestTime = v.(cachedCiphers).createTime
				first = false
			}
			return true
		})
		if oldestKey != "" {
			blockCipherCache.Delete(oldestKey)
			cacheEntryCount.Add(-1)
		}
	}

	// Insert to cache.
	entry := cachedCiphers{
		cipherList: blockCiphers,
		createTime: t,
	}
	blockCipherCache.Store(pw, entry)
	cacheEntryCount.Add(1)

	if stateless {
		return blockCiphers, nil
	}

	// Set cipher to stateful if needed.
	blocks := CloneBlockCiphers(blockCiphers)
	for i := 0; i < len(blocks); i++ {
		blocks[i].SetImplicitNonceMode(true)
	}
	return blocks, nil
}

func newBlockCipherList(password []byte, stateless bool) ([]BlockCipher, time.Time, error) {
	t := time.Now()
	salts := saltFromTime(t)
	blockCiphers := make([]BlockCipher, 0, 3)
	for i := 0; i < 3; i++ {
		keygen := pbkdf2Gen{
			Salt: salts[i],
			Iter: KeyIter,
		}
		cipherKey, err := keygen.NewKey(password, DefaultKeyLen)
		if err != nil {
			return nil, t, fmt.Errorf("NewKey() failed: %w", err)
		}
		blockCipher, err := newXChaCha20Poly1305BlockCipher(cipherKey)
		if err != nil {
			return nil, t, fmt.Errorf("newXChaCha20Poly1305BlockCipher() failed: %w", err)
		}
		if !stateless {
			blockCipher.SetImplicitNonceMode(true)
		}
		blockCiphers = append(blockCiphers, blockCipher)
	}
	return blockCiphers, t, nil
}
