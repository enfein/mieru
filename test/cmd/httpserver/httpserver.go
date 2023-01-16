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

// httpserver is a simple HTTP server listening on port 8080.
// There are 100 pre-filled data blocks: 90 blocks have 64 KiB data, 10 blocks have 1 MiB data.
// If flag "-huge" is set, there is a single block with 256 MiB data.
// When a request comes in, a random block is selected, and the block as well as its SHA-1 value
// are returned to the client. The SHA-1 value is provided in "X-SHA1" header.
package main

import (
	crand "crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"flag"
	mrand "math/rand"
	"net/http"
	"time"

	"github.com/enfein/mieru/pkg/log"
)

var (
	data         = make(map[int][]byte)
	sha1CheckSum = make(map[int][]byte)
)

var huge = flag.Bool("huge", false, "Generate huge data size: 256 MiB")

const (
	// Data size in bytes.
	smallDataSize = 64 * 1024
	largeDataSize = 1 * 1024 * 1024
	hugeDataSize  = 256 * 1024 * 1024

	// Range of each category.
	smallUpperRange = 90
)

func fillData() {
	if *huge {
		data[0] = make([]byte, hugeDataSize)
		n, err := crand.Read(data[0])
		if err != nil {
			log.Fatalf("failed to generate random data: %v", err)
		}
		checkSum := sha1.Sum(data[0])
		sha1CheckSum[0] = checkSum[:]
		log.Infof("generated %d bytes with SHA-1 %v", n, hex.EncodeToString(sha1CheckSum[0]))
		return
	}
	for i := 0; i < 100; i++ {
		if i < smallUpperRange {
			data[i] = make([]byte, smallDataSize)
		} else {
			data[i] = make([]byte, largeDataSize)
		}
		n, err := crand.Read(data[i])
		if err != nil {
			log.Fatalf("failed to generate random data: %v", err)
		}
		checkSum := sha1.Sum(data[i])
		sha1CheckSum[i] = checkSum[:]
		log.Infof("generated %d bytes at position %d with SHA-1 %v", n, i, hex.EncodeToString(sha1CheckSum[i]))
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	partition := 0
	if !*huge {
		partition = mrand.Intn(100)
	}
	sha1 := hex.EncodeToString(sha1CheckSum[partition])
	w.Header().Add("X-SHA1", sha1)
	w.Write(data[partition])
	log.Debugf("HTTP server returned %d bytes at position %d with SHA-1 checksum %s",
		len(data[partition]), partition, sha1)
}

func main() {
	flag.Parse()
	log.SetFormatter(&log.DaemonFormatter{})
	mrand.Seed(time.Now().UnixNano())
	fillData()
	log.Infof("HTTP server data initialized.")
	http.HandleFunc("/", handler)
	http.ListenAndServe(":8080", nil)
}
