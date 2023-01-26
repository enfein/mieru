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

import (
	"fmt"
	"regexp"
	"strconv"
)

var (
	versionFmt = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)
	tagFmt     = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)
)

// Version defines the components of version number.
type Version struct {
	Major int
	Minor int
	Patch int
}

// String returns the string representation of version number, e.g. "1.0.0".
func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// ToTag returns the associated tag name of the version, e.g. "v1.0.0".
func (v Version) ToTag() string {
	return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// LessThan returns true if the current version is less than the compared version.
func (v Version) LessThan(another Version) bool {
	if v.Major > another.Major {
		return false
	}
	if v.Major < another.Major {
		return true
	}

	if v.Minor > another.Minor {
		return false
	}
	if v.Minor < another.Minor {
		return true
	}

	if v.Patch >= another.Patch {
		return false
	}
	return true
}

// Parse constructs the version from a string.
func Parse(s string) (Version, error) {
	matches := versionFmt.FindStringSubmatch(s)
	if len(matches) != 4 {
		return Version{}, fmt.Errorf("failed to parse version %s", s)
	}
	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	patch, _ := strconv.Atoi(matches[3])
	return Version{
		Major: major,
		Minor: minor,
		Patch: patch,
	}, nil
}

// ParseTag constructs the version from a tag name.
func ParseTag(tag string) (Version, error) {
	matches := tagFmt.FindStringSubmatch(tag)
	if len(matches) != 4 {
		return Version{}, fmt.Errorf("failed to parse tag %s", tag)
	}
	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	patch, _ := strconv.Atoi(matches[3])
	return Version{
		Major: major,
		Minor: minor,
		Patch: patch,
	}, nil
}
