//go:build unix

package app

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestCreateWorkspaceFilePermissionDenied leaves the destination absent when the directory is not writable.
func TestCreateWorkspaceFilePermissionDenied(t *testing.T) {
	if syscall.Geteuid() == 0 {
		t.Skip("root ignores directory mode")
	}

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Errorf("restore directory mode: %v", err)
		}
	})

	dest := filepath.Join(dir, "workspace.json")
	spec := oneProject(t, "group/service", nil)

	if err := CreateWorkspaceFile(dest, spec); err == nil {
		t.Fatal("wrote a workspace into a read-only directory")
	}

	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("destination appeared after a permission error", err)
	}
}
