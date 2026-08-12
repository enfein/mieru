//go:build !windows

package wintun

func InitWintun() error {
	return nil
}
