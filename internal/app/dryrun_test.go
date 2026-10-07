package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestDryRunDoesNotFetchOrWrite verifies that a dry run neither fetches nor writes.
func TestDryRunDoesNotFetchOrWrite(t *testing.T) {
	f := setup(t)
	branch(t, f, "release")
	git(t, f.repo, "fetch", "origin")

	f.cfg.Branch = "release"
	f.cfg.DryRun = true
	git(t, f.repo, "remote", "set-url", "origin", "ssh://git@unreachable.invalid/repo")

	want := git(t, f.repo, "rev-parse", "HEAD")

	before, err := os.ReadFile(filepath.Join(f.repo, ".git", "FETCH_HEAD"))
	if err != nil {
		t.Fatal(err)
	}

	s, log, e := run(t, f, nil)
	if e != nil || s.Planned != 1 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}

	after, err := os.ReadFile(filepath.Join(f.repo, ".git", "FETCH_HEAD"))
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(before, after) || git(t, f.repo, "rev-parse", "HEAD") != want {
		t.Fatal("dry-run wrote repository")
	}

	if _, statErr := os.Stat(filepath.Join(f.base, ".release-align.lock")); !os.IsNotExist(statErr) {
		t.Fatal("dry-run created lock")
	}
}

// TestConflictingTagFailsWithoutMovingBranch verifies that a conflicting tag fails and leaves the branch in place.
func TestConflictingTagFailsWithoutMovingBranch(t *testing.T) {
	f := setup(t)
	git(t, f.repo, "tag", "v1")

	want := git(t, f.repo, "rev-parse", "HEAD")
	git(t, f.seed, "commit", "--allow-empty", "-m", "new")
	git(t, f.seed, "tag", "v1")
	git(t, f.seed, "push", "origin", "master", "v1")

	s, log, e := run(t, f, nil)
	if e == nil || s.Failed != 1 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}

	if git(t, f.repo, "rev-parse", "HEAD") != want || git(t, f.repo, "rev-parse", "v1") != want {
		t.Fatal("local refs overwritten")
	}
}

// TestIgnoredFilesAreNotOverwritten verifies that checkout does not replace ignored files.
func TestIgnoredFilesAreNotOverwritten(t *testing.T) {
	f := setup(t)
	write(t, filepath.Join(f.repo, ".git", "info", "exclude"), "release-file\n")
	write(t, filepath.Join(f.repo, "release-file"), "local ignored data")
	branch(t, f, "release")

	f.cfg.Branch = "release"
	s, log, e := run(t, f, nil)

	if e == nil || s.Failed != 1 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}

	b, err := os.ReadFile(filepath.Join(f.repo, "release-file"))
	if err != nil {
		t.Fatal(err)
	}

	if string(b) != "local ignored data" {
		t.Fatal("ignored file overwritten")
	}

	if git(t, f.repo, "branch", "--show-current") != "master" {
		t.Fatal("switched despite ignored file conflict")
	}
}
