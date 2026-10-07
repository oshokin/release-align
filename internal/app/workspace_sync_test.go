package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oshokin/release-align/internal/logger"
)

// TestWorkspaceSyncBlocksMissingRepositoryBeforeCheckout verifies that a missing clone blocks every checkout.
func TestWorkspaceSyncBlocksMissingRepositoryBeforeCheckout(t *testing.T) {
	f := setup(t)
	second := addClone(t, f, "beta")
	before := map[string]string{
		f.repo:                                 git(t, f.repo, "rev-parse", "HEAD"),
		filepath.Join(f.base, "group", second): git(t, filepath.Join(f.base, "group", second), "rev-parse", "HEAD"),
	}
	branch(t, f, "Release-26.3.0")

	spec := &WorkspaceSpec{
		SchemaVersion: 1,
		Release:       "Mailion 26.3.0",
		DefaultBranch: "Release-26.3.0",
		Projects: []*ProjectSpec{
			{Path: "group/repo with spaces", Groups: []string{"search"}},
			{Path: "group/" + second, Groups: []string{"search"}},
			{Path: "group/missing", Groups: []string{"search"}},
		},
	}
	report, err := runWorkspace(t, f, spec, ModeSync)

	if ExitCodeForWorkspace(err) != 3 || report.Ready || report.ExpectedCount != 3 || !report.Coverage {
		t.Fatalf("%+v %v", report, err)
	}

	var missing, blocked int

	for _, row := range report.Rows {
		switch row.ReasonCode {
		case reasonMissingRepository:
			missing++
		case reasonPlanBlocked:
			blocked++
		}
	}

	if missing != 1 || blocked != 2 {
		t.Fatalf("rows: %+v", report.Rows)
	}

	for dir, head := range before {
		if git(t, dir, "rev-parse", "HEAD") != head || git(t, dir, "branch", "--show-current") != "master" {
			t.Fatal("worktree changed despite a missing repository")
		}
	}
}

// TestWorkspaceSyncDoesNotFallBackWhenBranchIsAbsent verifies that a missing branch is not replaced by another ref.
func TestWorkspaceSyncDoesNotFallBackWhenBranchIsAbsent(t *testing.T) {
	f := setup(t)
	head := git(t, f.repo, "rev-parse", "HEAD")
	revision := &RevisionSpec{Branch: "Release-26.3.0"}
	spec := oneProject(t, "group/repo with spaces", revision)
	report, err := runWorkspace(t, f, spec, ModeSync)

	if ExitCodeForWorkspace(err) != 3 || report.Rows[0].ReasonCode != reasonMissingTarget {
		t.Fatalf("%+v %v", report, err)
	}

	if git(t, f.repo, "rev-parse", "HEAD") != head {
		t.Fatal("fallback changed HEAD")
	}
}

// TestWorkspaceSyncFastForwardsExactBranch verifies a fast-forward onto the exact requested branch.
func TestWorkspaceSyncFastForwardsExactBranch(t *testing.T) {
	f := setup(t)
	oid := branch(t, f, "Release-26.3.0")
	spec := oneProject(t, "group/repo with spaces", nil)
	spec.DefaultBranch = "Release-26.3.0"
	report, err := runWorkspace(t, f, spec, ModeSync)

	if err != nil || !report.Ready || report.Freshness != freshnessFetched {
		t.Fatalf("%+v %v", report, err)
	}

	row := report.Rows[0]
	if row.Actual.Head != oid || row.Expected.OID != oid || row.Actual.Branch != "Release-26.3.0" || row.Actual.Dirty {
		t.Fatalf("%+v", row)
	}

	if git(t, f.repo, "rev-parse", "HEAD") != oid {
		t.Fatal("HEAD was not the observed commit")
	}
}

// TestWorkspaceStatusSeesDirtyTreeWithoutNetwork verifies that status sees a dirty tree without contacting a remote.
func TestWorkspaceStatusSeesDirtyTreeWithoutNetwork(t *testing.T) {
	f := setup(t)
	oid := branch(t, f, "Release-26.3.0")
	git(t, f.repo, "fetch", "origin")
	git(t, f.repo, "switch", "--track", "origin/Release-26.3.0")
	write(t, filepath.Join(f.repo, "untracked"), "local")
	git(t, f.repo, "remote", "set-url", "origin", "git@192.0.2.1:missing/repo.git")

	spec := oneProject(t, "group/repo with spaces", nil)
	spec.DefaultBranch = "Release-26.3.0"
	report, err := runWorkspace(t, f, spec, ModeStatus)

	if ExitCodeForWorkspace(err) != 3 || report.Freshness != freshnessCached ||
		report.Rows[0].ReasonCode != reasonDirty {
		t.Fatalf("%+v %v", report, err)
	}

	if report.Rows[0].Actual.Head != oid || git(t, f.repo, "branch", "--show-current") != "Release-26.3.0" {
		t.Fatal("status changed the worktree or hid HEAD")
	}
}

// TestWorkspaceSelectionKeepsInventory verifies that a group filter still reports the full inventory size.
func TestWorkspaceSelectionKeepsInventory(t *testing.T) {
	f := setup(t)
	_ = branch(t, f, "Release-26.3.0")
	spec := &WorkspaceSpec{
		SchemaVersion: 1,
		DefaultBranch: "Release-26.3.0",
		Projects: []*ProjectSpec{
			{Path: "group/repo with spaces", Groups: []string{"search"}},
			{Path: "group/absent-one"},
			{Path: "group/absent-two"},
		},
	}
	f.cfg.Groups = []string{"search"}
	report, err := runWorkspace(t, f, spec, ModeSync)

	if err != nil || report.ExpectedCount != 1 || report.InventoryCount != 3 || report.Scope != scopeSelection ||
		!report.Ready {
		t.Fatalf("%+v %v", report, err)
	}
}

// runWorkspace executes workspace mode against a fixture.
func runWorkspace(t *testing.T, f *fixture, spec *WorkspaceSpec, mode string) (*WorkspaceReport, error) {
	t.Helper()

	buf := &strings.Builder{}
	log := logger.NewWithWriter(nil, buf)
	ctx := logger.ToContext(context.Background(), log)

	return RunWorkspace(ctx, f.cfg, spec, mode)
}

// oneProject builds a one-project workspace document.
func oneProject(t *testing.T, path string, revision *RevisionSpec) *WorkspaceSpec {
	t.Helper()

	return &WorkspaceSpec{
		SchemaVersion: 1,
		Release:       "test",
		DefaultBranch: "master",
		Projects:      []*ProjectSpec{{Path: path, Revision: revision}},
	}
}

// addClone adds another clone under the fixture base directory.
func addClone(t *testing.T, f *fixture, name string) string {
	t.Helper()

	dest := filepath.Join(f.base, "group", name)
	git(t, f.base, "clone", f.remote, dest)

	return name
}
