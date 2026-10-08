package app

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"
)

// cancelArchiveWriter cancels the operation on its first output write.
type cancelArchiveWriter struct {
	// cancel cancels the archive context without injecting an I/O error.
	cancel context.CancelFunc
}

// stallReader returns one byte, then waits longer than the caller's deadline.
type stallReader struct {
	// reads counts Read calls.
	reads int
}

// Write simulates cancellation arriving while a ZIP member is copied.
func (w *cancelArchiveWriter) Write(p []byte) (int, error) {
	w.cancel()

	return len(p), nil
}

// Read returns one byte and then sleeps past a one-second deadline.
func (r *stallReader) Read(p []byte) (int, error) {
	r.reads++

	if r.reads == 1 {
		p[0] = 1

		return 1, nil
	}

	time.Sleep(2 * time.Second)

	return 0, io.EOF
}

// TestReviewArchiveRelativeDestination covers the documented CLI path shape.
func TestReviewArchiveRelativeDestination(t *testing.T) {
	f := setup(t)
	file := writeWorkspace(t, f.base, archiveProject(f, nil))
	t.Chdir(t.TempDir())
	in := &archiveOptInput{fixture: f, workspace: file, dest: "./out.zip"}
	opts := archiveOpts(in)

	_, err := ArchiveWorkspace(t.Context(), opts)
	if err != nil {
		leaks, globErr := filepath.Glob(filepath.Join(f.repo, ".release-align-repo-*.zip"))
		t.Fatalf("relative destination failed: %v; leaked repo ZIPs: %v (%v)", err, leaks, globErr)
	}

	if _, err = os.Stat("out.zip"); err != nil {
		t.Fatal(err)
	}

	leaks, globErr := filepath.Glob(filepath.Join(f.repo, ".release-align-repo-*.zip"))
	if globErr != nil || len(leaks) != 0 {
		t.Fatal(globErr, leaks)
	}
}

// TestReviewArchiveRejectsOrdinarySubdirectory prevents exporting a parent repository as another project.
func TestReviewArchiveRejectsOrdinarySubdirectory(t *testing.T) {
	f := setup(t)
	child := filepath.Join(f.repo, "ordinary")

	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}

	write(t, filepath.Join(child, "file.txt"), "parent repository content\n")
	git(t, f.repo, "add", "ordinary")
	git(t, f.repo, "commit", "-m", "ordinary directory")
	git(t, f.repo, "push", "origin", "HEAD:master")
	spec := archiveProject(f, nil)
	spec.Projects[0].Path += "/ordinary"
	file := writeWorkspace(t, f.base, spec)
	dest := filepath.Join(f.base, "wrong-root.zip")
	in := &archiveOptInput{fixture: f, workspace: file, dest: dest}
	opts := archiveOpts(in)

	report, err := ArchiveWorkspace(t.Context(), opts)
	if err == nil {
		t.Fatalf("non-repository directory was accepted: %+v", report)
	}

	if _, statErr := os.Stat(dest); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("invalid repository published an archive: %v", statErr)
	}
}

// TestReviewArchiveCancellationDuringLastEntry requires cancellation to propagate.
func TestReviewArchiveCancellationDuringLastEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.zip")

	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	zw := zip.NewWriter(file)
	header := &zip.FileHeader{
		Name:   "group/repo/blob.bin",
		Method: zip.Store,
	}

	body, err := zw.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = body.Write(make([]byte, 128*1024)); err != nil {
		t.Fatal(err)
	}

	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}

	if err = file.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	sink := &cancelArchiveWriter{
		cancel: cancel,
	}
	writer := zip.NewWriter(sink)

	source := &repoZipSource{
		writer:  writer,
		names:   newArchiveNames(),
		project: "group/repo",
		path:    path,
	}

	err = copyRepoZip(ctx, source)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("last entry copy returned %v after cancellation; ctx=%v", err, ctx.Err())
	}

	_ = writer.Close()
}

// TestReviewArchiveCopyDeadline stops a raw copy when the context deadline passes between chunks.
func TestReviewArchiveCopyDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()

		err := copyBounded(ctx, io.Discard, new(stallReader))
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	})
}

// TestReviewRefreshUsesWorkspaceLocalTimeout applies timeouts.local before the Git scan.
func TestReviewRefreshUsesWorkspaceLocalTimeout(t *testing.T) {
	f := setup(t)
	local := "1ns"
	spec := archiveProject(f, nil)
	spec.Timeouts = &WorkspaceTimeouts{
		Local: &local,
	}
	file := writeWorkspace(t, f.base, spec)
	client := offlineClient(f)
	options := &WorkspaceRefreshOptions{
		BaseDir: f.base,
	}

	_, err := RefreshWorkspace(t.Context(), client, file, options)
	if client.LocalTimeout != time.Nanosecond || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout %s err %v", client.LocalTimeout, err)
	}
}
