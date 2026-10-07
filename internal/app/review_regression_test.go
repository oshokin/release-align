package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReviewAuthIsolation requires independent authorization per repository.
func TestReviewAuthIsolation(t *testing.T) {
	f := setup(t)
	second := addClone(t, f, "z-allowed")
	other := filepath.Join(f.base, "group", second)
	git(t, f.repo, "remote", "set-url", "origin", "git@gitlab.example:denied.git")
	git(t, other, "remote", "set-url", "origin", "git@gitlab.example:allowed.git")
	calls := filepath.Join(t.TempDir(), "calls")
	body := fmt.Sprintf(
		"echo \"$*\" >> '%s'\ncase \"$*\" in *denied.git*) echo 'Permission denied (publickey)' >&2; exit 255;; esac\nexec git-upload-pack '%s'\n",
		calls,
		f.remote,
	)
	sshScript(t, body)
	spec := &WorkspaceSpec{
		SchemaVersion: 1,
		DefaultRevision: &RevisionSpec{
			Branch: "master",
		},
		Projects: []*ProjectSpec{
			{
				Path: "group/repo with spaces",
			},
			{
				Path: "group/z-allowed",
			},
		},
	}
	report, err := runWorkspace(t, f, spec, ModeSync)

	b, readErr := os.ReadFile(calls)
	if readErr != nil {
		t.Fatal(readErr)
	}

	t.Logf("err=%v calls=%s second=%+v", err, b, report.Rows[1])

	if err == nil || report.Ready || report.Rows[0].ReasonCode != reasonAuth ||
		report.Rows[1].ReasonCode != reasonPlanBlocked || !strings.Contains(string(b), "allowed.git") {
		t.Fatal("authorization failure from first repository poisoned second repository")
	}

	if git(t, f.repo, "branch", "--show-current") != "master" || git(t, other, "branch", "--show-current") != "master" {
		t.Fatal("strict auth failure still switched a repository")
	}
}

// TestReviewActualAfterFailedApply requires actual state to be read after partial mutation.
func TestReviewActualAfterFailedApply(t *testing.T) {
	f := setup(t)
	branch(t, f, "Release-26.3.0")
	git(t, f.repo, "fetch", "origin")
	git(t, f.repo, "branch", "Release-26.3.0", "HEAD")
	git(t, f.repo, "branch", "--set-upstream-to=origin/Release-26.3.0", "Release-26.3.0")
	write(t, filepath.Join(f.repo, ".git", "info", "exclude"), "release-file\n")

	const precious = "precious ignored file"

	write(t, filepath.Join(f.repo, "release-file"), precious)
	second := addClone(t, f, "z-later")
	later := filepath.Join(f.base, "group", second)
	spec := &WorkspaceSpec{
		SchemaVersion: 1,
		DefaultRevision: &RevisionSpec{
			Branch: "Release-26.3.0",
		},
		Projects: []*ProjectSpec{
			{
				Path: "group/repo with spaces",
			},
			{
				Path: "group/" + second,
			},
		},
	}
	report, err := runWorkspace(t, f, spec, ModeSync)
	actual := git(t, f.repo, "branch", "--show-current")
	head := git(t, f.repo, "rev-parse", "HEAD")
	kept, readErr := os.ReadFile(filepath.Join(f.repo, "release-file"))

	t.Logf("err=%v real_branch=%s reported=%+v row=%+v", err, actual, report.Rows[0].Actual, report.Rows[0])

	if readErr != nil || string(kept) != precious {
		t.Fatal("ignored file was replaced", readErr)
	}

	if report.Ready || report.Rows[0].Actual == nil || report.Rows[0].Actual.Branch != actual ||
		report.Rows[0].Actual.Head != head {
		t.Fatal("report contains state from before the failed apply")
	}

	if report.Rows[1].Outcome != outcomeNotStarted || report.Rows[1].ReasonCode != reasonPlanBlocked ||
		git(t, later, "branch", "--show-current") != "master" {
		t.Fatalf("later repository was started: %+v", report.Rows[1])
	}
}

// TestReviewConfirmFailureDropsStaleActual requires a failed final read to drop the pre-checkout observation.
func TestReviewConfirmFailureDropsStaleActual(t *testing.T) {
	f := setup(t)
	branch(t, f, "Release-26.3.0")
	f.cfg.afterCheckout = func(dir string) {
		if err := os.Remove(filepath.Join(dir, ".git", "HEAD")); err != nil {
			t.Errorf("remove HEAD: %v", err)
		}
	}
	spec := oneProject(t, "group/repo with spaces", nil)
	spec.DefaultRevision = &RevisionSpec{
		Branch: "Release-26.3.0",
	}
	report, err := runWorkspace(t, f, spec, ModeSync)

	if err == nil || report.Ready || report.Rows[0].Actual != nil {
		t.Fatalf("stale actual kept: err=%v row=%+v", err, report.Rows[0])
	}
}

// TestObserveFailedApplySkipsGitWhenCanceled keeps a dead context from publishing the old branch.
func TestObserveFailedApplySkipsGitWhenCanceled(t *testing.T) {
	item := &workspaceItem{
		row: &WorkspaceRow{
			Actual: &ObservedState{
				Branch:   "master",
				Verified: true,
			},
		},
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	r := new(runner)
	r.observeFailedApply(ctx, item)

	if item.row.Actual != nil {
		t.Fatal("canceled apply kept pre-checkout state")
	}
}
