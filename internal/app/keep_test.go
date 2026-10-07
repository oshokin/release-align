package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestKeepChangesOverlaysUnstagedEdits verifies that keep carries unstaged edits onto the updated branch.
func TestKeepChangesOverlaysUnstagedEdits(t *testing.T) {
	f := setup(t)
	want := branch(t, f, "release")
	write(t, filepath.Join(f.repo, "file"), "changed\n")
	git(t, f.repo, "add", "file")
	write(t, filepath.Join(f.repo, "extra"), "new\n")

	f.cfg.Branch = "release"
	f.cfg.Local = localKeep
	s, log, e := run(t, f, nil)

	if e != nil || s.Updated != 1 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}

	if git(t, f.repo, "rev-parse", "HEAD") != want || git(t, f.repo, "branch", "--show-current") != "release" {
		t.Fatal("branch was not updated")
	}

	git(t, f.repo, "diff", "--cached", "--quiet")

	if git(t, f.repo, "diff", "--", "file") == "" {
		t.Fatal("change was not unstaged")
	}

	if readFile(t, filepath.Join(f.repo, "file")) != "changed\n" ||
		readFile(t, filepath.Join(f.repo, "extra")) != "new\n" {
		t.Fatal("worktree contents changed")
	}

	if git(t, f.repo, "stash", "list") != "" || !strings.Contains(log, "local changes kept as unstaged edits") {
		t.Fatalf("stash or log: %s\n%s", git(t, f.repo, "stash", "list"), log)
	}
}

// TestKeepChangesDropsOnlyItsOwnStash verifies that keep drops only the stash it created.
func TestKeepChangesDropsOnlyItsOwnStash(t *testing.T) {
	f := setup(t)
	branch(t, f, "release")
	write(t, filepath.Join(f.repo, "file"), "once\n")
	git(t, f.repo, "stash", "push", "--include-untracked", "--message", "user-note")
	write(t, filepath.Join(f.repo, "file"), "twice\n")

	f.cfg.Branch = "release"
	f.cfg.Local = localKeep
	s, log, e := run(t, f, nil)

	if e != nil || s.Updated != 1 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}

	list := git(t, f.repo, "stash", "list")
	if strings.Contains(list, stashMessage) || !strings.Contains(list, "user-note") {
		t.Fatal(list)
	}

	if readFile(t, filepath.Join(f.repo, "file")) != "twice\n" {
		t.Fatal(readFile(t, filepath.Join(f.repo, "file")))
	}
}

// TestKeepChangesKeepsStashOnConflict verifies that a conflicting stash is left in place.
func TestKeepChangesKeepsStashOnConflict(t *testing.T) {
	f := setup(t)
	branch(t, f, "release")
	write(t, filepath.Join(f.seed, "file"), "origin\n")
	git(t, f.seed, "add", "file")
	git(t, f.seed, "commit", "-m", "origin edit")
	git(t, f.seed, "push")
	write(t, filepath.Join(f.repo, "file"), "local\n")

	f.cfg.Branch = "release"
	f.cfg.Local = localKeep
	s, log, e := run(t, f, nil)

	if e == nil || s.Failed != 1 || s.Updated != 0 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}

	if git(t, f.repo, "branch", "--show-current") != "release" {
		t.Fatal(git(t, f.repo, "branch", "--show-current"))
	}

	list := git(t, f.repo, "stash", "list")
	if !strings.Contains(list, stashMessage) || !strings.Contains(log, errStashKept.Error()) {
		t.Fatalf("stash or log:\n%s\n%s", list, log)
	}

	if !strings.Contains(readFile(t, filepath.Join(f.repo, "file")), "local") {
		t.Fatal(readFile(t, filepath.Join(f.repo, "file")))
	}
}

