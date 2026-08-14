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

package protocol

import (
	"sync/atomic"

	"github.com/enfein/mieru/v3/pkg/metrics"
)

const (
	// SourceUserCacheMetricGroupName is the metrics group for server-side
	// source-to-user decryption cache activity.
	SourceUserCacheMetricGroupName         = "user_decrypt_cache"
	sourceUserCacheMetricGroup             = SourceUserCacheMetricGroupName
	sourceUserCacheLookupMetric            = "Lookups"
	sourceUserCacheSourceHitMetric         = "SourceHits"
	sourceUserCacheSourceMissMetric        = "SourceMisses"
	sourceUserCacheAuthenticationHitMetric = "AuthenticationHits"
	sourceUserCacheFullFallbackMetric      = "FullFallbacks"
	sourceUserCacheInsertionMetric         = "Insertions"
	sourceUserCacheExpiryMetric            = "Expiries"
	sourceUserCacheEvictionMetric          = "Evictions"
)

var registeredSourceUserCacheMetrics = sourceUserCacheMetrics{
	lookups:            metrics.RegisterMetric(sourceUserCacheMetricGroup, sourceUserCacheLookupMetric, metrics.COUNTER),
	sourceHits:         metrics.RegisterMetric(sourceUserCacheMetricGroup, sourceUserCacheSourceHitMetric, metrics.COUNTER),
	sourceMisses:       metrics.RegisterMetric(sourceUserCacheMetricGroup, sourceUserCacheSourceMissMetric, metrics.COUNTER),
	authenticationHits: metrics.RegisterMetric(sourceUserCacheMetricGroup, sourceUserCacheAuthenticationHitMetric, metrics.COUNTER),
	fullFallbacks:      metrics.RegisterMetric(sourceUserCacheMetricGroup, sourceUserCacheFullFallbackMetric, metrics.COUNTER),
	insertions:         metrics.RegisterMetric(sourceUserCacheMetricGroup, sourceUserCacheInsertionMetric, metrics.COUNTER),
	expiries:           metrics.RegisterMetric(sourceUserCacheMetricGroup, sourceUserCacheExpiryMetric, metrics.COUNTER),
	evictions:          metrics.RegisterMetric(sourceUserCacheMetricGroup, sourceUserCacheEvictionMetric, metrics.COUNTER),
}

// sourceUserCacheStats has Mux lifetime rather than generation lifetime. Hot
// paths update only these lock-free totals. Insertion, expiry, and eviction
// each count one completed cache-update event, even when replacing a source
// entry changes the reachability of multiple user slots. A live same-user
// refresh counts none of those transitions. published is used only by the Mux
// maintenance loop when it batches new deltas into the metrics registry.
type sourceUserCacheStats struct {
	lookups            atomic.Uint64
	sourceHits         atomic.Uint64
	sourceMisses       atomic.Uint64
	authenticationHits atomic.Uint64
	fullFallbacks      atomic.Uint64
	insertions         atomic.Uint64
	expiries           atomic.Uint64
	evictions          atomic.Uint64

	published sourceUserCachePublishedStats
}

type sourceUserCachePublishedStats struct {
	lookups            atomic.Uint64
	sourceHits         atomic.Uint64
	sourceMisses       atomic.Uint64
	authenticationHits atomic.Uint64
	fullFallbacks      atomic.Uint64
	insertions         atomic.Uint64
	expiries           atomic.Uint64
	evictions          atomic.Uint64
}

// sourceUserCacheStatsSnapshot is aggregate-only and never contains source or
// user identity. A snapshot is weakly consistent: each field is loaded
// atomically and is monotonic, but concurrent events can become visible in
// different fields at different times. Cross-field equalities therefore need
// not hold in a snapshot taken during active traffic.
type sourceUserCacheStatsSnapshot struct {
	lookups            uint64
	sourceHits         uint64
	sourceMisses       uint64
	authenticationHits uint64
	fullFallbacks      uint64
	insertions         uint64
	expiries           uint64
	evictions          uint64
}

type sourceUserCacheMetrics struct {
	lookups            metrics.Metric
	sourceHits         metrics.Metric
	sourceMisses       metrics.Metric
	authenticationHits metrics.Metric
	fullFallbacks      metrics.Metric
	insertions         metrics.Metric
	expiries           metrics.Metric
	evictions          metrics.Metric
}

func (s *sourceUserCacheStats) load() sourceUserCacheStatsSnapshot {
	if s == nil {
		return sourceUserCacheStatsSnapshot{}
	}
	return sourceUserCacheStatsSnapshot{
		lookups:            s.lookups.Load(),
		sourceHits:         s.sourceHits.Load(),
		sourceMisses:       s.sourceMisses.Load(),
		authenticationHits: s.authenticationHits.Load(),
		fullFallbacks:      s.fullFallbacks.Load(),
		insertions:         s.insertions.Load(),
		expiries:           s.expiries.Load(),
		evictions:          s.evictions.Load(),
	}
}

func (s *sourceUserCacheStats) flushTo(m sourceUserCacheMetrics) {
	if s == nil {
		return
	}
	flushSourceUserCacheCounter(&s.lookups, &s.published.lookups, m.lookups)
	flushSourceUserCacheCounter(&s.sourceHits, &s.published.sourceHits, m.sourceHits)
	flushSourceUserCacheCounter(&s.sourceMisses, &s.published.sourceMisses, m.sourceMisses)
	flushSourceUserCacheCounter(&s.authenticationHits, &s.published.authenticationHits, m.authenticationHits)
	flushSourceUserCacheCounter(&s.fullFallbacks, &s.published.fullFallbacks, m.fullFallbacks)
	flushSourceUserCacheCounter(&s.insertions, &s.published.insertions, m.insertions)
	flushSourceUserCacheCounter(&s.expiries, &s.published.expiries, m.expiries)
	flushSourceUserCacheCounter(&s.evictions, &s.published.evictions, m.evictions)
}

// flushSourceUserCacheCounter claims and publishes each delta exactly once,
// even if a test or a future maintenance path invokes flushing concurrently.
func flushSourceUserCacheCounter(total, published *atomic.Uint64, metric metrics.Metric) {
	if metric == nil {
		return
	}
	for {
		before := published.Load()
		now := total.Load()
		if now == before {
			return
		}
		if published.CompareAndSwap(before, now) {
			metric.Add(int64(now - before))
			return
		}
	}
}
