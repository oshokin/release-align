package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oshokin/release-align/internal/gitlab"
)

func TestCompletionListedArchivedActuallyClones(t *testing.T) {
	f := setup(t)
	path := "group/archived"

	git(t, f.repo, "remote", "set-url", "origin", "git@unrelated.invalid:other/service.git")

	spec := oneProject(t, path, nil)
	project := remoteProject(1, path, f.remote)
	project.Archived = true
	server := catalogServer(t, []*gitlab.Project{project})
	spec.GitLab = &GitLabSource{URL: server.URL, Groups: []string{"group"}}
	file := saveWorkspace(t, spec)
	opts := cloneOptions(t, f.base, file, server)
	opts.Repos = []string{path}
	opts.IncludeArchived = true

	report, err := CloneWorkspace(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}

	_, statErr := os.Stat(filepath.Join(f.base, filepath.FromSlash(path), gitMetaDir))
	if report.Cloned != 1 || report.Reused != 0 || statErr != nil {
		t.Fatalf(
			"missing archived checkout reported as success: cloned=%d reused=%d stat=%v",
			report.Cloned,
			report.Reused,
			statErr,
		)
	}
}

func TestCompletionDeletePreservesUnlistedNestedCheckout(t *testing.T) {
	f := setup(t)
	parent := "group/old"
	child := parent + "/unlisted"
	childDir := filepath.Join(f.base, filepath.FromSlash(child))

	if err := os.MkdirAll(childDir, 0o700); err != nil {
		t.Fatal(err)
	}

	git(t, filepath.Dir(childDir), "init", "--initial-branch=master")
	git(t, childDir, "init", "--initial-branch=master")
	write(t, filepath.Join(childDir, "keep-me"), "unlisted local work\n")

	file := remoteFile(t, f, remoteProjects(parent, "group/repo with spaces"))
	before := readBytes(t, file)
	opts := remoteOptions(f, true, true, "group/repo with spaces")
	report, err := RefreshWorkspace(t.Context(), offlineClient(f), file, opts)
	_, statErr := os.Stat(filepath.Join(childDir, "keep-me"))

	if err == nil || statErr != nil || !bytes.Equal(before, readBytes(t, file)) {
		t.Fatalf(
			"unlisted nested checkout must block deletion before YAML write: report=%+v err=%v stat=%v",
			report,
			err,
			statErr,
		)
	}
}

func TestCompletionRemoteRunRetainsArchiveDiff(t *testing.T) {
	f := setup(t)
	path := "group/repo with spaces"
	project := remoteProject(1, path, f.remote)
	project.Archived = true
	server := catalogServer(t, []*gitlab.Project{project})
	spec := oneProject(t, path, nil)
	spec.GitLab = &GitLabSource{URL: server.URL, Groups: []string{"group"}}
	f.cfg.Remote = true
	f.cfg.remoteHooks = &remoteHooks{token: "secret", httpClient: server.Client(), allowLocalClone: true}

	report, err := runWorkspace(t, f, spec, ModeStatus)
	if report == nil || report.RemoteInventory == nil || report.RemoteInventory.Catalog == nil {
		t.Fatal(report, err)
	}

	catalog := report.RemoteInventory.Catalog
	if !samePaths(catalog.BecameArchived, []string{path}) {
		t.Fatalf(
			"active->archived lost during real status --remote: became_archived=%v saved project now archived=%v runErr=%v",
			catalog.BecameArchived,
			spec.Projects[0].Archived,
			err,
		)
	}
}

func TestCompletionMixedReadinessUsesActiveDenominator(t *testing.T) {
	f := setup(t)
	spec := oneProject(t, "group/repo with spaces", nil)
	archived := &ProjectSpec{Path: "group/archived", Archived: true}
	spec.Projects = append(spec.Projects, archived)

	report, err := runWorkspace(t, f, spec, ModeStatus)
	if err != nil || !report.Ready || report.ActionableCount != 1 || report.SkippedArchivedCount != 1 {
		t.Fatal(report, err)
	}

	var out bytes.Buffer

	if err = WriteRemoteInventory(&out, f.cfg, report); err != nil {
		t.Fatal(err)
	}

	text := out.String()
	if !strings.Contains(text, "Active: 1/1") || !strings.Contains(text, "archived skipped: 1") ||
		strings.Count(text, remoteNotCheckedMsg) != 1 {
		t.Fatalf("JSON ready=true actionable=1 skipped=1, text=%q", text)
	}
}

func TestCompletionFreshArchivedPinChecksDefaultBranch(t *testing.T) {
	f := setup(t)
	expected := git(t, f.repo, "rev-parse", "refs/remotes/origin/master")

	git(t, f.repo, "switch", "-c", "other-default")
	write(t, filepath.Join(f.repo, "file"), "different remote HEAD\n")
	git(t, f.repo, "add", "file")
	git(t, f.repo, "commit", "-m", "different default")
	git(t, f.repo, "push", "origin", "other-default")
	git(t, f.remote, "symbolic-ref", "HEAD", "refs/heads/other-default")

	fresh := filepath.Join(t.TempDir(), "fresh")
	git(t, f.base, "clone", f.remote, fresh)

	project := &gitlab.Project{DefaultBranch: "master", Archived: true}
	item := &cloneItem{path: "group/fresh", project: project}
	job := &cloneJob{client: offlineClient(f)}
	actual, err := job.archivedPin(t.Context(), item, fresh)

	if err == nil && strings.TrimSpace(actual) != expected {
		t.Fatalf("fresh clone with HEAD != catalog default accepted: HEAD=%s origin/master=%s", actual, expected)
	}
}
