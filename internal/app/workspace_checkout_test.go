package app

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// TestWorkspacePinnedDetachedCheckoutIsIdempotent verifies that a detached HEAD already at the pin is left in place.
func TestWorkspacePinnedDetachedCheckoutIsIdempotent(t *testing.T) {
	f := setup(t)
	oid := branch(t, f, "Release-26.3.0")
	git(t, f.repo, "fetch", "origin")
	git(t, f.repo, "checkout", "--detach", oid)
	revision := &RevisionSpec{Commit: oid}
	spec := oneProject(t, "group/repo with spaces", revision)
	report, err := runWorkspace(t, f, spec, ModeSync)

	if err != nil || !report.Ready || report.Rows[0].Actual.Head != oid || report.Rows[0].Actual.Branch != "" {
		t.Fatalf("%+v %v", report, err)
	}

	report, err = runWorkspace(t, f, spec, ModeStatus)
	if err != nil || !report.Ready {
		t.Fatalf("status: %+v %v", report, err)
	}
}

// TestWorkspaceRefusesOtherDetachedHead verifies that some other detached HEAD is not switched.
func TestWorkspaceRefusesOtherDetachedHead(t *testing.T) {
	f := setup(t)
	current := git(t, f.repo, "rev-parse", "HEAD")
	oid := branch(t, f, "Release-26.3.0")
	git(t, f.repo, "checkout", "--detach", current)
	revision := &RevisionSpec{Commit: oid}
	spec := oneProject(t, "group/repo with spaces", revision)
	report, err := runWorkspace(t, f, spec, ModeSync)

	if ExitCodeForWorkspace(err) != 3 || report.Rows[0].ReasonCode != reasonDetached {
		t.Fatalf("%+v %v", report, err)
	}

	if git(t, f.repo, "rev-parse", "HEAD") != current {
		t.Fatal("detached commit was moved")
	}
}

// TestWorkspaceDryRunDoesNotMutate verifies that a workspace dry run does not fetch or switch.
func TestWorkspaceDryRunDoesNotMutate(t *testing.T) {
	f := setup(t)
	head := git(t, f.repo, "rev-parse", "HEAD")
	_ = branch(t, f, "Release-26.3.0")
	git(t, f.repo, "fetch", "origin")
	f.cfg.DryRun = true
	spec := oneProject(t, "group/repo with spaces", nil)
	spec.DefaultBranch = "Release-26.3.0"
	report, err := runWorkspace(t, f, spec, ModeSync)

	if err != nil || report.Ready || report.Mode != ModePlan || !report.DryRun || report.Freshness != freshnessCached {
		t.Fatalf("%+v %v", report, err)
	}

	if git(t, f.repo, "rev-parse", "HEAD") != head || git(t, f.repo, "status", "--porcelain") != "" {
		t.Fatal("dry-run changed the repository")
	}
}

// TestWorkspaceTargetChangeSkipsCheckout verifies that a moved target ref skips checkout.
func TestWorkspaceTargetChangeSkipsCheckout(t *testing.T) {
	f := setup(t)
	master := git(t, f.repo, "rev-parse", "HEAD")
	_ = branch(t, f, "Release-26.3.0")
	f.cfg.beforeCheckout = func() {
		git(t, f.repo, "update-ref", "refs/remotes/origin/Release-26.3.0", master)
	}
	spec := oneProject(t, "group/repo with spaces", nil)
	spec.DefaultBranch = "Release-26.3.0"
	report, err := runWorkspace(t, f, spec, ModeSync)

	if ExitCodeForWorkspace(err) != 3 || report.Rows[0].ReasonCode != reasonTargetChanged {
		t.Fatalf("%+v %v", report, err)
	}

	if git(t, f.repo, "rev-parse", "HEAD") != master {
		t.Fatal("checkout used a moved ref")
	}
}

// TestWorkspacePostCheckRejectsExternalEdit verifies that an edit after checkout fails the post-check.
func TestWorkspacePostCheckRejectsExternalEdit(t *testing.T) {
	f := setup(t)
	_ = branch(t, f, "Release-26.3.0")
	f.cfg.afterCheckout = func(dir string) {
		write(t, filepath.Join(dir, "sneaky"), "x")
	}
	spec := oneProject(t, "group/repo with spaces", nil)
	spec.DefaultBranch = "Release-26.3.0"
	report, err := runWorkspace(t, f, spec, ModeSync)

	if ExitCodeForWorkspace(err) != 3 || report.Ready || report.Rows[0].ReasonCode != reasonDirty {
		t.Fatalf("%+v %v", report, err)
	}
}

// TestWorkspaceRedactsCredentials verifies that credentials are removed from messages.
func TestWorkspaceRedactsCredentials(t *testing.T) {
	raw := "failed https://user:supersecret@gitlab.example/group/repo.git?private_token=abc123"
	got := redactGitText(raw)

	if strings.Contains(got, "supersecret") || strings.Contains(got, "abc123") || strings.Contains(got, "user:") {
		t.Fatal(got)
	}
}

// TestWorkspaceUnknownGroupIsUsage verifies that an unknown group is a usage error.
func TestWorkspaceUnknownGroupIsUsage(t *testing.T) {
	spec := oneProject(t, "group/repo with spaces", nil)
	f := setup(t)
	f.cfg.Groups = []string{"serach"}
	_, err := runWorkspace(t, f, spec, ModeSync)

	if ExitCodeForWorkspace(err) != 2 || !errors.Is(err, errWorkspaceUnknownGroup) {
		t.Fatal(err)
	}
}

// TestWorkspaceBranchOverrideKeepsExplicitRevision verifies that --branch does not replace an explicit tag or commit.
func TestWorkspaceBranchOverrideKeepsExplicitRevision(t *testing.T) {
	spec := &WorkspaceSpec{
		SchemaVersion: 1,
		DefaultBranch: "Release-26.3.0",
		Projects: []*ProjectSpec{
			{Path: "search/pasifae"},
			{Path: "storage/dos", Revision: &RevisionSpec{Tag: "v4.499.6"}},
		},
	}
	next, err := spec.WithDefaultBranch("Release-26.4.0")

	if err != nil || next.DefaultBranch != "Release-26.4.0" || spec.DefaultBranch != "Release-26.3.0" {
		t.Fatal(next, err)
	}

	if next.RevisionFor(next.Projects[0]).Branch != "Release-26.4.0" ||
		next.RevisionFor(next.Projects[1]).Tag != "v4.499.6" {
		t.Fatal(next.Projects)
	}
}
