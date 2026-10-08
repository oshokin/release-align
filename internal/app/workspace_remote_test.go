package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oshokin/release-align/internal/gitlab"
)

func TestAppendKeepsGitLabSource(t *testing.T) {
	spec := &WorkspaceSpec{
		SchemaVersion: 1,
		DefaultRevision: &RevisionSpec{
			Branch: "master",
		},
		GitLab: &GitLabSource{
			URL:    "https://gitlab.example",
			Groups: []string{"lamiona"},
		},
		Projects: []*ProjectSpec{{
			Path: "lamiona/search/calyra",
		}},
	}
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}

	discovered := &ProjectSpec{
		Path:   "lamiona/search/new-indexer",
		Groups: []string{"lamiona", "lamiona/search"},
	}
	appended, err := AppendDiscoveredProjects(spec, []*ProjectSpec{discovered})

	if err != nil || appended == nil {
		t.Fatal(err)
	}

	next := appended.spec
	added := appended.added

	if len(added) != 1 || next.GitLab == nil || next.GitLab.URL != spec.GitLab.URL {
		t.Fatal(err, added, next.GitLab)
	}

	next.GitLab.Groups[0] = "changed"
	if spec.GitLab.Groups[0] != "lamiona" {
		t.Fatal(spec.GitLab.Groups)
	}
}

func TestGitLabSourceCloneDoesNotShareGroups(t *testing.T) {
	spec := &WorkspaceSpec{
		SchemaVersion: 1,
		DefaultRevision: &RevisionSpec{
			Branch: "master",
		},
		GitLab: &GitLabSource{
			URL:    "https://gitlab.example",
			Groups: []string{"lamiona", "lamiona"},
		},
		Projects: []*ProjectSpec{{
			Path: "lamiona/search/calyra",
		}},
	}
	if err := spec.Validate(); err != nil || spec.GitLab.CloneProtocol != cloneProtocolSSH ||
		len(spec.GitLab.Groups) != 1 {
		t.Fatal(err, spec.GitLab)
	}

	next, err := spec.WithDefaultBranch("release")
	if err != nil {
		t.Fatal(err)
	}

	next.GitLab.Groups[0] = "changed"
	if spec.GitLab.Groups[0] != "lamiona" {
		t.Fatal(spec.GitLab.Groups)
	}
}

func TestRemoteIdentityIgnoresUserAndKeepsPort(t *testing.T) {
	left, ok := parseRemote("git@GitLab.Example:Lamiona/Search/Calyra.git")
	right, okHTTPS := parseRemote("https://gitlab.example/Lamiona/Search/Calyra")
	ported, okPort := parseRemote("ssh://git@gitlab.example:2222/Lamiona/Search/Calyra.git")
	_, fileOK := parseRemote("file:///tmp/repo.git")

	if !ok || !okHTTPS || left == nil || right == nil || left.host != right.host || left.path != right.path ||
		!okPort || ported == nil || ported.host == left.host || fileOK {
		t.Fatalf("%v %v %v file %v", left, right, ported, fileOK)
	}
}

func TestStatusRemoteReportsDiffWithoutCloning(t *testing.T) {
	f := setup(t)
	git(t, f.repo, "remote", "set-url", "origin", "git@gitlab.example:group/repo with spaces.git")
	listed := remoteProject(1, "group/repo with spaces", "git@gitlab.example:group/repo with spaces.git")
	indexer := remoteProject(2, "lamiona/search/new-indexer", "git@gitlab.example:lamiona/search/new-indexer.git")
	cleaner := remoteProject(3, "lamiona/auth/token-cleaner", "git@gitlab.example:lamiona/auth/token-cleaner.git")
	projects := []*gitlab.Project{listed, indexer, cleaner}
	server := catalogServer(t, projects)
	spec := oneProject(t, "group/repo with spaces", nil)
	spec.GitLab = &GitLabSource{
		URL:    server.URL,
		Groups: []string{"lamiona", "group"},
	}
	f.cfg.Remote = true
	f.cfg.WorkspaceFile = saveWorkspace(t, spec)
	f.cfg.remoteHooks = &remoteHooks{
		token:      "secret",
		httpClient: server.Client(),
	}
	f.cfg.Attempts = 1

	report, err := runWorkspace(t, f, spec, ModeStatus)
	if err != nil || !report.Ready || report.RemoteInventory == nil || report.RemoteInventory.Catalog == nil {
		t.Fatal(err, report)
	}

	catalog := report.RemoteInventory.Catalog
	if len(catalog.NotCloned) != 2 || catalog.NotCloned[0] != "lamiona/auth/token-cleaner" {
		t.Fatal(catalog)
	}

	if _, statErr := os.Stat(
		filepath.Join(f.base, "lamiona", "search", "new-indexer"),
	); !errors.Is(
		statErr,
		os.ErrNotExist,
	) {
		t.Fatal("status created a clone", statErr)
	}
}

