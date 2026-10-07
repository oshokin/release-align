package app

import (
	"path/filepath"
	"strings"
	"testing"
)

var dirtyWorktreeKinds = []string{
	"untracked",
	"staged",
	"unstaged",
	"unpushed",
	"no-upstream",
	"detached",
	"merge",
	"other-remote",
	"missing-upstream",
}

var unsafeTargetKinds = []string{
	"ahead",
	"diverged",
	"no-upstream",
	"wrong-upstream",
}

// TestPreferredBranchAndFastForward verifies a fast-forward onto the preferred branch.
func TestPreferredBranchAndFastForward(t *testing.T) {
	f := setup(t)
	want := branch(t, f, "Release-2.3.2")
	f.cfg.Branch = "Release-2.3.2"
	s, log, e := run(t, f, nil)

	if e != nil || s.Updated != 1 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}

	if got := git(t, f.repo, "rev-parse", "HEAD"); got != want {
		t.Fatal(got, want)
	}

	if got := git(t, f.repo, "rev-parse", "--abbrev-ref", "@{u}"); got != "origin/Release-2.3.2" {
		t.Fatal(got)
	}

	write(t, filepath.Join(f.seed, "new"), "next")
	git(t, f.seed, "add", ".")
	git(t, f.seed, "commit", "-m", "next")
	git(t, f.seed, "push")

	s, log, e = run(t, f, nil)
	if e != nil || s.Updated != 1 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}

	if git(t, f.repo, "rev-parse", "HEAD") != git(t, f.seed, "rev-parse", "HEAD") {
		t.Fatal("not fast-forwarded")
	}
}

// TestDirtyAndUnsafeCurrentRemainUntouched verifies that an unsafe current branch is not switched.
func TestDirtyAndUnsafeCurrentRemainUntouched(t *testing.T) {
	for _, kind := range dirtyWorktreeKinds {
		t.Run(kind, func(t *testing.T) {
			assertDirtyLeftUntouched(t, kind)
		})
	}
}

// TestUnsafeTargetDoesNotSwitch verifies that an unsafe target branch is not checked out.
func TestUnsafeTargetDoesNotSwitch(t *testing.T) {
	for _, kind := range unsafeTargetKinds {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			branch(t, f, "release")
			git(t, f.repo, "fetch", "origin")
			git(t, f.repo, "switch", "--track", "origin/release")

			switch kind {
			case "ahead", "diverged":
				git(t, f.repo, "commit", "--allow-empty", "-m", "local")
			case "no-upstream":
				git(t, f.repo, "branch", "--unset-upstream")
			case "wrong-upstream":
				git(t, f.repo, "branch", "--set-upstream-to=origin/master")
			}

			if kind == "diverged" {
				git(t, f.seed, "commit", "--allow-empty", "-m", "remote")
				git(t, f.seed, "push")
			}

			target := git(t, f.repo, "rev-parse", "release")
			git(t, f.repo, "switch", "master")

			f.cfg.Branch = "release"
			s, log, e := run(t, f, nil)

			if e != nil || s.Skipped != 1 {
				t.Fatalf("%+v %v\n%s", s, e, log)
			}

			if git(t, f.repo, "branch", "--show-current") != "master" ||
				git(t, f.repo, "rev-parse", "release") != target {
				t.Fatal("target changed")
			}
		})
	}
}

// TestCommitBranchSelectionExcludesOriginHEAD verifies that origin/HEAD is not chosen as the branch that contains a commit.
func TestCommitBranchSelectionExcludesOriginHEAD(t *testing.T) {
	f := setup(t)
	f.cfg.Branch = "absent"
	commit := git(t, f.repo, "rev-parse", "HEAD")
	git(t, f.seed, "branch", "aaa")
	git(t, f.seed, "push", "origin", "aaa")

	table := Manifest{filepath.Base(f.repo): {Tag: "v1", Commit: commit[:7]}}
	s, log, e := run(t, f, table)

	if e != nil || s.Updated != 1 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}

	if git(t, f.repo, "branch", "--show-current") != "aaa" {
		t.Fatal("expected lexicographic first real origin branch")
	}
}

