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

package util

import (
	"context"
	"testing"
)

func TestDNSResolver(t *testing.T) {
	ctx := context.Background()

	// Default policy.
	d := DNSResolver{}
	addr, err := d.LookupIP(ctx, "localhost")
	if err != nil {
		t.Fatalf("LookupIP() failed: %v", err)
	}
	if !addr.IsLoopback() {
		t.Errorf("Returned IP address is not loopback address")
	}

	// IPv4 only.
	d.DNSPolicy = DNSPolicyIPv4Only
	addr, err = d.LookupIP(ctx, "localhost")
	if err != nil {
		t.Fatalf("LookupIP() failed: %v", err)
	}
	if !addr.IsLoopback() {
		t.Errorf("Returned IP address is not loopback address")
	}

	if IsIPDualStack() {
		// IPv6 only.
		d.DNSPolicy = DNSPolicyIPv6Only
		addr, err = d.LookupIP(ctx, "localhost")
		if err != nil {
			t.Fatalf("LookupIP() failed: %v", err)
		}
		if !addr.IsLoopback() {
			t.Errorf("Returned IP address is not loopback address")
		}
	}
}
