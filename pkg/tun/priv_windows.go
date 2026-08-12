//go:build windows

package tun

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func checkTunPrivileges() error {
	if !windows.GetCurrentProcessToken().IsElevated() {
		return fmt.Errorf("administrator privileges are required to create a TUN interface. Please run this program as administrator")
	}
	return nil
}
