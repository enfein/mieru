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
	mrand "math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sync"
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
	if runtime.GOOS == "windows" {
		t.Skip("metrics dumping is not supported on Windows")
	}

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

func TestToMetricPBConcurrent(t *testing.T) {
	counter := &Counter{name: "counter", timeSeries: true}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Goroutine to continuously add history.
	wg.Add(1)
	go func() {
		defer wg.Done()
		currentTime := time.Now()
		for {
			select {
			case <-stop:
				return
			default:
				// Add a small random duration to the time to ensure it's always increasing.
				currentTime = currentTime.Add(time.Duration(mrand.Intn(100)+1) * time.Millisecond)
				counter.addWithTime(int64(mrand.Intn(100)), currentTime)
				// Run the for loop enough times to trigger rollup.
				time.Sleep(time.Microsecond)
			}
		}
	}()

	// Goroutine to verify the counter value matches the sum of history.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				p := ToMetricPB(counter)
				var sum int64
				for _, h := range p.GetHistory() {
					sum += h.GetDelta()
				}
				if p.GetValue() != sum {
					t.Errorf("sum of history deltas (%d) does not match counter value (%d)", sum, p.GetValue())
				}
				time.Sleep(time.Duration(mrand.Intn(100)) * time.Microsecond)
			}
		}
	}()

	time.Sleep(time.Second)
	close(stop)
	wg.Wait()
}

func TestMetricPBConvertion(t *testing.T) {
	testCases := []struct {
		name     string
		metricPB *pb.Metric
	}{
		{
			name: "CounterTimeSeries",
			metricPB: &pb.Metric{
				Name:  proto.String("counter_ts"),
				Type:  pb.MetricType_COUNTER_TIME_SERIES.Enum(),
				Value: proto.Int64(100),
				History: []*pb.History{
					{
						TimeUnixMilli: proto.Int64(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()),
						Delta:         proto.Int64(100),
						RollUp:        pb.RollUpLabel_ROLL_UP_TO_DAY.Enum(),
					},
				},
			},
		},
		{
			name: "Counter",
			metricPB: &pb.Metric{
				Name:  proto.String("counter"),
				Type:  pb.MetricType_COUNTER.Enum(),
				Value: proto.Int64(200),
			},
		},
		{
			name: "Gauge",
			metricPB: &pb.Metric{
				Name:  proto.String("gauge"),
				Type:  pb.MetricType_GAUGE.Enum(),
				Value: proto.Int64(300),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := FromMetricPB(tc.metricPB)
			if err != nil {
				t.Fatalf("FromMetricPB() failed: %v", err)
			}
			p2 := ToMetricPB(m)

			if !proto.Equal(tc.metricPB, p2) {
				t.Errorf("metric protobuf doesn't match.\nwant: %v\ngot: %v", tc.metricPB, p2)
			}
		})
	}
}

func TestLoadLegacyHistory(t *testing.T) {
	base := time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC)
	ms := func(d time.Duration) *int64 { return proto.Int64(base.Add(d).UnixMilli()) }
	src := &pb.Metric{
		Name:  proto.String("counter_ts"),
		Type:  pb.MetricType_COUNTER_TIME_SERIES.Enum(),
		Value: proto.Int64(31),
		History: []*pb.History{
			{TimeUnixMilli: ms(-10 * 24 * time.Hour), Delta: proto.Int64(1), RollUp: pb.RollUpLabel_ROLL_UP_TO_DAY.Enum()},
			{TimeUnixMilli: ms(-3 * time.Hour), Delta: proto.Int64(2), RollUp: pb.RollUpLabel_ROLL_UP_TO_HOUR.Enum()},
			{TimeUnixMilli: ms(-10 * time.Minute), Delta: proto.Int64(4), RollUp: pb.RollUpLabel_ROLL_UP_TO_MINUTE.Enum()},
			{TimeUnixMilli: ms(-10 * time.Second), Delta: proto.Int64(8), RollUp: pb.RollUpLabel_ROLL_UP_TO_SECOND.Enum()},
			{TimeUnixMilli: ms(-1500 * time.Millisecond), Delta: proto.Int64(16), RollUp: pb.RollUpLabel_NO_ROLL_UP.Enum()},
		},
	}
	m, err := FromMetricPB(src)
	if err != nil {
		t.Fatalf("FromMetricPB() failed: %v", err)
	}
	c := m.(*Counter)
	if got := c.DeltaBetween(base.Add(-time.Hour), base); got != 4+8+16 {
		t.Errorf("DeltaBetween() = %d, want %d", got, 4+8+16)
	}
	if got, want := c.LastUpdateTime(), base.Add(-1500*time.Millisecond); !got.Equal(want) {
		t.Errorf("LastUpdateTime() = %v, want %v", got, want)
	}
	p := ToMetricPB(c)
	var sum int64
	for i, h := range p.GetHistory() {
		sum += h.GetDelta()
		if h.GetRollUp() == pb.RollUpLabel_NO_ROLL_UP {
			t.Errorf("history %d is not rolled up", i)
		}
		if i > 0 && h.GetTimeUnixMilli() < p.GetHistory()[i-1].GetTimeUnixMilli() {
			t.Errorf("history %d is not ordered by time", i)
		}
	}
	if sum != p.GetValue() || sum != 31 {
		t.Errorf("history sum = %d, value = %d, want 31", sum, p.GetValue())
	}
}
