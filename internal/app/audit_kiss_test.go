package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oshokin/release-align/internal/gitlab"
)

// TestAuditDeleteRefusesAParentOfAKeptProject leaves the nested checkout and the file in place.
func TestAuditDeleteRefusesAParentOfAKeptProject(t *testing.T) {
	f := setup(t)
	kept := filepath.Join(f.base, "group", "old", "kept")

	if err := os.MkdirAll(kept, 0o700); err != nil {
		t.Fatal(err)
	}

	write(t, filepath.Join(kept, "stay"), "x")
	file := remoteFile(t, f, remoteProjects("group/old", "group/old/kept"))
	before := readBytes(t, file)
	options := remoteOptions(f, true, true, "group/old/kept")

	_, err := RefreshWorkspace(t.Context(), offlineClient(f), file, options)
	if !errors.Is(err, ErrWorkspaceRefreshUsage) || !errors.Is(err, errWorkspaceRefreshDeleteNested) ||
		!sameBytes(readBytes(t, file), before) {
		t.Fatal(err)
	}

	if _, statErr := os.Stat(filepath.Join(kept, "stay")); statErr != nil {
		t.Fatal(statErr)
	}
}

// TestAuditStashRefusesANestedDirectory does not stash a file outside the named subdirectory.
func TestAuditStashRefusesANestedDirectory(t *testing.T) {
	f := setup(t)
	nested := filepath.Join(f.repo, "nested")

	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}

	write(t, filepath.Join(f.repo, "file"), "outside\n")
	file := stashFile(t, "group/repo with spaces/nested")

	_, err := StashWorkspace(t.Context(), stashGit(f), file, &WorkspaceStashOptions{BaseDir: f.base})
	if !errors.Is(err, errWorkspaceRoot) || git(t, f.repo, "stash", "list") != "" {
		t.Fatal(err)
	}

	if !strings.Contains(git(t, f.repo, "status", "--porcelain"), "file") {
		t.Fatal("parent worktree was stashed")
	}
}

// TestAuditStashRefusesADifferentOrigin leaves the dirty file in the worktree.
func TestAuditStashRefusesADifferentOrigin(t *testing.T) {
	f := setup(t)
	git(t, f.repo, "remote", "set-url", "origin", "git@gitlab.example:group/repo.git")
	write(t, filepath.Join(f.repo, "file"), "outside\n")

	spec := &WorkspaceSpec{
		SchemaVersion: 1,
		DefaultRevision: &RevisionSpec{
			Branch: "master",
		},
		Projects: []*ProjectSpec{{
			Path: "group/repo with spaces",
			URL:  "https://gitlab.example/other/repo.git",
		}},
	}
	file := saveWorkspace(t, spec)

	_, err := StashWorkspace(t.Context(), stashGit(f), file, &WorkspaceStashOptions{BaseDir: f.base})
	if err == nil || !strings.Contains(err.Error(), "origin does not match") ||
		git(t, f.repo, "stash", "list") != "" {
		t.Fatal(err)
	}
}

// TestAuditAppendAcceptsACommitPin keeps a branch pin out and stores a full object id.
func TestAuditAppendAcceptsACommitPin(t *testing.T) {
	current := &WorkspaceSpec{
		SchemaVersion: 1,
		DefaultRevision: &RevisionSpec{
			Branch: "master",
		},
		Projects: []*ProjectSpec{{Path: "old"}},
	}
	branch := &ProjectSpec{
		Path:     "branched",
		Revision: &RevisionSpec{Branch: "invented"},
	}

	if _, err := AppendDiscoveredProjects(current, []*ProjectSpec{branch}); err == nil {
		t.Fatal("accepted a branch pin")
	}

	oid := "0123456789abcdef0123456789abcdef01234567"
	pinned := &ProjectSpec{
		Path:     "pinned",
		Revision: &RevisionSpec{Commit: oid},
	}

	appended, err := AppendDiscoveredProjects(current, []*ProjectSpec{pinned})
	if err != nil || len(appended.added) != 1 || appended.spec.Projects[1].Revision.Commit != oid {
		t.Fatal(err, appended)
	}
}

