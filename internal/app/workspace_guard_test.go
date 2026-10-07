package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWorkspaceOtherWorktreeIsNotForced verifies that a branch checked out elsewhere is not taken over.
func TestWorkspaceOtherWorktreeIsNotForced(t *testing.T) {
	f := setup(t)
	oid := branch(t, f, "Release-26.3.0")
	git(t, f.repo, "fetch", "origin")
	git(t, f.repo, "branch", "Release-26.3.0", "origin/Release-26.3.0")

	other := filepath.Join(t.TempDir(), "other")
	git(t, f.repo, "worktree", "add", other, "Release-26.3.0")

	head := git(t, f.repo, "rev-parse", "HEAD")
	spec := oneProject(t, "group/repo with spaces", nil)
	spec.DefaultRevision = &RevisionSpec{
		Branch: "Release-26.3.0",
	}
	report, err := runWorkspace(t, f, spec, ModeSync)

	if err == nil || report.Ready {
		t.Fatalf("%+v %v", report, err)
	}

	if git(t, f.repo, "rev-parse", "HEAD") != head || git(t, other, "rev-parse", "HEAD") != oid {
		t.Fatal("worktree data changed")
	}

	if git(t, other, "branch", "--show-current") != "Release-26.3.0" {
		t.Fatal("other worktree left the branch")
	}
}

// TestWorkspaceReportsEveryMissingPath verifies a row for every missing repository.
func TestWorkspaceReportsEveryMissingPath(t *testing.T) {
	f := setup(t)
	head := git(t, f.repo, "rev-parse", "HEAD")
	projects := make([]*ProjectSpec, 60)
	projects[0] = &ProjectSpec{
		Path: "group/repo with spaces",
	}

	for i := 1; i < len(projects); i++ {
		projects[i] = &ProjectSpec{
			Path: fmt.Sprintf("missing/repo-%02d", i),
		}
	}

	spec := &WorkspaceSpec{
		SchemaVersion: 1,
		DefaultRevision: &RevisionSpec{
			Branch: "master",
		},
		Projects: projects,
	}
	report, err := runWorkspace(t, f, spec, ModeSync)

	if ExitCodeForWorkspace(err) != 3 || report.ExpectedCount != 60 || report.InventoryCount != 60 || !report.Coverage {
		t.Fatalf("%+v %v", report, err)
	}

	missing := 0

	for _, row := range report.Rows {
		if row.ReasonCode == reasonMissingRepository {
			missing++
		}
	}

	if missing != 59 || git(t, f.repo, "rev-parse", "HEAD") != head {
		t.Fatalf("missing=%d %+v", missing, report.Rows[0])
	}
}

// TestWorkspaceOutageStopsBeforeFetch verifies that an unreachable origin stops the run before fetch.
func TestWorkspaceOutageStopsBeforeFetch(t *testing.T) {
	f := setup(t)
	count := filepath.Join(t.TempDir(), "calls")
	sshScript(
		t,
		fmt.Sprintf("echo x >> '%s'\necho 'ssh: Could not resolve hostname gitlab.example' >&2\nexit 255\n", count),
	)
	git(t, f.repo, "remote", "set-url", "origin", "git@gitlab.example:group/repo.git")
	head := git(t, f.repo, "rev-parse", "HEAD")
	spec := oneProject(t, "group/repo with spaces", nil)
	report, err := runWorkspace(t, f, spec, ModeSync)

	if ExitCodeForWorkspace(err) != 1 || report.Freshness != freshnessCached ||
		report.Rows[0].ReasonCode != reasonNetwork {
		t.Fatalf("%+v %v", report, err)
	}

	body, readErr := os.ReadFile(count)
	if readErr != nil {
		t.Fatal(readErr)
	}

	if strings.Count(string(body), "x") != 3 || git(t, f.repo, "rev-parse", "HEAD") != head {
		t.Fatalf("calls=%s", body)
	}
}

// TestWorkspaceJSONRoundTripKeepsMessage verifies that a JSON round trip keeps the row message.
func TestWorkspaceJSONRoundTripKeepsMessage(t *testing.T) {
	f := setup(t)
	oid := branch(t, f, "Release-26.3.0")
	spec := oneProject(t, "group/repo with spaces", nil)
	spec.DefaultRevision = &RevisionSpec{
		Branch: "Release-26.3.0",
	}

	report, err := runWorkspace(t, f, spec, ModeSync)
	if err != nil {
		t.Fatal(err)
	}

	var buf strings.Builder

	if err = WriteWorkspaceReport(&buf, report); err != nil || !json.Valid([]byte(buf.String())) {
		t.Fatal(err)
	}

	if !strings.Contains(buf.String(), oid) || strings.Contains(buf.String(), "\x1b") {
		t.Fatal(buf.String())
	}
}