// TestTagFallbackAndLocalTagsSurvive verifies tag fallback and that local tags stay in place.
func TestTagFallbackAndLocalTagsSurvive(t *testing.T) {
	f := setup(t)
	f.cfg.Branch = "absent"
	git(t, f.repo, "tag", "local-only")
	git(t, f.repo, "config", "fetch.pruneTags", "true")
	git(t, f.repo, "config", "remote.origin.pruneTags", "true")
	git(t, f.repo, "config", "--add", "remote.origin.fetch", "refs/tags/*:refs/tags/*")
	git(t, f.seed, "tag", "v1")
	git(t, f.seed, "push", "origin", "v1")

	table := Manifest{filepath.Base(f.repo): {Tag: "v1", Commit: "deadbeef"}}
	s, log, e := run(t, f, table)

	if e != nil || s.Updated != 1 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}

	if git(t, f.repo, "branch", "--show-current") != "" {
		t.Fatal("not detached")
	}

	git(t, f.repo, "show-ref", "--verify", "refs/tags/local-only")
}

// TestFallbackKeepsRenamedLocalBranch verifies that fallback does not rename the current local branch.
func TestFallbackKeepsRenamedLocalBranch(t *testing.T) {
	f := setup(t)
	git(t, f.repo, "branch", "-m", "my-local-name")

	f.cfg.Branch = "absent"
	git(t, f.seed, "commit", "--allow-empty", "-m", "next")
	git(t, f.seed, "push")

	s, log, e := run(t, f, nil)
	if e != nil || s.Updated != 1 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}

	if git(t, f.repo, "branch", "--show-current") != "my-local-name" {
		t.Fatal("renamed local branch switched")
	}

	if git(t, f.repo, "rev-parse", "HEAD") != git(t, f.seed, "rev-parse", "HEAD") {
		t.Fatal("not updated")
	}
}

// assertDirtyLeftUntouched checks that an unsafe worktree is not switched.
func assertDirtyLeftUntouched(t *testing.T, kind string) {
	t.Helper()

	f := setup(t)
	want := git(t, f.repo, "rev-parse", "HEAD")
	branch(t, f, "release")

	f.cfg.Branch = "release"
	want = makeUnsafeWorktree(t, f, kind, want)

	before := git(t, f.repo, "status", "--porcelain")
	s, log, e := run(t, f, nil)

	if e != nil || s.Skipped != 1 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}

	if git(t, f.repo, "rev-parse", "HEAD") != want || git(t, f.repo, "status", "--porcelain") != before {
		t.Fatal("unsafe repository changed")
	}

	if kind != "untracked" && kind != "staged" && kind != "unstaged" {
		return
	}

	name := "file"
	if kind == "untracked" {
		name = "untracked"
	}

	if !strings.Contains(log, "working tree has staged, unstaged or untracked changes\n  "+name) {
		t.Fatalf("missing differing files in %s", log)
	}
}

// makeUnsafeWorktree puts a clone into the requested unsafe state.
func makeUnsafeWorktree(t *testing.T, f *fixture, kind, head string) string {
	t.Helper()

	switch kind {
	case "untracked":
		write(t, filepath.Join(f.repo, "untracked"), "x")
	case "staged":
		write(t, filepath.Join(f.repo, "file"), "changed")
		git(t, f.repo, "add", "file")
	case "unstaged":
		write(t, filepath.Join(f.repo, "file"), "changed")
	case "unpushed":
		git(t, f.repo, "commit", "--allow-empty", "-m", "local")

		return git(t, f.repo, "rev-parse", "HEAD")
	case "no-upstream":
		git(t, f.repo, "branch", "--unset-upstream")
	case "detached":
		git(t, f.repo, "checkout", "--detach")
	case "merge":
		write(t, filepath.Join(f.repo, ".git", "MERGE_HEAD"), head+"\n")
	case "other-remote":
		git(t, f.repo, "remote", "add", "other", f.remote)
		git(t, f.repo, "fetch", "other")
		git(t, f.repo, "branch", "--set-upstream-to=other/master")
	case "missing-upstream":
		git(t, f.repo, "update-ref", "-d", "refs/remotes/origin/master")
	}

	return head
}
