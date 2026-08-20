//go:build 386 && windows

package wintun

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

//go:embed bin/x86/wintun.dll
var wintunDll []byte

func InitWintun() error {
	dllPath := filepath.Join(os.TempDir(), "wintun.dll")

	if _, err := os.Stat(dllPath); os.IsNotExist(err) {
		if err := os.WriteFile(dllPath, wintunDll, 0644); err != nil {
			return err
		}
	}

	handle, err := syscall.LoadLibrary(dllPath)
	if err != nil {
		return fmt.Errorf("wintun.dll library not loaded: %w", err)
	}
	_ = handle

	return nil
}
