package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResetBranchDiscardsLocalCommits verifies that reset drops local commits on the target branch.
func TestResetBranchDiscardsLocalCommits(t *testing.T) {
	f := setup(t)
	origin := branch(t, f, "release")
	git(t, f.repo, "fetch", "origin")
	git(t, f.repo, "switch", "--track", "origin/release")
	git(t, f.repo, "commit", "--allow-empty", "-m", "local")

	local := git(t, f.repo, "rev-parse", "HEAD")
	f.cfg.Branch = "release"
	f.cfg.Local = localReset
	s, log, e := run(t, f, nil)

	if e != nil || s.Updated != 1 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}

	if git(t, f.repo, "rev-parse", "HEAD") != origin || git(t, f.repo, "branch", "--show-current") != "release" {
		t.Fatal("branch was not reset")
	}

	if git(t, f.repo, "rev-parse", "--abbrev-ref", "@{u}") != "origin/release" {
		t.Fatal("upstream was not set")
	}

	history := git(t, f.repo, "log", "--format=%H", "release")
	if strings.Contains(history, local) || !strings.Contains(log, "discarded 1 local commit") {
		t.Fatalf("local commit kept\n%s", log)
	}
}

// TestResetBranchDryRunKeepsCommits verifies that a dry run with reset does not drop commits.
func TestResetBranchDryRunKeepsCommits(t *testing.T) {
	f := setup(t)
	branch(t, f, "release")
	git(t, f.repo, "fetch", "origin")
	git(t, f.repo, "switch", "--track", "origin/release")
	git(t, f.repo, "commit", "--allow-empty", "-m", "local")
	git(t, f.repo, "commit", "--allow-empty", "-m", "local-2")

	local := git(t, f.repo, "rev-parse", "HEAD")
	f.cfg.Branch = "release"
	f.cfg.Local = localReset
	f.cfg.DryRun = true
	s, log, e := run(t, f, nil)

	if e != nil || s.Planned != 1 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}

	if git(t, f.repo, "rev-parse", "HEAD") != local || !strings.Contains(log, "discarding 2 local commits") {
		t.Fatalf("dry-run moved the branch\n%s", log)
	}
}

// TestResetBranchLeavesCommitsOnAnotherBranch verifies that reset does not touch commits on another branch.
func TestResetBranchLeavesCommitsOnAnotherBranch(t *testing.T) {
	f := setup(t)
	branch(t, f, "release")
	git(t, f.repo, "commit", "--allow-empty", "-m", "local")

	master := git(t, f.repo, "rev-parse", "master")
	f.cfg.Branch = "release"
	f.cfg.Local = localReset
	s, log, e := run(t, f, nil)

	if e != nil || s.Updated != 1 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}

	if git(t, f.repo, "branch", "--show-current") != "release" || git(t, f.repo, "rev-parse", "master") != master {
		t.Fatal("commit on master was discarded")
	}
}

// TestResetBranchStillSkipsDetachedHead verifies that reset still refuses a detached HEAD.
func TestResetBranchStillSkipsDetachedHead(t *testing.T) {
	f := setup(t)
	branch(t, f, "release")
	git(t, f.repo, "checkout", "--detach")

	head := git(t, f.repo, "rev-parse", "HEAD")
	f.cfg.Branch = "release"
	f.cfg.Local = localReset
	s, log, e := run(t, f, nil)

	if e != nil || s.Skipped != 1 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}

	if git(t, f.repo, "rev-parse", "HEAD") != head || git(t, f.repo, "branch", "--show-current") != "" {
		t.Fatal("detached HEAD was moved")
	}
}

// TestResetBranchDiscardsEdits verifies that reset deletes uncommitted edits.
func TestResetBranchDiscardsEdits(t *testing.T) {
	f := setup(t)
	branch(t, f, "release")
	git(t, f.repo, "fetch", "origin")
	git(t, f.repo, "switch", "--track", "origin/release")
	git(t, f.repo, "commit", "--allow-empty", "-m", "local")
	write(t, filepath.Join(f.repo, "file"), "changed\n")
	git(t, f.repo, "add", "file")
	write(t, filepath.Join(f.repo, "extra"), "new\n")

	f.cfg.Branch = "release"
	f.cfg.Local = localReset
	s, log, e := run(t, f, nil)

	if e != nil || s.Updated != 1 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}

	if readFile(t, filepath.Join(f.repo, "file")) != "initial\n" || git(t, f.repo, "stash", "list") != "" {
		t.Fatalf("edits were kept\n%s", log)
	}

	if _, err := os.Stat(filepath.Join(f.repo, "extra")); !os.IsNotExist(err) {
		t.Fatal("untracked file was kept")
	}
}
