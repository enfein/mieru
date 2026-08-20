//go:build !linux && !windows

package tun

import "fmt"

func checkTunPrivileges() error {
	return fmt.Errorf("TUN mode is not supported on this platform")
}
