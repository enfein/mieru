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

package metrics

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMetricsDump(t *testing.T) {
	dumpPath := filepath.Join(t.TempDir(), "metrics.pb")
	SetMetricsDumpFilePath(dumpPath)
	if err := EnableMetricsDump(); err != nil {
		t.Fatalf("EnableMetricsDump(): %v", err)
	}
	DisableMetricsDump()

	if err := DumpMetricsNow(); err != nil {
		t.Fatalf("DumpMetricsNow(): %v", err)
	}
	if _, err := os.Stat(dumpPath); errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Dump file %s doesn't exist.", dumpPath)
	}
	if err := LoadMetricsFromDump(); err != nil {
		t.Fatalf("LoadMetricsFromDump(): %v", err)
	}
}
