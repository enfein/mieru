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

package version

import "testing"

func TestAppVersion(t *testing.T) {
	v, err := Parse(AppVersion)
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}
	str := v.String()
	if str != AppVersion {
		t.Errorf("version string = %s, want %s", str, AppVersion)
	}

	tag := "v" + AppVersion
	v, err = ParseTag(tag)
	if err != nil {
		t.Fatalf("ParseTag() failed: %v", err)
	}
	tagStr := v.ToTag()
	if tagStr != tag {
		t.Errorf("tag = %s, want %s", tagStr, tag)
	}
}

func TestLessThan(t *testing.T) {
	testcases := []struct {
		my      Version
		another Version
		isLess  bool
	}{
		{
			Version{1, 2, 3},
			Version{1, 2, 3},
			false,
		},
		{
			Version{1, 2, 3},
			Version{2, 3, 4},
			true,
		},
		{
			Version{2, 3, 4},
			Version{1, 2, 3},
			false,
		},
		{
			Version{1, 2, 3},
			Version{1, 3, 2},
			true,
		},
		{
			Version{1, 3, 2},
			Version{1, 2, 3},
			false,
		},
		{
			Version{1, 2, 3},
			Version{1, 2, 4},
			true,
		},
		{
			Version{1, 2, 4},
			Version{1, 2, 3},
			false,
		},
	}

	for _, tc := range testcases {
		res := tc.my.LessThan(tc.another)
		if res != tc.isLess {
			t.Errorf("%s less than %s got %v, want %v", tc.my.String(), tc.another.String(), res, tc.isLess)
		}
	}
}
