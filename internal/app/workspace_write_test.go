package app

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// failWriteCloser injects a write or close error without touching the filesystem.
type failWriteCloser struct {
	writeErr error
	closeErr error
}

// shortWriteCloser reports a partial write and a successful close.
type shortWriteCloser struct{}

// Write returns the injected write error, or accepts the buffer.
func (f *failWriteCloser) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}

	return len(p), nil
}

// Close returns the injected close error.
func (f *failWriteCloser) Close() error {
	return f.closeErr
}

// Write accepts one byte of a non-empty buffer.
func (shortWriteCloser) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	return 1, nil
}

// Close reports success.
func (shortWriteCloser) Close() error {
	return nil
}

// TestCreateWorkspaceFileRejectsOversizedDocument checks the existing 1 MiB limit before create.
func TestCreateWorkspaceFileRejectsOversizedDocument(t *testing.T) {
	spec := oneProject(t, "group/service", nil)
	spec.Release = strings.Repeat("a", workspaceMaxBytes)
	dest := filepath.Join(t.TempDir(), "workspace.json")

	if err := CreateWorkspaceFile(dest, spec); !errors.Is(err, errWorkspaceSize) {
		t.Fatal(err)
	}

	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("oversized workspace was created", err)
	}
}

// TestCreateWorkspaceFileMissingParentDoesNotInventADirectory keeps a bad path visible.
func TestCreateWorkspaceFileMissingParentDoesNotInventADirectory(t *testing.T) {
	spec := oneProject(t, "group/service", nil)
	dest := filepath.Join(t.TempDir(), "missing", "workspace.json")

	if err := CreateWorkspaceFile(dest, spec); err == nil {
		t.Fatal("created a workspace without its parent directory")
	}

	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

// TestFinishExclusiveRemovesIncompleteFile checks write and close failures delete only the new file.
func TestFinishExclusiveRemovesIncompleteFile(t *testing.T) {
	dir := t.TempDir()
	kept := filepath.Join(dir, "kept.json")

	if err := os.WriteFile(kept, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		file io.WriteCloser
		want error
	}{
		{
			name: "write",
			file: &failWriteCloser{
				writeErr: io.ErrClosedPipe,
			},
			want: io.ErrClosedPipe,
		},
		{
			name: "short",
			file: shortWriteCloser{},
			want: io.ErrShortWrite,
		},
		{
			name: "close",
			file: &failWriteCloser{
				closeErr: io.ErrUnexpectedEOF,
			},
			want: io.ErrUnexpectedEOF,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dest := filepath.Join(dir, tc.name+".json")

			if err := os.WriteFile(dest, []byte("partial"), 0o600); err != nil {
				t.Fatal(err)
			}

			err := finishExclusive(tc.file, dest, []byte("{}\n"))
			if !errors.Is(err, tc.want) {
				t.Fatal(err)
			}

			if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
				t.Fatal("incomplete file kept", statErr)
			}

			body, readErr := os.ReadFile(kept)
			if readErr != nil || string(body) != "old\n" {
				t.Fatal("unrelated file changed", readErr)
			}
		})
	}
}
