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
	"time"

	pb "github.com/enfein/mieru/v3/pkg/metrics/metricspb"
	"google.golang.org/protobuf/proto"
)

func TestEnableAndDisableLogging(t *testing.T) {
	if err := SetLoggingDuration(10 * time.Millisecond); err != nil {
		t.Fatalf("SetLoggingDuration() failed: %v", err)
	}
	EnableLogging()
	time.Sleep(50 * time.Millisecond) // Allow the log metrics loop to run.
	DisableLogging()

	// Disable logging is idempotent.
	DisableLogging()
	DisableLogging()

	// Can enable and disable logging again.
	EnableLogging()
	DisableLogging()
}

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

func TestMetricPBConvertion(t *testing.T) {
	p := &pb.Metric{
		Name:  proto.String("counter"),
		Type:  pb.MetricType_COUNTER_TIME_SERIES.Enum(),
		Value: proto.Int64(100),
		History: []*pb.History{
			{
				TimeUnixMilli: proto.Int64(time.Now().UnixMilli()),
				Delta:         proto.Int64(100),
			},
		},
	}
	c, err := NewCounterFromMetricPB(p)
	if err != nil {
		t.Fatalf("NewCounterFromMetricPB() failed: %v", err)
	}
	p2 := ToMetricPB(c)
	if !proto.Equal(p, p2) {
		t.Errorf("metric protobuf doesn't match")
	}
}
