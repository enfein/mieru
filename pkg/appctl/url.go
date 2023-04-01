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

package appctl

import (
	"encoding/base64"
	"fmt"
	"net/url"

	pb "github.com/enfein/mieru/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/pkg/stderror"
	"google.golang.org/protobuf/proto"
)

// ClientConfigToURL creates a URL to share the client configuration.
func ClientConfigToURL(config *pb.ClientConfig) (string, error) {
	if config == nil {
		return "", stderror.ErrNullPointer
	}
	b, err := proto.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("proto.Marshal() failed: %w", err)
	}
	return "mieru://" + base64.StdEncoding.EncodeToString(b), nil
}

// URLToClientConfig returns a client configuration based on the URL.
func URLToClientConfig(s string) (*pb.ClientConfig, error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("url.Parse() failed: %w", err)
	}
	if u.Scheme != "mieru" {
		return nil, fmt.Errorf("unrecognized URL scheme %q", u.Scheme)
	}
	if u.Opaque != "" {
		return nil, fmt.Errorf("URL is opaque")
	}
	b, err := base64.StdEncoding.DecodeString(s[8:]) // Remove "mieru://"
	if err != nil {
		return nil, fmt.Errorf("base64.StdEncoding.DecodeString() failed: %w", err)
	}
	c := &pb.ClientConfig{}
	if err := proto.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("proto.Unmarshal() failed: %w", err)
	}
	return c, nil
}