func TestSyncRemoteFailureStaysHonest(t *testing.T) {
	f := setup(t)

	var calls int

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++

		http.Error(w, "secret-body", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)

	spec := oneProject(t, "group/repo with spaces", nil)
	spec.GitLab = &GitLabSource{
		URL:    server.URL,
		Groups: []string{"group"},
	}
	f.cfg.Remote = true
	f.cfg.WorkspaceFile = "workspace.json"
	f.cfg.remoteHooks = &remoteHooks{
		token:      "secret",
		httpClient: server.Client(),
	}
	f.cfg.Attempts = 3
	f.cfg.RetryDelay = time.Millisecond

	report, err := runWorkspace(t, f, spec, ModeSync)
	if !report.Ready || !errors.Is(err, errRemoteAfterSync) || calls != 3 {
		t.Fatalf("ready %v calls %d err %v", report.Ready, calls, err)
	}

	if report.RemoteInventory.Status != remoteStatusFailed || report.RemoteInventory.Catalog != nil {
		t.Fatal(report.RemoteInventory)
	}

	if ExitCodeForWorkspace(err) != 1 || strings.Contains(err.Error(), "secret-body") {
		t.Fatal(err)
	}
}

func TestRemoteSkipsAPIAfterNetworkFailure(t *testing.T) {
	f := setup(t)
	git(t, f.repo, "remote", "set-url", "origin", "ssh://git@127.0.0.1:1/group/repo.git")

	var calls int

	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls++
	}))
	t.Cleanup(server.Close)

	spec := oneProject(t, "group/repo with spaces", nil)
	spec.GitLab = &GitLabSource{
		URL:    server.URL,
		Groups: []string{"group"},
	}
	f.cfg.Remote = true
	f.cfg.remoteHooks = &remoteHooks{
		token:      "secret",
		httpClient: server.Client(),
	}
	f.cfg.Attempts = 1
	f.cfg.ProbeTimeout = 200 * time.Millisecond

	report, err := runWorkspace(t, f, spec, ModeSync)
	if err == nil || calls != 0 || report.RemoteInventory.Reason != remoteReasonNetwork {
		t.Fatalf("calls %d reason %v err %v", calls, report.RemoteInventory, err)
	}
}

func TestRemoteDryRunAndMissingTokenDoNotCallAPI(t *testing.T) {
	f := setup(t)

	var calls int

	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls++
	}))
	t.Cleanup(server.Close)

	spec := oneProject(t, "group/repo with spaces", nil)
	spec.GitLab = &GitLabSource{
		URL:    server.URL,
		Groups: []string{"group"},
	}
	f.cfg.Remote = true
	f.cfg.DryRun = true
	f.cfg.remoteHooks = &remoteHooks{
		token:      "secret",
		httpClient: server.Client(),
	}
	_, err := runWorkspace(t, f, spec, ModeSync)

	if !errors.Is(err, errRemoteDryRun) || calls != 0 || ExitCodeForWorkspace(err) != 2 {
		t.Fatal(err, calls)
	}

	f.cfg.DryRun = false
	f.cfg.remoteHooks.token = ""

	t.Setenv("GITLAB_TOKEN", "")
	_, err = runWorkspace(t, f, spec, ModeStatus)
	if !errors.Is(err, errRemoteToken) || calls != 0 {
		t.Fatal(err, calls)
	}
}