// TestAuditArchiveSkipsOriginWhenTheURLIsEmpty packs a local checkout that has no origin.
func TestAuditArchiveSkipsOriginWhenTheURLIsEmpty(t *testing.T) {
	f := setup(t)
	oid := git(t, f.repo, "rev-parse", "HEAD")
	git(t, f.repo, "remote", "remove", "origin")
	spec := archiveProject(&RevisionSpec{Commit: oid})
	file := writeWorkspace(t, f.base, spec)
	dest := filepath.Join(f.base, "out.zip")
	in := &archiveOptInput{fixture: f, workspace: file, dest: dest}

	if _, err := ArchiveWorkspace(t.Context(), archiveOpts(in)); err != nil {
		t.Fatal(err)
	}

	if zipText(t, dest, "group/repo with spaces/file") != "initial\n" {
		t.Fatal("empty URL still required origin")
	}
}

// TestAuditCatalogSplitsArchivedClones keeps an archived gap out of the active clone hint.
func TestAuditCatalogSplitsArchivedClones(t *testing.T) {
	spec := &WorkspaceSpec{
		Projects: []*ProjectSpec{{Path: "group/old"}},
		GitLab: &GitLabSource{
			URL:    "https://gitlab.example",
			Groups: []string{"group"},
		},
	}
	missing := remoteProject(1, "group/missing", "git@gitlab.example:group/missing.git")
	missing.Archived = true
	live := remoteProject(2, "group/live", "git@gitlab.example:group/live.git")
	old := remoteProject(3, "group/old", "git@gitlab.example:group/old.git")
	old.Archived = true
	diff := newRemoteDiff(&remoteDiffQuery{spec: spec, base: t.TempDir()})
	diff.classify(missing)
	diff.classify(live)
	diff.classify(old)
	diff.finish()

	if !samePaths(diff.catalog.NotCloned, []string{"group/live"}) ||
		!samePaths(diff.catalog.BecameArchived, []string{"group/old"}) ||
		!containsPath(diff.catalog.NotClonedArchived, "group/missing") ||
		containsPath(diff.catalog.NotCloned, "group/missing") {
		t.Fatal(diff.catalog)
	}

	inventory := checkedInventory(spec.GitLab, diff.catalog)
	if inventory.IncludeArchived == nil || !*inventory.IncludeArchived {
		t.Fatal(inventory.IncludeArchived)
	}

	report := &WorkspaceReport{RemoteInventory: inventory}
	cfg := &Config{WorkspaceFile: "workspace.yml", BaseDir: "/work"}

	var out bytes.Buffer

	if err := WriteRemoteInventory(&out, cfg, report); err != nil {
		t.Fatal(err)
	}

	text := out.String()
	if !strings.Contains(text, "Not cloned, archived:") || !strings.Contains(text, "--include-archived") ||
		strings.Contains(text, "No project differences.") {
		t.Fatal(text)
	}
}

// TestAuditRefreshPreviewShowsArchiveState leaves the file unchanged.
func TestAuditRefreshPreviewShowsArchiveState(t *testing.T) {
	f := setup(t)
	file := kissStateFile(t)
	before := readBytes(t, file)
	options := &WorkspaceRefreshOptions{
		BaseDir: f.base,
		Remote:  true,
		catalog: &refreshCatalog{projects: kissArchiveCatalog()},
	}

	result, err := RefreshWorkspace(t.Context(), offlineClient(f), file, options)
	if err != nil || result.Written || !samePaths(result.BecameArchived, []string{"group/old"}) ||
		!samePaths(result.BecameActive, []string{"group/live"}) || !sameBytes(readBytes(t, file), before) {
		t.Fatal(result, err)
	}
}

// TestAuditOpaqueUserdataDoesNotBlockANeighborMark stores the archived neighbor and keeps the scalar.
func TestAuditOpaqueUserdataDoesNotBlockANeighborMark(t *testing.T) {
	f := setup(t)
	file := kissOpaqueFile(t)
	options := &WorkspaceRefreshOptions{
		BaseDir: f.base,
		Remote:  true,
		Apply:   true,
		catalog: &refreshCatalog{projects: kissArchiveCatalog()},
	}

	result, err := RefreshWorkspace(t.Context(), offlineClient(f), file, options)
	if err != nil || !result.Written {
		t.Fatal(result, err)
	}

	text := string(readBytes(t, file))
	if !strings.Contains(text, "userdata: opaque") || !strings.Contains(text, "archived: true") ||
		!strings.Contains(text, "path: group/old") {
		t.Fatal(text)
	}
}

