package app

import (
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oshokin/release-align/internal/gitlab"
)

func TestCloneReusesThenClonesAndRepeats(t *testing.T) {
	f := setup(t)
	second := filepath.Join(f.base, "..", "second.git")
	git(t, filepath.Dir(f.base), "init", "--bare", "--initial-branch=master", second)
	seed := filepath.Join(filepath.Dir(f.base), "second-seed")
	git(t, filepath.Dir(f.base), "clone", second, seed)
	write(t, filepath.Join(seed, "file"), "next\n")
	git(t, seed, "add", "file")
	git(t, seed, "commit", "-m", "next")
	git(t, seed, "push", "-u", "origin", "master")
	git(t, f.repo, "remote", "set-url", "origin", f.remote)

	spec := oneProject(t, "group/repo with spaces", nil)
	existing := &gitlab.Project{
		ID:                1,
		PathWithNamespace: "group/repo with spaces",
		SSHURLToRepo:      f.remote,
		DefaultBranch:     "master",
	}
	indexer := &gitlab.Project{
		ID:                2,
		PathWithNamespace: "lamiona/search/new-indexer",
		SSHURLToRepo:      second,
		DefaultBranch:     "master",
	}
	projects := []*gitlab.Project{existing, indexer}
	server := catalogServer(t, projects)
	spec.GitLab = &GitLabSource{
		URL:    server.URL,
		Groups: []string{"group", "lamiona"},
	}
	file := saveWorkspace(t, spec)
	before := readBytes(t, file)
	head := git(t, f.repo, "rev-parse", "HEAD")
	opts := cloneOptions(t, f.base, file, server)
	opts.Repos = []string{"group/repo with spaces"}

	report, err := CloneWorkspace(t.Context(), opts)
	if err != nil || report.Reused != 1 || report.Cloned != 0 || git(t, f.repo, "rev-parse", "HEAD") != head {
		t.Fatal(err, report)
	}

	if string(readBytes(t, file)) != string(before) {
		t.Fatal("reuse rewrote the workspace")
	}

	opts.Repos = []string{"lamiona/search/new-indexer"}
	report, err = CloneWorkspace(t.Context(), opts)

	if err != nil || report.Cloned != 1 || report.Added != 1 {
		t.Fatal(err, report)
	}

	cloned := filepath.Join(f.base, "lamiona", "search", "new-indexer")
	if git(t, cloned, "rev-parse", "--abbrev-ref", "HEAD") != "master" {
		t.Fatal("clone did not check out the default branch")
	}

	again, err := CloneWorkspace(t.Context(), opts)
	if err != nil || again.Cloned != 0 || again.Reused != 1 {
		t.Fatal(err, again)
	}
}

func TestClonePartialKeepsFinishedCheckout(t *testing.T) {
	f := setup(t)
	good := filepath.Join(filepath.Dir(f.base), "good.git")
	git(t, filepath.Dir(f.base), "init", "--bare", "--initial-branch=master", good)
	seed := filepath.Join(filepath.Dir(f.base), "good-seed")
	git(t, filepath.Dir(f.base), "clone", good, seed)
	write(t, filepath.Join(seed, "file"), "good\n")
	git(t, seed, "add", "file")
	git(t, seed, "commit", "-m", "good")
	git(t, seed, "push", "-u", "origin", "master")

	spec := oneProject(t, "group/repo with spaces", nil)
	goodProject := &gitlab.Project{
		ID:                1,
		PathWithNamespace: "lamiona/a-good",
		SSHURLToRepo:      good,
		DefaultBranch:     "master",
	}
	badProject := &gitlab.Project{
		ID:                2,
		PathWithNamespace: "lamiona/b-bad",
		SSHURLToRepo:      filepath.Join(filepath.Dir(f.base), "missing-origin.git"),
		DefaultBranch:     "master",
	}
	projects := []*gitlab.Project{goodProject, badProject}
	server := catalogServer(t, projects)
	spec.GitLab = &GitLabSource{
		URL:    server.URL,
		Groups: []string{"lamiona"},
	}
	file := saveWorkspace(t, spec)
	opts := cloneOptions(t, f.base, file, server)
	opts.All = true
	report, err := CloneWorkspace(t.Context(), opts)

	if !errors.Is(err, errClonePartial) || report.Cloned != 1 {
		t.Fatal(err, report)
	}

	if _, statErr := os.Stat(filepath.Join(f.base, "lamiona", "a-good", ".git")); statErr != nil {
		t.Fatal(statErr)
	}

	body := string(readBytes(t, file))
	if !strings.Contains(body, "lamiona/a-good") || strings.Contains(body, "lamiona/b-bad") {
		t.Fatal(body)
	}
}

func TestCloneRejectsUnknownBeforeWrite(t *testing.T) {
	f := setup(t)
	spec := oneProject(t, "group/repo with spaces", nil)
	listed := remoteProject(1, "group/repo with spaces", "git@gitlab.example:group/repo with spaces.git")
	projects := []*gitlab.Project{listed}
	server := catalogServer(t, projects)
	spec.GitLab = &GitLabSource{
		URL:    server.URL,
		Groups: []string{"group"},
	}
	file := saveWorkspace(t, spec)
	before := readBytes(t, file)
	opts := cloneOptions(t, f.base, file, server)
	opts.Repos = []string{"lamiona/missing", "group/repo with spaces"}

	if _, err := CloneWorkspace(t.Context(), opts); !errors.Is(err, errCloneUnknown) {
		t.Fatal(err)
	}

	if string(readBytes(t, file)) != string(before) {
		t.Fatal("unknown selection wrote the workspace")
	}
}

func cloneOptions(t *testing.T, base, file string, server *httptest.Server) *WorkspaceCloneOptions {
	t.Helper()

	return &WorkspaceCloneOptions{
		BaseDir:       base,
		WorkspaceFile: file,
		Attempts:      1,
		ProbeTimeout:  time.Second,
		RetryDelay:    time.Millisecond,
		FetchTimeout:  5 * time.Second,
		LocalTimeout:  5 * time.Second,
		CloneTimeout:  time.Minute,
		hooks: &remoteHooks{
			token:           "secret",
			httpClient:      server.Client(),
			allowLocalClone: true,
		},
	}
}