// TestKeepChangesRestoresWorktreeWhenSwitchRefuses verifies that a refused switch restores the worktree.
func TestKeepChangesRestoresWorktreeWhenSwitchRefuses(t *testing.T) {
	f := setup(t)
	branch(t, f, "release")
	write(t, filepath.Join(f.seed, "build.out"), "tracked\n")
	git(t, f.seed, "add", "build.out")
	git(t, f.seed, "commit", "-m", "track build output")
	git(t, f.seed, "push")

	exclude := filepath.Join(f.repo, ".git", "info", "exclude")
	body := append([]byte(readFile(t, exclude)), []byte("\nbuild.out\n")...)

	if err := os.WriteFile(exclude, body, 0o600); err != nil {
		t.Fatal(err)
	}

	write(t, filepath.Join(f.repo, "build.out"), "ignored\n")
	write(t, filepath.Join(f.repo, "file"), "changed\n")
	git(t, f.repo, "add", "file")

	f.cfg.Branch = "release"
	f.cfg.Local = localKeep
	s, log, e := run(t, f, nil)

	if e == nil || s.Failed != 1 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}

	if git(t, f.repo, "branch", "--show-current") != "master" {
		t.Fatal(log)
	}

	if git(t, f.repo, "diff", "--cached", "--", "file") == "" {
		t.Fatal("staging was not restored")
	}

	if readFile(t, filepath.Join(f.repo, "build.out")) != "ignored\n" || git(t, f.repo, "stash", "list") != "" {
		t.Fatal(git(t, f.repo, "stash", "list"))
	}
}

// TestKeepChangesDoesNotCarryUnpushedCommits verifies that keep does not move unpushed commits.
func TestKeepChangesDoesNotCarryUnpushedCommits(t *testing.T) {
	f := setup(t)
	branch(t, f, "release")
	git(t, f.repo, "commit", "--allow-empty", "-m", "local")

	head := git(t, f.repo, "rev-parse", "HEAD")
	f.cfg.Branch = "release"
	f.cfg.Local = localKeep
	s, log, e := run(t, f, nil)

	if e != nil || s.Skipped != 1 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}

	if git(t, f.repo, "rev-parse", "HEAD") != head || git(t, f.repo, "stash", "list") != "" {
		t.Fatal("unpushed repository was changed")
	}
}

// TestKeepChangesDryRunDoesNotStash verifies that a dry run does not create a stash.
func TestKeepChangesDryRunDoesNotStash(t *testing.T) {
	f := setup(t)
	branch(t, f, "release")
	git(t, f.repo, "fetch", "origin")
	write(t, filepath.Join(f.repo, "file"), "changed\n")

	before := git(t, f.repo, "status", "--porcelain")
	f.cfg.Branch = "release"
	f.cfg.Local = localKeep
	f.cfg.DryRun = true
	s, log, e := run(t, f, nil)

	if e != nil || s.Planned != 1 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}

	if git(t, f.repo, "status", "--porcelain") != before || git(t, f.repo, "stash", "list") != "" {
		t.Fatal("dry-run stashed or edited the worktree")
	}
}

// TestKeepChangesDetachesWithUnstagedEdits verifies that keep can detach at a tag and still overlay edits.
func TestKeepChangesDetachesWithUnstagedEdits(t *testing.T) {
	f := setup(t)
	f.cfg.Branch = "absent"
	f.cfg.Local = localKeep
	write(t, filepath.Join(f.repo, "file"), "changed\n")
	git(t, f.seed, "tag", "v1")
	git(t, f.seed, "push", "origin", "v1")

	table := Manifest{filepath.Base(f.repo): {Tag: "v1", Commit: "deadbeef"}}
	s, log, e := run(t, f, table)

	if e != nil || s.Updated != 1 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}

	if git(t, f.repo, "branch", "--show-current") != "" || readFile(t, filepath.Join(f.repo, "file")) != "changed\n" {
		t.Fatal("detach did not keep the edit")
	}

	git(t, f.repo, "diff", "--cached", "--quiet")

	if git(t, f.repo, "stash", "list") != "" {
		t.Fatal(git(t, f.repo, "stash", "list"))
	}
}

// readFile returns the contents of a test file.
func readFile(t *testing.T, path string) string {
	t.Helper()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}
