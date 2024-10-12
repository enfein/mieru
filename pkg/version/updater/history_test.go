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
	"path/filepath"
	"testing"
	"time"

	"github.com/enfein/mieru/v3/pkg/version/updater/updaterpb"
	"google.golang.org/protobuf/proto"
)

func TestHistoryLoadStore(t *testing.T) {
	h := NewHistory()

	r1 := &updaterpb.UpdateRecord{
		TimeUnix:        proto.Int64(1678886400),
		Version:         proto.String("1.0.0"),
		LatestVersion:   proto.String("1.1.0"),
		NewReleaseFound: proto.Bool(true),
	}
	r2 := &updaterpb.UpdateRecord{
		TimeUnix:        proto.Int64(1688886400),
		Version:         proto.String("1.1.0"),
		LatestVersion:   proto.String("1.1.0"),
		NewReleaseFound: proto.Bool(false),
	}
	r3 := &updaterpb.UpdateRecord{
		TimeUnix: proto.Int64(1698886400),
		Version:  proto.String("1.2.0"),
		Error:    proto.String("failed to check update"),
	}

	h.Insert(r1)
	h.Insert(r2)
	h.Insert(r3)

	h.Trim()
	if len(h.history.Records) != 1 {
		t.Fatalf("Got %d records, want %d", len(h.history.Records), 1)
	}
	if h.history.Records[0].GetTimeUnix() != 1698886400 {
		t.Errorf("Record time is %d, want %d", h.history.Records[0].GetTimeUnix(), 1698886400)
	}

	dir := t.TempDir()
	if err := h.StoreTo(filepath.Join(dir, "history.pb")); err != nil {
		t.Fatalf("StoreTo() failed: %v", err)
	}

	h2 := NewHistory()
	if err := h2.LoadFrom(filepath.Join(dir, "history.pb")); err != nil {
		t.Fatalf("LoadFrom() failed: %v", err)
	}

	if !proto.Equal(h.history, h2.history) {
		t.Errorf("History has changed.")
	}
}

func TestShouldCheckUpdate(t *testing.T) {
	tests := []struct {
		name     string
		records  []*updaterpb.UpdateRecord
		expected bool
	}{
		{
			name:     "empty history",
			records:  []*updaterpb.UpdateRecord{},
			expected: true,
		},
		{
			name: "last check failed more than 7 days ago",
			records: []*updaterpb.UpdateRecord{
				{
					TimeUnix: proto.Int64(time.Now().AddDate(0, 0, -8).Unix()),
					Error:    proto.String("failed to check update"),
				},
			},
			expected: true,
		},
		{
			name: "last check failed less than 7 days ago",
			records: []*updaterpb.UpdateRecord{
				{
					TimeUnix: proto.Int64(time.Now().AddDate(0, 0, -6).Unix()),
					Error:    proto.String("failed to check update"),
				},
			},
			expected: false,
		},
		{
			name: "last check found a new version more than 7 days ago",
			records: []*updaterpb.UpdateRecord{
				{
					TimeUnix:        proto.Int64(time.Now().AddDate(0, 0, -8).Unix()),
					NewReleaseFound: proto.Bool(true),
				},
			},
			expected: true,
		},
		{
			name: "last check found a new version less than 7 days ago",
			records: []*updaterpb.UpdateRecord{
				{
					TimeUnix:        proto.Int64(time.Now().AddDate(0, 0, -6).Unix()),
					NewReleaseFound: proto.Bool(true),
				},
			},
			expected: false,
		},
		{
			name: "last check didn't find a new version more than 30 days ago",
			records: []*updaterpb.UpdateRecord{
				{
					TimeUnix:        proto.Int64(time.Now().AddDate(0, 0, -31).Unix()),
					NewReleaseFound: proto.Bool(false),
				},
			},
			expected: true,
		},
		{
			name: "last check didn't find a new version less than 30 days ago",
			records: []*updaterpb.UpdateRecord{
				{
					TimeUnix:        proto.Int64(time.Now().AddDate(0, 0, -29).Unix()),
					NewReleaseFound: proto.Bool(false),
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHistory()
			h.history.Records = tt.records
			got := h.ShouldCheckUpdate()
			if got != tt.expected {
				t.Errorf("ShouldCheckUpdate() = %v, want %v", got, tt.expected)
			}
		})
	}
}
