//go:build unix

package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestRefreshPermissionErrorDoesNotPublish stops when a directory cannot be read.
func TestRefreshPermissionErrorDoesNotPublish(t *testing.T) {
	if syscall.Geteuid() == 0 {
		t.Skip("root ignores directory mode")
	}

	f := setup(t)
	file := workspaceFromScan(t, f)
	before := readBytes(t, file)
	blocked := filepath.Join(f.base, "secret")

	if err := os.MkdirAll(blocked, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := os.Chmod(blocked, 0o700); err != nil {
			t.Errorf("restore directory mode: %v", err)
		}
	})

	options := &WorkspaceRefreshOptions{
		BaseDir: f.base,
		Sync:    true,
	}
	if _, err := RefreshWorkspace(t.Context(), offlineClient(f), file, options); err == nil ||
		!bytes.Equal(readBytes(t, file), before) {
		t.Fatal(err)
	}
}

// TestPublishWorkspacePreservesMode copies the regular-file mode onto the replacement.
func TestPublishWorkspacePreservesMode(t *testing.T) {
	document := sampleDocument(t)
	if err := os.Chmod(document.path, 0o640); err != nil {
		t.Fatal(err)
	}

	info, err := os.Lstat(document.path)
	if err != nil {
		t.Fatal(err)
	}

	document.info = info
	next := document.spec.clone()
	next.Release = "replacement"

	if err = publishWorkspace(t.Context(), document, next, nil); err != nil {
		t.Fatal(err)
	}

	got, err := os.Lstat(document.path)
	if err != nil {
		t.Fatal(err)
	}

	if got.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o", got.Mode().Perm())
	}

	if _, err = LoadWorkspace(document.path); err != nil {
		t.Fatal(err)
	}
}

// TestRefreshRejectsSymlinkWorkspace refuses to replace a workspace path that is a symbolic link.
func TestRefreshRejectsSymlinkWorkspace(t *testing.T) {
	f := setup(t)
	file := workspaceFromScan(t, f)
	before := readBytes(t, file)
	link := filepath.Join(t.TempDir(), "link.json")

	if err := os.Symlink(file, link); err != nil {
		t.Skip(err)
	}

	cloneRel(t, f, "lamiona/search/new-indexer")
	options := &WorkspaceRefreshOptions{
		BaseDir: f.base,
		Sync:    true,
	}
	if _, err := RefreshWorkspace(
		t.Context(),
		offlineClient(f),
		link,
		options,
	); !errors.Is(
		err,
		errWorkspaceFileKind,
	) ||
		!bytes.Equal(readBytes(t, file), before) {
		t.Fatal(err)
	}
}
