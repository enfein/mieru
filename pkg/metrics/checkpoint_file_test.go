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

//go:build !windows

package metrics

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	pb "github.com/enfein/mieru/v3/pkg/metrics/metricspb"
	"google.golang.org/protobuf/proto"
)

func TestCheckpointReplacesFileWithoutMutatingExistingReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.pb")
	setCheckpointPath(t, path)
	old, err := proto.Marshal(&pb.AllMetrics{Groups: []*pb.MetricGroup{{Name: proto.String("old-checkpoint")}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, old, 0640); err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	if err := DumpMetricsNow(); err != nil {
		t.Fatal(err)
	}
	stillOld, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(old, stillOld) {
		t.Fatal("checkpoint rewrote the inode held by an existing reader")
	}
	fresh, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(old, fresh) {
		t.Fatal("checkpoint did not publish a new snapshot")
	}
	if err := proto.Unmarshal(fresh, &pb.AllMetrics{}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0640 {
		t.Fatalf("checkpoint permissions %o, want 0640", info.Mode().Perm())
	}
}

func TestCheckpointRejectsSymlinkWithoutTouchingTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.pb")
	setCheckpointPath(t, path)
	target := filepath.Join(dir, "unrelated")
	if err := os.WriteFile(target, []byte("leave this alone"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := DumpMetricsNow(); err == nil {
		t.Fatal("checkpoint followed a symlink")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "leave this alone" {
		t.Fatal("symlink target changed")
	}
}

type failingCheckpointFile struct {
	checkpointFile
	fail string
	err  error
}

func (f failingCheckpointFile) Write(b []byte) (int, error) {
	if f.fail == "write" {
		return 0, f.err
	}
	if f.fail == "short-write" {
		return len(b) - 1, nil
	}
	return f.checkpointFile.Write(b)
}

func (f failingCheckpointFile) Chmod(mode os.FileMode) error {
	if f.fail == "chmod" {
		return f.err
	}
	return f.checkpointFile.Chmod(mode)
}

func (f failingCheckpointFile) Sync() error {
	if f.fail == "sync" {
		return f.err
	}
	return f.checkpointFile.Sync()
}

func (f failingCheckpointFile) Close() error {
	err := f.checkpointFile.Close()
	if f.fail == "close" {
		return f.err
	}
	return err
}

func TestCheckpointPublicationFailures(t *testing.T) {
	operations := []string{"open-dir", "create", "write", "short-write", "chmod", "sync", "close", "rename", "dir-sync", "dir-close"}
	for _, operation := range operations {
		t.Run(operation, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "metrics.pb")
			old, next := []byte("previous complete checkpoint"), []byte("next complete checkpoint")
			if err := os.WriteFile(path, old, 0600); err != nil {
				t.Fatal(err)
			}
			injected := errors.New("synthetic checkpoint failure")
			ops := checkpointFileOps{
				createTemp: func(dir, pattern string) (checkpointFile, error) {
					if operation == "create" {
						return nil, injected
					}
					f, err := os.CreateTemp(dir, pattern)
					if err != nil {
						return nil, err
					}
					return failingCheckpointFile{f, operation, injected}, nil
				},
				rename: func(from, to string) error {
					if operation == "rename" {
						return injected
					}
					return os.Rename(from, to)
				},
				openDir: func(dir string) (checkpointFile, error) {
					if operation == "open-dir" {
						return nil, injected
					}
					f, err := os.Open(dir)
					if err != nil {
						return nil, err
					}
					failure := ""
					if operation == "dir-sync" {
						failure = "sync"
					}
					if operation == "dir-close" {
						failure = "close"
					}
					return failingCheckpointFile{f, failure, injected}, nil
				},
			}
			err := writeCheckpointFile(path, next, ops)
			wantErr := injected
			if operation == "short-write" {
				wantErr = io.ErrShortWrite
			}
			if !errors.Is(err, wantErr) {
				t.Fatalf("error %v, want %v", err, wantErr)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want := old
			if operation == "dir-sync" || operation == "dir-close" {
				want = next
			}
			if !bytes.Equal(data, want) {
				t.Fatalf("published %q, want %q", data, want)
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != "metrics.pb" {
				t.Fatalf("temporary checkpoint leaked: %v", entries)
			}
		})
	}
}

func TestCheckpointNewFilePrivateAndComplete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.pb")
	setCheckpointPath(t, path)
	if err := DumpMetricsNow(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("new checkpoint permissions %o, want 0600", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := proto.Unmarshal(data, &pb.AllMetrics{}); err != nil {
		t.Fatal(err)
	}
}
