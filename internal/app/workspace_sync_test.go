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
		Release:       "Lamiona 26.3.0",
		DefaultRevision: &RevisionSpec{
			Branch: "Release-26.3.0",
		},
		Projects: []*ProjectSpec{
			{
				Path:   "group/repo with spaces",
				Groups: []string{"search"},
			},
			{
				Path:   "group/" + second,
				Groups: []string{"search"},
			},
			{
				Path:   "group/missing",
				Groups: []string{"search"},
			},
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
	revision := &RevisionSpec{
		Branch: "Release-26.3.0",
	}
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
	spec.DefaultRevision = &RevisionSpec{
		Branch: "Release-26.3.0",
	}
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

// TestWorkspaceSyncLeavesLocalBranchAndChecksOutPin moves HEAD off an unpublished branch.
func TestWorkspaceSyncLeavesLocalBranchAndChecksOutPin(t *testing.T) {
	f := setup(t)
	git(t, f.repo, "switch", "-c", "local-work")
	write(t, filepath.Join(f.repo, "local"), "kek\n")
	git(t, f.repo, "add", "local")
	git(t, f.repo, "commit", "-m", "kek")
	kept := git(t, f.repo, "rev-parse", "HEAD")
	write(t, filepath.Join(f.seed, "file"), "server\n")
	git(t, f.seed, "add", "file")
	git(t, f.seed, "commit", "-m", "server")
	git(t, f.seed, "push", "origin", "master")
	want := git(t, f.seed, "rev-parse", "HEAD")

	spec := oneProject(t, "group/repo with spaces", nil)
	report, err := runWorkspace(t, f, spec, ModeSync)

	if err != nil || !report.Ready || report.Rows[0].Actual.Branch != "master" || report.Rows[0].Actual.Head != want {
		t.Fatalf("%+v %v", report, err)
	}

	if git(t, f.repo, "rev-parse", "refs/heads/local-work") != kept {
		t.Fatal("local branch lost its commit")
	}
}

// TestWorkspaceSyncRefusesUnpushedCommitsOnThePinnedBranch leaves a diverged pin untouched.
func TestWorkspaceSyncRefusesUnpushedCommitsOnThePinnedBranch(t *testing.T) {
	f := setup(t)
	write(t, filepath.Join(f.repo, "local"), "mine\n")
	git(t, f.repo, "add", "local")
	git(t, f.repo, "commit", "-m", "mine")
	head := git(t, f.repo, "rev-parse", "HEAD")

	spec := oneProject(t, "group/repo with spaces", nil)
	report, err := runWorkspace(t, f, spec, ModeSync)

	if ExitCodeForWorkspace(err) != 3 || report.Rows[0].ReasonCode != reasonUnpushed {
		t.Fatalf("%+v %v", report, err)
	}

	if git(t, f.repo, "rev-parse", "HEAD") != head || git(t, f.repo, "branch", "--show-current") != "master" {
		t.Fatal("diverged pin moved")
	}
}

// TestWorkspaceSyncMovesATagFromOrigin overwrites a tag that points at a different commit.
func TestWorkspaceSyncMovesATagFromOrigin(t *testing.T) {
	f := setup(t)
	git(t, f.seed, "tag", "v1")
	git(t, f.seed, "push", "origin", "v1")
	git(t, f.repo, "fetch", "origin", "tag", "v1")
	git(t, f.repo, "tag", "local-only")
	localOnly := git(t, f.repo, "rev-parse", "local-only^{}")
	write(t, filepath.Join(f.seed, "file"), "moved\n")
	git(t, f.seed, "add", "file")
	git(t, f.seed, "commit", "-m", "moved")
	git(t, f.seed, "tag", "--force", "v1")
	git(t, f.seed, "push", "--force", "origin", "v1")
	git(t, f.seed, "push", "origin", "master")
	want := git(t, f.seed, "rev-parse", "HEAD")

	spec := oneProject(t, "group/repo with spaces", nil)
	report, err := runWorkspace(t, f, spec, ModeSync)

	if err != nil || !report.Ready || report.Rows[0].Actual.Head != want {
		t.Fatalf("%+v %v", report, err)
	}

	if git(t, f.repo, "rev-parse", "v1^{}") != want {
		t.Fatal("origin tag was left behind")
	}

	if git(t, f.repo, "rev-parse", "local-only^{}") != localOnly {
		t.Fatal("local-only tag was removed")
	}
}

// TestWorkspaceSyncFastForwardsWithoutAnUpstream updates a branch that can move and has no upstream.
func TestWorkspaceSyncFastForwardsWithoutAnUpstream(t *testing.T) {
	f := setup(t)
	git(t, f.repo, "branch", "--unset-upstream")
	write(t, filepath.Join(f.seed, "file"), "server\n")
	git(t, f.seed, "add", "file")
	git(t, f.seed, "commit", "-m", "server")
	git(t, f.seed, "push", "origin", "master")
	want := git(t, f.seed, "rev-parse", "HEAD")

	spec := oneProject(t, "group/repo with spaces", nil)
	report, err := runWorkspace(t, f, spec, ModeSync)

	if err != nil || !report.Ready || report.Rows[0].Actual.Head != want || report.Rows[0].Actual.Branch != "master" {
		t.Fatalf("%+v %v", report, err)
	}
}

// TestWorkspaceSyncIgnoreErrorsUpdatesTheOthers checks out the repositories that can move.
func TestWorkspaceSyncIgnoreErrorsUpdatesTheOthers(t *testing.T) {
	f := setup(t)
	second := addClone(t, f, "beta")
	other := filepath.Join(f.base, "group", second)
	before := git(t, f.repo, "rev-parse", "HEAD")
	write(t, filepath.Join(f.seed, "file"), "server\n")
	git(t, f.seed, "add", "file")
	git(t, f.seed, "commit", "-m", "server")
	git(t, f.seed, "push", "origin", "master")
	want := git(t, f.seed, "rev-parse", "HEAD")
	write(t, filepath.Join(f.repo, "dirty"), "local\n")
	f.cfg.IgnoreErrors = true

	spec := &WorkspaceSpec{
		SchemaVersion: 1,
		DefaultRevision: &RevisionSpec{
			Branch: "master",
		},
		Projects: []*ProjectSpec{
			{Path: "group/repo with spaces"},
			{Path: "group/" + second},
		},
	}
	report, err := runWorkspace(t, f, spec, ModeSync)

	if ExitCodeForWorkspace(err) != 3 || report.Ready || len(report.Rows) != 2 {
		t.Fatalf("%+v %v", report, err)
	}

	var dirtyRow, cleanRow *WorkspaceRow

	for _, row := range report.Rows {
		switch row.Path {
		case "group/repo with spaces":
			dirtyRow = row
		case "group/" + second:
			cleanRow = row
		}
	}

	if dirtyRow == nil || dirtyRow.ReasonCode != reasonDirty || git(t, f.repo, "rev-parse", "HEAD") != before {
		t.Fatalf("dirty repo moved: %+v", dirtyRow)
	}

	moved := cleanRow != nil && cleanRow.Actual != nil && cleanRow.Actual.Head == want
	if !moved || git(t, other, "rev-parse", "HEAD") != want || cleanRow.ReasonCode == reasonPlanBlocked {
		t.Fatalf("clean repo stayed: %+v", cleanRow)
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
	spec.DefaultRevision = &RevisionSpec{
		Branch: "Release-26.3.0",
	}
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
		DefaultRevision: &RevisionSpec{
			Branch: "Release-26.3.0",
		},
		Projects: []*ProjectSpec{
			{
				Path:   "group/repo with spaces",
				Groups: []string{"search"},
			},
			{
				Path: "group/absent-one",
			},
			{
				Path: "group/absent-two",
			},
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

	buf := new(strings.Builder)
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
		DefaultRevision: &RevisionSpec{
			Branch: "master",
		},
		Projects: []*ProjectSpec{
			{
				Path:     path,
				Revision: revision,
			},
		},
	}
}

// addClone adds another clone under the fixture base directory.
func addClone(t *testing.T, f *fixture, name string) string {
	t.Helper()

	dest := filepath.Join(f.base, "group", name)
	git(t, f.base, "clone", f.remote, dest)

	return name
}
