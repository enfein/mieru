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

package serveruser

import (
	"bytes"
	"encoding/hex"
	"sort"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/enfein/mieru/v3/pkg/metrics"
)

type sourceUserCacheTestMetric struct {
	name  string
	value atomic.Int64
}

func (m *sourceUserCacheTestMetric) Name() string             { return m.name }
func (m *sourceUserCacheTestMetric) Type() metrics.MetricType { return metrics.COUNTER }
func (m *sourceUserCacheTestMetric) Add(delta int64) int64 {
	if delta < 0 {
		panic("negative counter delta")
	}
	return m.value.Add(delta)
}
func (m *sourceUserCacheTestMetric) Load() int64 { return m.value.Load() }
func (m *sourceUserCacheTestMetric) Store(int64) { panic("counter store") }

func newSourceUserCacheTestMetrics() sourceUserCacheMetrics {
	return sourceUserCacheMetrics{
		lookups:            &sourceUserCacheTestMetric{name: sourceUserCacheLookupMetric},
		sourceHits:         &sourceUserCacheTestMetric{name: sourceUserCacheSourceHitMetric},
		sourceMisses:       &sourceUserCacheTestMetric{name: sourceUserCacheSourceMissMetric},
		authenticationHits: &sourceUserCacheTestMetric{name: sourceUserCacheAuthenticationHitMetric},
		fullFallbacks:      &sourceUserCacheTestMetric{name: sourceUserCacheFullFallbackMetric},
		insertions:         &sourceUserCacheTestMetric{name: sourceUserCacheInsertionMetric},
		expiries:           &sourceUserCacheTestMetric{name: sourceUserCacheExpiryMetric},
		evictions:          &sourceUserCacheTestMetric{name: sourceUserCacheEvictionMetric},
	}
}

func TestSourceUserCacheStatsBatchingAndMonotonicity(t *testing.T) {
	stats := &sourceUserCacheStats{}
	metricSet := newSourceUserCacheTestMetrics()
	const (
		workers    = 8
		increments = 2000
	)

	stopFlush := make(chan struct{})
	flushDone := make(chan struct{})
	go func() {
		defer close(flushDone)
		for {
			select {
			case <-stopFlush:
				return
			default:
				stats.flushTo(metricSet)
			}
		}
	}()

	var wg sync.WaitGroup
	start := make(chan struct{})
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < increments; i++ {
				stats.lookups.Add(1)
				stats.sourceHits.Add(1)
				stats.sourceMisses.Add(1)
				stats.authenticationHits.Add(1)
				stats.fullFallbacks.Add(1)
				stats.insertions.Add(1)
				stats.expiries.Add(1)
				stats.evictions.Add(1)
			}
		}()
	}
	close(start)

	previous := stats.load()
	for stats.lookups.Load() < workers*increments {
		now := stats.load()
		if sourceUserCacheSnapshotLess(now, previous) {
			t.Fatalf("counter snapshot decreased: before=%+v after=%+v", previous, now)
		}
		previous = now
	}
	wg.Wait()
	close(stopFlush)
	<-flushDone
	stats.flushTo(metricSet)

	want := int64(workers * increments)
	for _, metric := range []metrics.Metric{
		metricSet.lookups,
		metricSet.sourceHits,
		metricSet.sourceMisses,
		metricSet.authenticationHits,
		metricSet.fullFallbacks,
		metricSet.insertions,
		metricSet.expiries,
		metricSet.evictions,
	} {
		if got := metric.Load(); got != want {
			t.Errorf("batched metric %s = %d, want %d", metric.Name(), got, want)
		}
	}

	// A flush with no new totals must not publish any duplicate deltas.
	stats.flushTo(metricSet)
	if got := metricSet.lookups.Load(); got != want {
		t.Fatalf("duplicate flush changed lookups to %d, want %d", got, want)
	}
}

func TestSourceUserCacheRetirementPreservesLateStats(t *testing.T) {
	registry := &Registry{}
	registry.SetUsers(rawUserMap("old-stats-user", "old-stats-password"))
	old := registry.users.Load()
	inFlightTable := old.cache.loadTable()
	key := sourceUserCacheTestKey(901)

	registry.SetUsers(rawUserMap("new-stats-user", "new-stats-password"))
	old.cache.recordAuthenticatedInTable(inFlightTable, key, 1)
	old.cache.recordAuthenticated(sourceUserCacheTestKey(902), 1)
	registry.users.Load().cache.recordAuthenticated(key, 1)

	snapshot := registry.stats.load()
	if snapshot.insertions != 2 {
		t.Fatalf("insertions across retirement = %d, want 2", snapshot.insertions)
	}
	metricSet := newSourceUserCacheTestMetrics()
	registry.stats.flushTo(metricSet)
	if got := metricSet.insertions.Load(); got != 2 {
		t.Fatalf("batched insertions across retirement = %d, want 2", got)
	}
}

func TestSourceUserCacheMetricsContainNoIdentityOrKeyMaterial(t *testing.T) {
	group := metrics.GetMetricGroupByName(sourceUserCacheMetricGroup)
	if group == nil {
		t.Fatalf("metric group %q is not registered", sourceUserCacheMetricGroup)
	}
	gotNames := make([]string, 0, len(group.GetAll()))
	for _, metric := range group.GetAll() {
		gotNames = append(gotNames, metric.Name())
	}
	sort.Strings(gotNames)
	wantNames := []string{
		sourceUserCacheAuthenticationHitMetric,
		sourceUserCacheEvictionMetric,
		sourceUserCacheExpiryMetric,
		sourceUserCacheFullFallbackMetric,
		sourceUserCacheInsertionMetric,
		sourceUserCacheLookupMetric,
		sourceUserCacheSourceHitMetric,
		sourceUserCacheSourceMissMetric,
	}
	sort.Strings(wantNames)
	if len(gotNames) != len(wantNames) {
		t.Fatalf("metric names = %v, want %v", gotNames, wantNames)
	}
	for i := range wantNames {
		if gotNames[i] != wantNames[i] {
			t.Fatalf("metric names = %v, want %v", gotNames, wantNames)
		}
	}

	output, err := (metrics.MetricGroupList{group}).MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() failed: %v", err)
	}
	prepared := cipher.HashPassword([]byte("private-password"), []byte("private-user"))
	for _, secret := range [][]byte{
		[]byte("192.0.2.123"),
		[]byte("private-user"),
		[]byte("private-password"),
		[]byte(hex.EncodeToString(prepared)),
		[]byte("private-cipher-material"),
	} {
		if bytes.Contains(output, secret) {
			t.Fatalf("metrics output contains private material %q: %s", secret, output)
		}
	}
}

func sourceUserCacheSnapshotLess(a, b sourceUserCacheStatsSnapshot) bool {
	return a.lookups < b.lookups ||
		a.sourceHits < b.sourceHits ||
		a.sourceMisses < b.sourceMisses ||
		a.authenticationHits < b.authenticationHits ||
		a.fullFallbacks < b.fullFallbacks ||
		a.insertions < b.insertions ||
		a.expiries < b.expiries ||
		a.evictions < b.evictions
}
