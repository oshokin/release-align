package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestArchivePacksPinnedTrees checks filters, pins, and a dirty worktree.
func TestArchivePacksPinnedTrees(t *testing.T) {
	f := setup(t)
	second := filepath.Join(f.base, "mailion", "two")

	if err := os.MkdirAll(filepath.Dir(second), 0o700); err != nil {
		t.Fatal(err)
	}

	remote := filepath.Join(filepath.Dir(f.remote), "two.git")
	git(t, f.base, "init", "--bare", "--initial-branch=master", remote)
	git(t, f.base, "clone", remote, second)
	write(t, filepath.Join(second, "keep.txt"), "two\n")
	git(t, second, "add", "keep.txt")
	git(t, second, "commit", "-m", "two")
	git(t, second, "push", "-u", "origin", "master")

	write(t, filepath.Join(f.repo, "file"), "dirty\n")
	write(t, filepath.Join(f.repo, "untracked.txt"), "nope\n")
	git(t, f.repo, "switch", "-c", "side")
	write(t, filepath.Join(f.repo, "side.txt"), "side\n")
	git(t, f.repo, "add", "side.txt")
	git(t, f.repo, "commit", "-m", "side")

	pinned := git(t, f.repo, "rev-parse", "refs/remotes/origin/master")
	spec := &WorkspaceSpec{
		SchemaVersion: 1,
		Release:       "26.3.0",
		DefaultBranch: "master",
		Projects: []*ProjectSpec{
			{
				Path:   "group/repo with spaces",
				Groups: []string{"search"},
				Revision: &RevisionSpec{
					Commit: pinned,
				},
			},
			{
				Path:   "mailion/two",
				Groups: []string{"mailion"},
			},
		},
	}
	file := writeWorkspace(t, f.base, spec)
	dest := filepath.Join(f.base, "out.zip")

	_, err := ArchiveWorkspace(t.Context(), archiveOpts(f, file, dest, nil, nil))
	if err != nil {
		t.Fatal(err)
	}

	body := zipText(t, dest, "group/repo with spaces/file")
	if body != "initial\n" || zipHas(t, dest, "group/repo with spaces/untracked.txt") ||
		zipHas(t, dest, "group/repo with spaces/side.txt") || zipHas(t, dest, ".git") {
		t.Fatalf("worktree leaked into the archive: %q", body)
	}

	manifest := readManifest(t, dest)
	if manifest.Release != "26.3.0" || manifest.Repositories[0].Commit != pinned ||
		manifest.Repositories[0].Requested != pinned {
		t.Fatalf("manifest %+v", manifest.Repositories[0])
	}

	grouped, err := ArchiveWorkspace(
		t.Context(),
		archiveOpts(f, file, filepath.Join(f.base, "group.zip"), nil, []string{"mailion"}),
	)
	if err != nil {
		t.Fatal(err)
	}

	if grouped.Repositories != 1 || zipHas(t, grouped.File, "group/repo with spaces/file") {
		t.Fatal(grouped)
	}
}

// TestArchiveKeepsThePlannedCommit moves the branch after planning.
func TestArchiveKeepsThePlannedCommit(t *testing.T) {
	f := setup(t)
	file := writeWorkspace(t, f.base, archiveProject(f, nil))
	dest := filepath.Join(f.base, "planned.zip")

	job, err := newArchiveJob(t.Context(), archiveOpts(f, file, dest, nil, nil))
	if err != nil {
		t.Fatal(err)
	}

	planned, err := job.plan()
	if err != nil {
		t.Fatal(err)
	}

	write(t, filepath.Join(f.seed, "file"), "moved\n")
	git(t, f.seed, "add", "file")
	git(t, f.seed, "commit", "-m", "moved")
	git(t, f.seed, "push", "origin", "master")
	git(t, f.repo, "fetch", "origin")

	if err = job.write(planned); err != nil {
		t.Fatal(err)
	}

	if zipText(t, dest, "group/repo with spaces/file") != "initial\n" {
		t.Fatal(zipText(t, dest, "group/repo with spaces/file"))
	}
}

