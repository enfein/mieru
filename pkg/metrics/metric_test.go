// Copyright (C) 2023  mieru authors
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

import "testing"

func TestMarshalJSON(t *testing.T) {
	list := MetricGroupList{}
	metricMap.Range(func(k, v any) bool {
		group := v.(*MetricGroup)
		if group.IsLoggingEnabled() {
			list = list.Append(group)
		}
		return true
	})
	s, err := list.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() failed: %v", err)
	}
	if len(s) == 0 {
		t.Errorf("MarshalJSON() returns empty response")
	}
}
