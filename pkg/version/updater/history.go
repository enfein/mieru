// Copyright (C) 2024  mieru authors
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

package updater

import (
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/enfein/mieru/v3/pkg/version/updater/updaterpb"
	"google.golang.org/protobuf/proto"
)

// History operates updater's history.
type History struct {
	history *updaterpb.UpdateHistory
	mu      sync.Mutex
}

// NewHistory returns a new History object.
func NewHistory() *History {
	return &History{
		history: &updaterpb.UpdateHistory{},
	}
}

// LoadFrom loads history from a protobuf file.
func (h *History) LoadFrom(filePath string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	b, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("os.ReadFile() failed: %w", err)
	}
	uh := &updaterpb.UpdateHistory{}
	if err := proto.Unmarshal(b, uh); err != nil {
		return fmt.Errorf("proto.Unmarshal() failed: %w", err)
	}
	h.history = uh
	return nil
}

// StoreTo stores history to a protobuf file.
func (h *History) StoreTo(filePath string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	b, err := proto.Marshal(h.history)
	if err != nil {
		return fmt.Errorf("proto.Marshal() failed: %w", err)
	}
	if err := os.WriteFile(filePath, b, 0664); err != nil {
		return fmt.Errorf("os.WriteFile() failed: %w", err)
	}
	return nil
}

// Insert adds a new record to the history.
func (h *History) Insert(record *updaterpb.UpdateRecord) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.history.Records = append(h.history.Records, record)
	h.sort()
}

// Trim removes useless old history.
func (h *History) Trim() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.history.Records) > 1 {
		h.sort()
		h.history.Records = []*updaterpb.UpdateRecord{h.history.Records[0]}
	}
}

// ShouldCheckUpdate returns true if it is a good time to check update now.
func (h *History) ShouldCheckUpdate() bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Never check update before.
	if len(h.history.Records) == 0 {
		return true
	}

	h.sort()
	lastCheck := h.history.Records[0]

	// Last check failed for more than 7 days ago.
	if lastCheck.GetError() != "" {
		if time.Unix(lastCheck.GetTimeUnix(), 0).AddDate(0, 0, 7).Before(time.Now()) {
			return true
		}
	} else {
		// Last check found a new version more than 7 days ago.
		if lastCheck.GetNewReleaseFound() && time.Unix(lastCheck.GetTimeUnix(), 0).AddDate(0, 0, 7).Before(time.Now()) {
			return true
		}
		// Last check didn't find a new version more than 30 days ago.
		if !lastCheck.GetNewReleaseFound() && time.Unix(lastCheck.GetTimeUnix(), 0).AddDate(0, 0, 30).Before(time.Now()) {
			return true
		}
	}

	return false
}

func (h *History) sort() {
	sort.SliceStable(h.history.Records, func(i, j int) bool {
		return h.history.Records[i].GetTimeUnix() > h.history.Records[j].GetTimeUnix()
	})
}
