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

// Package pool provides typed buffer pools to reduce memory allocations.
package pool

import (
	"sync"
)

var (
	buf64kPool  = sync.Pool{New: func() any { b := make([]byte, 65536); return &b }}
	buf1kPool   = sync.Pool{New: func() any { b := make([]byte, 1024); return &b }}
	buf1500Pool = sync.Pool{New: func() any { b := make([]byte, 1500); return &b }}
	buf32Pool   = sync.Pool{New: func() any { b := make([]byte, 32); return &b }}
)

func GetBuf64k() []byte  { return *buf64kPool.Get().(*[]byte) }
func PutBuf64k(b []byte) { buf64kPool.Put(&b) }

func GetBuf1k() []byte  { return *buf1kPool.Get().(*[]byte) }
func PutBuf1k(b []byte) { buf1kPool.Put(&b) }

func GetBuf1500() []byte  { return *buf1500Pool.Get().(*[]byte) }
func PutBuf1500(b []byte) { buf1500Pool.Put(&b) }

func GetBuf32() []byte  { return *buf32Pool.Get().(*[]byte) }
func PutBuf32(b []byte) { buf32Pool.Put(&b) }
