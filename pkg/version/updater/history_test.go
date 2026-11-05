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
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
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
					TimeUnix: proto.Int64(now.AddDate(0, 0, -8).Unix()),
					Error:    proto.String("failed to check update"),
				},
			},
			expected: true,
		},
		{
			name: "last check failed less than 7 days ago",
			records: []*updaterpb.UpdateRecord{
				{
					TimeUnix: proto.Int64(now.AddDate(0, 0, -6).Unix()),
					Error:    proto.String("failed to check update"),
				},
			},
			expected: false,
		},
		{
			name: "last check found a new version more than 7 days ago",
			records: []*updaterpb.UpdateRecord{
				{
					TimeUnix:        proto.Int64(now.AddDate(0, 0, -8).Unix()),
					NewReleaseFound: proto.Bool(true),
				},
			},
			expected: true,
		},
		{
			name: "last check found a new version less than 7 days ago",
			records: []*updaterpb.UpdateRecord{
				{
					TimeUnix:        proto.Int64(now.AddDate(0, 0, -6).Unix()),
					NewReleaseFound: proto.Bool(true),
				},
			},
			expected: false,
		},
		{
			name: "last check didn't find a new version more than 30 days ago",
			records: []*updaterpb.UpdateRecord{
				{
					TimeUnix:        proto.Int64(now.AddDate(0, 0, -31).Unix()),
					NewReleaseFound: proto.Bool(false),
				},
			},
			expected: true,
		},
		{
			name: "last check didn't find a new version less than 30 days ago",
			records: []*updaterpb.UpdateRecord{
				{
					TimeUnix:        proto.Int64(now.AddDate(0, 0, -29).Unix()),
					NewReleaseFound: proto.Bool(false),
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHistory()
			h.clock.SetTime(now)
			h.history.Records = tt.records
			got := h.ShouldCheckUpdate()
			if got != tt.expected {
				t.Errorf("ShouldCheckUpdate() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestShouldCheckUpdateDeadline(t *testing.T) {
	for _, test := range []struct {
		name       string
		days       int
		err        string
		newRelease bool
	}{
		{name: "failed check", days: 7, err: "failed to check update"},
		{name: "new release", days: 7, newRelease: true},
		{name: "no new release", days: 30},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := NewHistory()
			lastCheck := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
			h.Insert(&updaterpb.UpdateRecord{
				TimeUnix:        proto.Int64(lastCheck.Unix()),
				Error:           proto.String(test.err),
				NewReleaseFound: proto.Bool(test.newRelease),
			})
			h.clock.SetTime(lastCheck)
			h.clock.Advance(lastCheck.AddDate(0, 0, test.days).Sub(lastCheck) - time.Nanosecond)
			for _, step := range []struct {
				name    string
				advance time.Duration
				want    bool
			}{
				{name: "before deadline"},
				{name: "at deadline", advance: time.Nanosecond},
				{name: "after deadline", advance: time.Nanosecond, want: true},
			} {
				h.clock.Advance(step.advance)
				if got := h.ShouldCheckUpdate(); got != step.want {
					t.Fatalf("%s: ShouldCheckUpdate() = %v, want %v", step.name, got, step.want)
				}
			}
		})
	}
}
