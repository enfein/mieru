//go:build linux

package tun

import "testing"

func TestCapNetAdminSet(t *testing.T) {
	testCases := []struct {
		name string
		mask uint64
		want bool
	}{
		{
			name: "no capabilities",
			mask: 0,
			want: false,
		},
		{
			name: "CAP_NET_ADMIN only",
			mask: 1 << capNetAdmin,
			want: true,
		},
		{
			name: "CAP_NET_ADMIN with other caps",
			mask: 0x0000000000001800,
			want: true,
		},
		{
			name: "root default CapEff",
			mask: 0x000001ffffffffff,
			want: true,
		},
		{
			name: "CAP_NET_BIND_SERVICE but no CAP_NET_ADMIN",
			mask: 1 << 10,
			want: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := capNetAdminSet(tc.mask); got != tc.want {
				t.Errorf("capNetAdminSet(0x%x) = %v, want %v", tc.mask, got, tc.want)
			}
		})
	}
}