// TestArchiveHonorsExportIgnoreAndModes checks attributes, a symlink, and a gitlink.
func TestArchiveHonorsExportIgnoreAndModes(t *testing.T) {
	f := setup(t)
	write(t, filepath.Join(f.repo, ".gitattributes"), "secret.txt export-ignore\n")
	write(t, filepath.Join(f.repo, "secret.txt"), "secret\n")
	write(t, filepath.Join(f.repo, "tool"), "#!/bin/sh\n")
	write(t, filepath.Join(f.repo, "target-name"), "SECRET\n")

	if err := os.Symlink("target-name", filepath.Join(f.repo, "link")); err != nil {
		t.Fatal(err)
	}

	git(t, f.repo, "add", ".")
	git(t, f.repo, "update-index", "--chmod=+x", "tool")
	oid := git(t, f.repo, "rev-parse", "HEAD")
	git(t, f.repo, "update-index", "--add", "--cacheinfo", "160000,"+oid+",vendor/lib")
	git(t, f.repo, "commit", "-m", "attrs")
	git(t, f.repo, "push", "origin", "HEAD:master")

	file := writeWorkspace(t, f.base, archiveProject(f, nil))
	dest := filepath.Join(f.base, "modes.zip")

	report, err := ArchiveWorkspace(t.Context(), archiveOpts(f, file, dest, nil, nil))
	if err != nil {
		t.Fatal(err)
	}

	if zipHas(t, dest, "group/repo with spaces/secret.txt") {
		t.Fatal("export-ignore file was packed")
	}

	if zipText(t, dest, "group/repo with spaces/link") != "target-name" {
		t.Fatal("symlink target was read")
	}

	if zipMode(t, dest, "group/repo with spaces/link")&os.ModeSymlink == 0 {
		t.Fatal("symlink was stored as a regular file")
	}

	if zipMode(t, dest, "group/repo with spaces/tool")&0o111 == 0 {
		t.Fatal("executable bit was dropped")
	}

	if report.Gitlinks != 1 || readManifest(t, dest).Repositories[0].Gitlinks[0].Path != "vendor/lib" {
		t.Fatalf("gitlinks %+v", readManifest(t, dest).Repositories[0].Gitlinks)
	}

	if zipText(t, dest, "group/repo with spaces/tool") == "" {
		t.Fatal("executable file missing")
	}
}

// TestArchiveFailurePublishesNothing covers a missing clone, a missing object, and an existing file.
func TestArchiveFailurePublishesNothing(t *testing.T) {
	f := setup(t)
	spec := archiveProject(f, nil)
	spec.Projects = append(spec.Projects, &ProjectSpec{
		Path: "mailion/missing",
	})
	file := writeWorkspace(t, f.base, spec)
	dest := filepath.Join(f.base, "missing.zip")

	_, err := ArchiveWorkspace(t.Context(), archiveOpts(f, file, dest, nil, nil))
	if err == nil {
		t.Fatal("missing clone succeeded")
	}

	if _, statErr := os.Stat(dest); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal(statErr)
	}

	pinned := &RevisionSpec{
		Commit: "0123456789abcdef0123456789abcdef01234567",
	}
	file = writeWorkspace(t, f.base, archiveProject(f, pinned))
	dest = filepath.Join(f.base, "absent.zip")

	_, err = ArchiveWorkspace(t.Context(), archiveOpts(f, file, dest, nil, nil))
	if err == nil {
		t.Fatal("missing object succeeded")
	}

	if _, statErr := os.Stat(dest); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal(statErr)
	}

	kept := filepath.Join(f.base, "kept.zip")
	write(t, kept, "keep")
	_, err = ArchiveWorkspace(
		t.Context(),
		archiveOpts(f, writeWorkspace(t, f.base, archiveProject(f, nil)), kept, nil, nil),
	)

	if err == nil || string(mustRead(t, kept)) != "keep" {
		t.Fatalf("existing archive changed: %v", err)
	}
}

// TestArchiveTimeoutAndCancel leave no ZIP behind.
func TestArchiveTimeoutAndCancel(t *testing.T) {
	f := setup(t)
	file := writeWorkspace(t, f.base, archiveProject(f, nil))
	dest := filepath.Join(f.base, "slow.zip")
	opts := archiveOpts(f, file, dest, nil, nil)
	opts.ArchiveTimeout = time.Nanosecond

	_, err := ArchiveWorkspace(t.Context(), opts)
	if err == nil {
		t.Fatal("timeout succeeded")
	}

	if _, statErr := os.Stat(dest); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal(statErr)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	dest = filepath.Join(f.base, "canceled.zip")
	_, err = ArchiveWorkspace(ctx, archiveOpts(f, file, dest, nil, nil))

	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}

	if _, statErr := os.Stat(dest); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal(statErr)
	}
}

