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

package updater

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/pkg/socks5"
	"github.com/enfein/mieru/v3/pkg/version"
	"github.com/enfein/mieru/v3/pkg/version/updater/updaterpb"
	"google.golang.org/protobuf/proto"
)

const (
	UpToDateMessage = "already up to date"
)

// CheckUpdate fetches the latest mieru / mita version using GitHub API
// and check if a new release is available.
// If the proxy URI is not empty, that is used by the HTTP client when
// sending the HTTP request.
func CheckUpdate(socks5ProxyURI string) (record *updaterpb.UpdateRecord, msg string, err error) {
	record = &updaterpb.UpdateRecord{
		TimeUnix: proto.Int64(time.Now().Unix()),
		Version:  proto.String(version.AppVersion),
	}

	var remoteTag string
	remoteTag, err = queryLatestVersion(socks5ProxyURI)
	if err != nil {
		err = fmt.Errorf("queryLatestVersion() failed: %w", err)
		record.Error = proto.String(err.Error())
		return
	}

	var currentVersion version.Version
	var remoteVersion version.Version
	currentVersion, err = version.Parse(version.AppVersion)
	if err != nil {
		err = fmt.Errorf("parse application version failed: %w", err)
		record.Error = proto.String(err.Error())
		return
	}
	remoteVersion, err = version.ParseTag(remoteTag)
	if err != nil {
		err = fmt.Errorf("parse release tag failed: %w", err)
		record.Error = proto.String(err.Error())
		return
	}
	record.LatestVersion = proto.String(remoteVersion.String())

	if currentVersion.IsLessThan(remoteVersion) {
		record.NewReleaseFound = proto.Bool(true)
		msg = fmt.Sprintf("update is available at %s", "https://github.com/enfein/mieru/releases/tag/"+remoteTag)
		return
	} else {
		record.NewReleaseFound = proto.Bool(false)
		msg = UpToDateMessage
		return
	}
}

func queryLatestVersion(socks5ProxyURI string) (string, error) {
	httpClient := http.Client{
		Timeout: 10 * time.Second,
	}
	if socks5ProxyURI != "" {
		httpClient.Transport = &http.Transport{
			Dial: socks5.Dial(socks5ProxyURI, constant.Socks5ConnectCmd),
		}
	}
	resp, err := httpClient.Get("https://api.github.com/repos/enfein/mieru/releases/latest")
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
