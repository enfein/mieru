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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// CheckUpdate fetches the latest mieru / mita version using GitHub API
// and check if a new release is available.
func CheckUpdate() (hasUpdate bool, msg string, err error) {
	var remoteTag string
	remoteTag, err = queryLatestVersion()
	if err != nil {
		return false, "", fmt.Errorf("queryLatestVersion() failed: %w", err)
	}

	var currentVersion Version
	var remoteVersion Version
	currentVersion, err = Parse(AppVersion)
	if err != nil {
		return false, "", fmt.Errorf("Parse() failed: %w", err)
	}
	remoteVersion, err = ParseTag(remoteTag)
	if err != nil {
		return false, "", fmt.Errorf("ParseTag() failed: %w", err)
	}

	if currentVersion.LessThan(remoteVersion) {
		return true, fmt.Sprintf("update is available at %s", "https://github.com/enfein/mieru/releases/tag/"+remoteTag), nil
	} else {
		return false, "already up to date", nil
	}
}

func queryLatestVersion() (string, error) {
	resp, err := http.Get("https://api.github.com/repos/enfein/mieru/releases/latest")
	if err != nil {
		return "", fmt.Errorf("http.Get() failed: %v [GitHub may be blocked in your network]", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP returned expected status code %d", resp.StatusCode)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("io.ReadAll() failed: %v", err)
	}
	var release latestRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return "", fmt.Errorf("json.Unmarshal() failed: %v", err)
	}
	return release.TagName, nil
}

type latestRelease struct {
	TagName string `json:"tag_name"`
}