func TestStatusWithoutRemoteDoesNotCallAPI(t *testing.T) {
	f := setup(t)

	var calls int

	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls++
	}))
	t.Cleanup(server.Close)

	spec := oneProject(t, "group/repo with spaces", nil)
	spec.GitLab = &GitLabSource{
		URL:    server.URL,
		Groups: []string{"group"},
	}
	f.cfg.Remote = false
	f.cfg.remoteHooks = &remoteHooks{
		token:      "secret",
		httpClient: server.Client(),
	}
	report, err := runWorkspace(t, f, spec, ModeStatus)

	if err != nil || calls != 0 || !strings.Contains(report.RemoteInventory.Error, "use --remote") {
		t.Fatal(err, calls, report.RemoteInventory)
	}
}

func TestCompareMatrix(t *testing.T) {
	f := setup(t)
	git(t, f.repo, "remote", "set-url", "origin", "git@gitlab.example:group/repo with spaces.git")
	other := filepath.Join(f.base, "lamiona", "elsewhere")
	git(t, f.base, "clone", f.remote, other)
	git(t, other, "remote", "set-url", "origin", "git@gitlab.example:lamiona/search/new-indexer.git")
	occupied := filepath.Join(f.base, "lamiona", "calendar", "reminders")

	if err := os.MkdirAll(occupied, 0o750); err != nil {
		t.Fatal(err)
	}

	spec := oneProject(t, "group/repo with spaces", nil)
	missing := &ProjectSpec{
		Path: "lamiona/missing/listed",
	}
	outside := &ProjectSpec{
		Path: "outside/kept",
	}
	spec.Projects = append(spec.Projects, missing, outside)
	spec.GitLab = &GitLabSource{
		URL:    "https://gitlab.example",
		Groups: []string{"lamiona", "group"},
	}
	emptyProject := &gitlab.Project{
		ID:                5,
		PathWithNamespace: "lamiona/empty",
		SSHURLToRepo:      "git@gitlab.example:lamiona/empty.git",
	}
	projects := []*gitlab.Project{
		remoteProject(1, "group/repo with spaces", "git@gitlab.example:group/repo with spaces.git"),
		remoteProject(2, "lamiona/search/new-indexer", "git@gitlab.example:lamiona/search/new-indexer.git"),
		remoteProject(3, "lamiona/calendar/reminders", "git@gitlab.example:lamiona/calendar/reminders.git"),
		remoteProject(4, "lamiona/search/absent", "git@gitlab.example:lamiona/search/absent.git"),
		emptyProject,
	}

	query := &projectCompare{
		git:        offlineGit(f.cfg),
		base:       f.base,
		spec:       spec,
		projects:   projects,
		allowLocal: false,
	}

	inventory, err := compareProjects(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}

	catalog := inventory.Catalog
	if !slices.Contains(catalog.DifferentPath, "lamiona/search/new-indexer") ||
		!slices.Contains(catalog.Conflicts, "lamiona/calendar/reminders") ||
		!slices.Contains(catalog.NotCloned, "lamiona/search/absent") ||
		!slices.Contains(catalog.NotReturned, "lamiona/missing/listed") ||
		!slices.Contains(catalog.OutsideScope, "outside/kept") ||
		!slices.Contains(catalog.SkippedEmpty, "lamiona/empty") {
		t.Fatalf("%+v", catalog)
	}
}

func catalogServer(t *testing.T, projects []*gitlab.Project) *httptest.Server {
	t.Helper()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Private-Token") == "" || strings.Contains(r.URL.RawQuery, "secret") {
			t.Errorf("token leaked into %s", r.URL.RequestURI())
		}

		if err := json.NewEncoder(w).Encode(projects); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func remoteProject(id int, path, ssh string) *gitlab.Project {
	return &gitlab.Project{
		ID:                id,
		PathWithNamespace: path,
		SSHURLToRepo:      ssh,
		HTTPURLToRepo:     "https://gitlab.example/" + path + ".git",
		DefaultBranch:     "master",
	}
}
