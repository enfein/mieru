// Copyright (C) 2026  mieru authors
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

package metrics

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// The boundary keeps publication and cleanup identical for real I/O and
// injected filesystem failures. No global hooks or extra writer goroutines.
type checkpointFile interface {
	io.Writer
	Name() string
	Chmod(os.FileMode) error
	Sync() error
	Close() error
}

type checkpointFileOps struct {
	createTemp func(dir, pattern string) (checkpointFile, error)
	rename     func(from, to string) error
	openDir    func(dir string) (checkpointFile, error)
}

// writeCheckpointFile publishes a complete file on the local server filesystem.
// The caller serializes snapshot collection and this entire operation. It is
// not a cross-process lock, nor a substitute for stopping byte producers.
func writeCheckpointFile(path string, data []byte, ops checkpointFileOps) (err error) {
	mode := os.FileMode(0600)
	info, statErr := os.Lstat(path)
	if statErr == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("checkpoint destination is not a regular file")
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("stat checkpoint: %w", statErr)
	}
	dir := filepath.Dir(path)
	directory, err := ops.openDir(dir)
	if err != nil {
		return fmt.Errorf("open checkpoint directory: %w", err)
	}
	defer func() {
		if closeErr := directory.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close checkpoint directory: %w", closeErr))
		}
	}()
	f, err := ops.createTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary checkpoint: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			if closeErr := f.Close(); closeErr != nil {
				err = errors.Join(err, fmt.Errorf("close temporary checkpoint: %w", closeErr))
			}
		}
		if removeErr := os.Remove(f.Name()); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove temporary checkpoint: %w", removeErr))
		}
	}()
	n, err := f.Write(data)
	if err != nil {
		return fmt.Errorf("write temporary checkpoint: %w", err)
	}
	if n != len(data) {
		return fmt.Errorf("write temporary checkpoint: %w", io.ErrShortWrite)
	}
	if err := f.Chmod(mode); err != nil {
		return fmt.Errorf("set checkpoint permissions: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync temporary checkpoint: %w", err)
	}
	closed = true
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temporary checkpoint: %w", err)
	}
	if err := ops.rename(f.Name(), path); err != nil {
		return fmt.Errorf("publish checkpoint: %w", err)
	}
	if err := directory.Sync(); err != nil {
		// Rename already happened. Never restore an older file or claim that
		// this error means nothing was published.
		return fmt.Errorf("checkpoint published but directory sync failed; crash durability uncertain: %w", err)
	}
	return nil
}
