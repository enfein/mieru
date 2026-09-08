// Copyright (C) 2026  mieru authors
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

//go:build !windows

package metrics

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/enfein/mieru/v3/pkg/metrics/metricspb"
	"google.golang.org/protobuf/proto"
)

type checkpointPauseMetric struct {
	value    atomic.Int64
	loads    atomic.Int64
	captured chan struct{}
	release  chan struct{}
}

func (*checkpointPauseMetric) Name() string        { return "bytes" }
func (*checkpointPauseMetric) Type() MetricType    { return COUNTER }
func (m *checkpointPauseMetric) Add(n int64) int64 { return m.value.Add(n) }
func (m *checkpointPauseMetric) Store(n int64)     { m.value.Store(n) }
func (m *checkpointPauseMetric) Load() int64 {
	n := m.value.Load()
	if m.loads.Add(1) == 1 {
		close(m.captured)
		<-m.release
	}
	return n
}

func installCheckpointPauseMetric(t *testing.T) (*checkpointPauseMetric, func()) {
	t.Helper()
	m := &checkpointPauseMetric{captured: make(chan struct{}), release: make(chan struct{})}
	g := &MetricGroup{name: "checkpoint-concurrency-test"}
	g.metrics.Store(m.Name(), m)
	metricMap.Store(g.name, g)
	var once sync.Once
	release := func() { once.Do(func() { close(m.release) }) }
	t.Cleanup(func() {
		release()
		metricMap.Delete(g.name)
	})
	return m, release
}

func waitForCheckpointPause(t *testing.T, m *checkpointPauseMetric) {
	t.Helper()
	select {
	case <-m.captured:
	case <-time.After(5 * time.Second):
		t.Fatal("checkpoint did not reach collection barrier")
	}
}

func setCheckpointPath(t *testing.T, path string) {
	t.Helper()
	logMutex.Lock()
	previous := metricsDumpFilePath
	logMutex.Unlock()
	SetMetricsDumpFilePath(path)
	t.Cleanup(func() { SetMetricsDumpFilePath(previous) })
}

func TestLoadWaitsForCheckpointPublication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.pb")
	setCheckpointPath(t, path)
	empty, err := proto.Marshal(&pb.AllMetrics{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, empty, 0600); err != nil {
		t.Fatal(err)
	}

	m, release := installCheckpointPauseMetric(t)
	dumpDone := make(chan error, 1)
	go func() { dumpDone <- DumpMetricsNow() }()
	waitForCheckpointPause(t, m)

	loadDone := make(chan error, 1)
	go func() { loadDone <- LoadMetricsFromDump() }()
	select {
	case err := <-loadDone:
		t.Fatalf("load completed while checkpoint publication was paused: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	release()
	for name, done := range map[string]chan error{"dump": dumpDone, "load": loadDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s failed: %v", name, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s did not finish", name)
		}
	}
}

func TestCheckpointSerializesCollectionThroughPublication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.pb")
	setCheckpointPath(t, path)
	m, release := installCheckpointPauseMetric(t)
	m.value.Store(100)

	firstDone := make(chan error, 1)
	go func() { firstDone <- DumpMetricsNow() }()
	waitForCheckpointPause(t, m)
	m.value.Store(200)

	secondDone := make(chan error, 1)
	go func() { secondDone <- DumpMetricsNow() }()
	select {
	case err := <-secondDone:
		release()
		<-firstDone
		t.Fatalf("newer checkpoint completed while older collection was paused: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	release()
	for name, done := range map[string]chan error{"first": firstDone, "second": secondDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s checkpoint failed: %v", name, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s checkpoint did not finish", name)
		}
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &pb.AllMetrics{}
	if err := proto.Unmarshal(b, snapshot); err != nil {
		t.Fatal(err)
	}
	for _, group := range snapshot.GetGroups() {
		if group.GetName() != "checkpoint-concurrency-test" {
			continue
		}
		for _, metric := range group.GetMetrics() {
			if metric.GetName() == m.Name() {
				if metric.GetValue() != 200 {
					t.Fatalf("persisted %d, want 200", metric.GetValue())
				}
				return
			}
		}
	}
	t.Fatal("checkpoint test metric missing")
}

func TestCheckpointPublishesToPathCapturedBeforeCollection(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pb")
	second := filepath.Join(dir, "second.pb")
	setCheckpointPath(t, first)

	m, release := installCheckpointPauseMetric(t)
	done := make(chan error, 1)
	go func() { done <- DumpMetricsNow() }()
	waitForCheckpointPause(t, m)
	SetMetricsDumpFilePath(second)
	release()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("checkpoint did not finish")
	}
	if _, err := os.Stat(first); err != nil {
		t.Fatalf("checkpoint was not published to captured path: %v", err)
	}
	if _, err := os.Stat(second); !os.IsNotExist(err) {
		t.Fatalf("checkpoint followed a concurrent path change: %v", err)
	}
}