// TestArchiveEntryNames rejects traversal, duplicates, and a file used as a directory.
func TestArchiveEntryNames(t *testing.T) {
	names := newArchiveNames()
	samples := []string{"../x", "/abs", `a\b`, "a/../../x"}

	for _, sample := range samples {
		if _, err := names.add(sample, 0o644); err == nil {
			t.Fatal(sample)
		}
	}

	if _, err := names.add("mailion/", os.ModeDir); err != nil {
		t.Fatal(err)
	}

	skip, err := names.add("mailion/", os.ModeDir)
	if err != nil || !skip {
		t.Fatal(skip, err)
	}

	if _, err = names.add("mailion/file", 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err = names.add("mailion/file", 0o644); err == nil {
		t.Fatal("duplicate file accepted")
	}

	if _, err = names.add("mailion/file/child", 0o644); err == nil {
		t.Fatal("file ancestor accepted")
	}
}

// TestArchiveTimeoutOverride lets an unlocked workspace value replace the default.
func TestArchiveTimeoutOverride(t *testing.T) {
	raw := `{"schema_version":1,"default_branch":"master","timeouts":{"archive":"0"},"projects":[{"path":"a/b"}]}`
	if _, err := DecodeWorkspace(strings.NewReader(raw)); err == nil {
		t.Fatal("zero archive timeout accepted")
	}

	raw = `{"schema_version":1,"default_branch":"master","timeouts":{"archive":"30m"},"projects":[{"path":"a/b"}]}`

	spec, err := DecodeWorkspace(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}

	opts := &WorkspaceArchiveOptions{
		ArchiveTimeout: 15 * time.Minute,
		LocalTimeout:   time.Second,
	}

	if err = applyArchiveTimeouts(opts, spec); err != nil {
		t.Fatal(err)
	}

	if opts.ArchiveTimeout != 30*time.Minute {
		t.Fatal(opts.ArchiveTimeout)
	}

	opts.ArchiveTimeout = 15 * time.Minute
	opts.timeoutLocks = &TimeoutLocks{
		Archive: true,
	}

	if err = applyArchiveTimeouts(opts, spec); err != nil {
		t.Fatal(err)
	}

	if opts.ArchiveTimeout != 15*time.Minute {
		t.Fatal(opts.ArchiveTimeout)
	}
}

// TestArchiveLargeFileCopiesABlob checks that a multi-megabyte blob survives packing.
func TestArchiveLargeFileCopiesABlob(t *testing.T) {
	f := setup(t)
	payload := bytes.Repeat([]byte("A"), 2<<20)
	write(t, filepath.Join(f.repo, "blob"), string(payload))
	git(t, f.repo, "add", "blob")
	git(t, f.repo, "commit", "-m", "blob")
	git(t, f.repo, "push", "origin", "HEAD:master")

	file := writeWorkspace(t, f.base, archiveProject(f, nil))
	dest := filepath.Join(f.base, "blob.zip")

	_, err := ArchiveWorkspace(t.Context(), archiveOpts(f, file, dest, nil, nil))
	if err != nil {
		t.Fatal(err)
	}

	if zipText(t, dest, "group/repo with spaces/blob") != string(payload) {
		t.Fatal("blob mismatch")
	}
}

func archiveProject(f *fixture, revision *RevisionSpec) *WorkspaceSpec {
	return &WorkspaceSpec{
		SchemaVersion: 1,
		DefaultBranch: "master",
		Projects: []*ProjectSpec{
			{
				Path:     "group/repo with spaces",
				Groups:   []string{"search"},
				Revision: revision,
			},
		},
	}
}

func archiveOpts(f *fixture, workspace, dest string, repos, groups []string) *WorkspaceArchiveOptions {
	return &WorkspaceArchiveOptions{
		WorkspaceFile:  workspace,
		BaseDir:        f.base,
		File:           dest,
		Repositories:   repos,
		Groups:         groups,
		Output:         outputText,
		ArchiveTimeout: time.Minute,
		LocalTimeout:   5 * time.Second,
	}
}

func writeWorkspace(t *testing.T, dir string, spec *WorkspaceSpec) string {
	t.Helper()

	encoded, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "workspace.json")
	write(t, path, string(encoded))

	return path
}

func zipText(t *testing.T, archive, name string) string {
	t.Helper()

	reader, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}

	defer reader.Close()

	for _, file := range reader.File {
		if file.Name != name {
			continue
		}

		body, openErr := file.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}

		data, readErr := io.ReadAll(body)
		_ = body.Close()

		if readErr != nil {
			t.Fatal(readErr)
		}

		return string(data)
	}

	t.Fatalf("missing %s", name)

	return ""
}

func zipMode(t *testing.T, archive, name string) os.FileMode {
	t.Helper()

	reader, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}

	defer reader.Close()

	for _, file := range reader.File {
		if file.Name == name {
			return file.Mode()
		}
	}

	t.Fatalf("missing %s", name)

	return 0
}

func zipHas(t *testing.T, archive, name string) bool {
	t.Helper()

	reader, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}

	defer reader.Close()

	for _, file := range reader.File {
		if file.Name == name || strings.Contains(file.Name, name) {
			return true
		}
	}

	return false
}

func readManifest(t *testing.T, archive string) *archiveManifest {
	t.Helper()

	var manifest archiveManifest

	if err := json.Unmarshal([]byte(zipText(t, archive, archiveManifestDir+"/manifest.json")), &manifest); err != nil {
		t.Fatal(err)
	}

	return &manifest
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return data
}
