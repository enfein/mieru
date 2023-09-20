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

package cipher

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

const (
	// defaultIter is the default number of iterations to generate the key.
	// This is part of mieru protocol. This value should not be changed.
	defaultIter = 4096

	// refreshInterval is the amount of time when the salt used to generate cipher block is changed.
	// This is part of mieru protocol. This value should not be changed.
	refreshInterval = 1 * time.Minute
)

// pbkdf2Gen implements KeyGenerator with PBKDF2 algorithm.
type pbkdf2Gen struct {
	Salt []byte
	Iter int
}

// NewKey creates a new key from the given password.
func (g *pbkdf2Gen) NewKey(password []byte, keyLen int) ([]byte, error) {
	if len(password) == 0 {
		return nil, fmt.Errorf("password is empty")
	}
	return pbkdf2.Key(password, g.Salt, g.Iter, keyLen, sha256.New), nil
}

// saltFromTime generate three salts (each 32 bytes) based on the time.
func saltFromTime(t time.Time) [][]byte {
	var times []time.Time
	rounded := t.Round(refreshInterval)
	times = append(times, rounded.Add(-refreshInterval))
	times = append(times, rounded)
	times = append(times, rounded.Add(refreshInterval))

	b := make([]byte, 8) // 64 bits
	var salts [][]byte

	for _, t := range times {
		binary.BigEndian.PutUint64(b, uint64(t.Unix()))
		sha := sha256.Sum256(b)
		salts = append(salts, sha[:])
	}

	return salts
}