// TestAuditArchivedCloneStoresTheMarkAndTheDefaultBranch pins a reused checkout to origin.
func TestAuditArchivedCloneStoresTheMarkAndTheDefaultBranch(t *testing.T) {
	f := setup(t)
	remote := filepath.Join(filepath.Dir(f.remote), "old.git")
	git(t, filepath.Dir(f.base), "init", "--bare", "--initial-branch=master", remote)
	rel := "group/old"
	dir := filepath.Join(f.base, "group", "old")
	git(t, f.base, "clone", remote, dir)
	write(t, filepath.Join(dir, "file"), "seed\n")
	git(t, dir, "add", "file")
	git(t, dir, "commit", "-m", "seed")
	git(t, dir, "push", "-u", "origin", "master")
	git(t, dir, "switch", "-c", "side")
	write(t, filepath.Join(dir, "file"), "local\n")
	git(t, dir, "add", "file")
	git(t, dir, "commit", "-m", "local")
	head := git(t, dir, "rev-parse", "HEAD")
	origin := git(t, dir, "rev-parse", "refs/remotes/origin/master")

	if head == origin {
		t.Fatal("local HEAD matches origin")
	}

	listed := remoteProject(1, "group/repo with spaces", f.remote)
	listed.Archived = true
	archived := remoteProject(2, rel, remote)
	archived.Archived = true
	projects := []*gitlab.Project{listed, archived}
	server := catalogServer(t, projects)
	spec := oneProject(t, "group/repo with spaces", nil)
	spec.Projects[0].Archived = false
	spec.GitLab = &GitLabSource{
		URL:    server.URL,
		Groups: []string{"group"},
	}
	file := saveWorkspace(t, spec)
	opts := cloneOptions(t, f.base, file, server)
	opts.IncludeArchived = true
	opts.Repos = []string{rel, "group/repo with spaces"}

	if _, err := CloneWorkspace(t.Context(), opts); err != nil {
		t.Fatal(err)
	}

	text := string(readBytes(t, file))
	if !strings.Contains(text, "archived: true") || !strings.Contains(text, origin) || strings.Contains(text, head) {
		t.Fatal(text)
	}
}

func kissStateFile(t *testing.T) string {
	t.Helper()

	body := "manifest:\n  defaults:\n    revision: refs/heads/master\n  projects:\n" +
		"    - name: live\n      path: group/live\n      userdata:\n        release-align:\n          archived: true\n" +
		"    - name: old\n      path: group/old\n" +
		"release-align:\n  schema-version: 1\n  gitlab:\n    url: https://gitlab.example\n" +
		"    groups:\n      - group\n    clone-protocol: ssh\n"

	return kissWrite(t, body)
}

func kissOpaqueFile(t *testing.T) string {
	t.Helper()

	body := "manifest:\n  defaults:\n    revision: refs/heads/master\n  projects:\n" +
		"    - name: live\n      path: group/live\n      userdata: opaque\n" +
		"    - name: old\n      path: group/old\n" +
		"release-align:\n  schema-version: 1\n  gitlab:\n    url: https://gitlab.example\n" +
		"    groups:\n      - group\n    clone-protocol: ssh\n"

	return kissWrite(t, body)
}

func kissWrite(t *testing.T, body string) string {
	t.Helper()

	file := filepath.Join(t.TempDir(), "workspace.yml")
	write(t, file, body)

	return file
}

func containsPath(paths []string, path string) bool {
	return slices.Contains(paths, path)
}

func kissArchiveCatalog() []*gitlab.Project {
	live := remoteProject(1, "group/live", "")
	old := remoteProject(2, "group/old", "")
	old.Archived = true

	return []*gitlab.Project{live, old}
}
