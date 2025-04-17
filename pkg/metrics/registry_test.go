// Copyright (C) 2025  mieru authors
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
	"fmt"
	"testing"
)

func TestGetMetricsForUser(t *testing.T) {
	RegisterMetric(fmt.Sprintf(UserMetricGroupFormat, "red"), UserMetricDownloadBytes, COUNTER_TIME_SERIES)
	RegisterMetric(fmt.Sprintf(UserMetricGroupFormat, "red"), UserMetricUploadBytes, COUNTER_TIME_SERIES)
	RegisterMetric(fmt.Sprintf(UserMetricGroupFormat, "blue"), UserMetricDownloadBytes, COUNTER_TIME_SERIES)
	RegisterMetric(fmt.Sprintf(UserMetricGroupFormat, "blue"), UserMetricUploadBytes, COUNTER_TIME_SERIES)

	redMetrics := GetMetricsForUser("red")
	if len(redMetrics) != 2 {
		t.Errorf("got %d metrics for user red, want %d", len(redMetrics), 2)
	}
	blueMetrics := GetMetricsForUser("blue")
	if len(blueMetrics) != 2 {
		t.Errorf("got %d metrics for user blue, want %d", len(blueMetrics), 2)
	}
	pinkMetrics := GetMetricsForUser("pink")
	if len(pinkMetrics) != 0 {
		t.Errorf("got %d metrics for user pink, want %d", len(pinkMetrics), 0)
	}
}
