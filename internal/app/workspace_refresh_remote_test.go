package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oshokin/release-align/internal/gitlab"
)

// TestRefreshRemotePreviewListsCatalogGaps leaves the file and the directories in place.
func TestRefreshRemotePreviewListsCatalogGaps(t *testing.T) {
	f := setup(t)
	gone := filepath.Join(f.base, "group", "gone")
	outside := filepath.Join(f.base, "other", "kept")

	if err := os.MkdirAll(gone, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}

	write(t, filepath.Join(gone, "stay"), "x")
	file := remoteFile(t, f, remoteProjects("group/repo with spaces", "group/gone", "other/kept"))
	before := readBytes(t, file)
	stamp := setPastMtime(t, file)
	options := remoteOptions(f, false, false, "group/repo with spaces")

	result, err := RefreshWorkspace(t.Context(), offlineClient(f), file, options)
	if err != nil || result.Written || !samePaths(result.RemoteAbsent, []string{"group/gone"}) {
		t.Fatal(result, err)
	}

	if !sameBytes(readBytes(t, file), before) || !mtimeEqual(t, file, stamp) {
		t.Fatal("preview wrote the file")
	}

	if _, err = os.Stat(filepath.Join(gone, "stay")); err != nil {
		t.Fatal(err)
	}
}

// TestRefreshRemoteSyncDropsAbsentPathsAndKeepsDirectories removes only the catalog gap from the file.
func TestRefreshRemoteSyncDropsAbsentPathsAndKeepsDirectories(t *testing.T) {
	f := setup(t)
	extra := cloneRel(t, f, "group/unlisted")
	gone := filepath.Join(f.base, "group", "gone")

	if err := os.MkdirAll(gone, 0o700); err != nil {
		t.Fatal(err)
	}

	write(t, filepath.Join(gone, "stay"), "x")
	file := remotePinnedFile(t)
	options := remoteOptions(f, true, false, "group/repo with spaces")

	result, err := RefreshWorkspace(t.Context(), offlineClient(f), file, options)
	if err != nil || !result.Written || !samePaths(result.Removed, []string{"group/gone"}) || len(result.Deleted) != 0 {
		t.Fatal(result, err)
	}

	text := string(readBytes(t, file))
	if !strings.Contains(text, "# keep me") || strings.Contains(text, "group/gone") ||
		strings.Contains(text, extra) || !strings.Contains(text, "refs/tags/v1") ||
		!strings.Contains(text, "other/kept") {
		t.Fatal(text)
	}

	if _, err = os.Stat(filepath.Join(gone, "stay")); err != nil {
		t.Fatal(err)
	}

	if _, err = os.Stat(filepath.Join(f.base, filepath.FromSlash(extra))); err != nil {
		t.Fatal(err)
	}

	stamp := setPastMtime(t, file)
	written := readBytes(t, file)
	again, err := RefreshWorkspace(t.Context(), offlineClient(f), file, options)

	if err != nil || again.Written || !sameBytes(readBytes(t, file), written) || !mtimeEqual(t, file, stamp) {
		t.Fatal(again, err)
	}
}

// TestRefreshRemoteDeleteRemovesOnlyTheDroppedCheckout leaves listed and unlisted directories.
func TestRefreshRemoteDeleteRemovesOnlyTheDroppedCheckout(t *testing.T) {
	f := setup(t)
	extra := cloneRel(t, f, "group/unlisted")
	gone := filepath.Join(f.base, "group", "gone")

	if err := os.MkdirAll(gone, 0o700); err != nil {
		t.Fatal(err)
	}

	file := remoteFile(t, f, remoteProjects("group/repo with spaces", "group/gone"))
	options := remoteOptions(f, true, true, "group/repo with spaces")

	result, err := RefreshWorkspace(t.Context(), offlineClient(f), file, options)
	if err != nil || !result.Written || !samePaths(result.Deleted, []string{"group/gone"}) {
		t.Fatal(result, err)
	}

	if _, err = os.Stat(gone); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}

	if _, err = os.Stat(f.repo); err != nil {
		t.Fatal(err)
	}

	if _, err = os.Stat(filepath.Join(f.base, filepath.FromSlash(extra))); err != nil {
		t.Fatal(err)
	}
}

// TestRefreshRemoteRefusesAnEmptyCatalog leaves a non-empty file in place.
func TestRefreshRemoteRefusesAnEmptyCatalog(t *testing.T) {
	f := setup(t)
	file := remoteFile(t, f, remoteProjects("group/repo with spaces"))
	before := readBytes(t, file)
	options := &WorkspaceRefreshOptions{
		BaseDir: f.base,
		Remote:  true,
		Sync:    true,
		catalog: new(refreshCatalog),
	}

	_, err := RefreshWorkspace(t.Context(), offlineClient(f), file, options)
	if !errors.Is(err, ErrWorkspaceRefreshUsage) || !errors.Is(err, errWorkspaceRefreshEmptyRemote) ||
		!sameBytes(readBytes(t, file), before) {
		t.Fatal(err)
	}
}

// TestRefreshRemoteRejectsDeleteAddAndPaths checks the combinations that would remove the wrong set.
func TestRefreshRemoteRejectsDeleteAddAndPaths(t *testing.T) {
	f := setup(t)
	file := remoteFile(t, f, remoteProjects("group/repo with spaces"))
	before := readBytes(t, file)
	cases := []*WorkspaceRefreshOptions{
		{BaseDir: f.base, Delete: true},
		{BaseDir: f.base, Remote: true, Add: []string{"group/repo with spaces"}},
		{BaseDir: f.base, Remote: true, Sync: true, SyncPaths: []string{"group/gone"}},
	}

	for _, options := range cases {
		options.catalog = new(refreshCatalog)
		_, err := RefreshWorkspace(t.Context(), offlineClient(f), file, options)

		if !errors.Is(err, ErrWorkspaceRefreshUsage) || !sameBytes(readBytes(t, file), before) {
			t.Fatal(options, err)
		}
	}

	plain := stashFile(t, "group/repo with spaces")
	plainBefore := readBytes(t, plain)
	options := &WorkspaceRefreshOptions{
		BaseDir: f.base,
		Remote:  true,
		catalog: new(refreshCatalog),
	}
	_, err := RefreshWorkspace(t.Context(), offlineClient(f), plain, options)

	if !errors.Is(err, errGitLabSource) || !sameBytes(readBytes(t, plain), plainBefore) {
		t.Fatal(err)
	}
}

// TestRefreshRemoteDeleteDoesNotFollowASymlink drops the path and leaves the target.
func TestRefreshRemoteDeleteDoesNotFollowASymlink(t *testing.T) {
	f := setup(t)
	outside := t.TempDir()
	write(t, filepath.Join(outside, "keep"), "x")
	link := filepath.Join(f.base, "group", "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	file := remoteFile(t, f, remoteProjects("group/link", "group/repo with spaces"))
	options := remoteOptions(f, true, true, "group/repo with spaces")

	result, err := RefreshWorkspace(t.Context(), offlineClient(f), file, options)
	if err == nil || !errors.Is(err, errWorkspacePathKind) {
		t.Fatal(result, err)
	}

	if _, statErr := os.Stat(filepath.Join(outside, "keep")); statErr != nil {
		t.Fatal(statErr)
	}

	text := string(readBytes(t, file))
	if strings.Contains(text, "group/link") || !strings.Contains(text, "group/repo with spaces") {
		t.Fatal(text)
	}
}

// TestRefreshRemoteReadsTheNonArchivedCatalog drops a project the listing did not return.
func TestRefreshRemoteReadsTheNonArchivedCatalog(t *testing.T) {
	f := setup(t)

	var archived string

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		archived = r.URL.Query().Get("archived")
		if r.Header.Get("Private-Token") == "" {
			t.Errorf("missing token")
		}

		projects := []*gitlab.Project{remoteProject(1, "group/repo with spaces", "")}
		if err := json.NewEncoder(w).Encode(projects); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)

	file := remoteServerFile(t, f, server.URL)
	options := &WorkspaceRefreshOptions{
		BaseDir: f.base,
		Remote:  true,
		Sync:    true,
		remote: &remoteHooks{
			token:      "secret",
			httpClient: server.Client(),
		},
	}

	result, err := RefreshWorkspace(t.Context(), offlineClient(f), file, options)
	if err != nil || archived != "" || !result.Written || !samePaths(result.Removed, []string{"group/gone"}) {
		t.Fatal(archived, result, err)
	}

	if _, err = os.Stat(filepath.Join(f.base, "group", "gone")); err != nil {
		t.Fatal(err)
	}
}

// TestRefreshRemoteKeepsTheFileWhenTheCatalogFails covers a missing token and a failed listing.
func TestRefreshRemoteKeepsTheFileWhenTheCatalogFails(t *testing.T) {
	f := setup(t)
	t.Setenv("GITLAB_TOKEN", "")

	var calls int

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++

		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	file := remoteServerFile(t, f, server.URL)
	before := readBytes(t, file)
	missing := &WorkspaceRefreshOptions{
		BaseDir: f.base,
		Remote:  true,
		Sync:    true,
	}

	_, err := RefreshWorkspace(t.Context(), offlineClient(f), file, missing)
	if !errors.Is(err, ErrWorkspaceRefreshUsage) || !errors.Is(err, errRemoteToken) || calls != 0 ||
		!sameBytes(readBytes(t, file), before) {
		t.Fatal(err, calls)
	}

	failed := &WorkspaceRefreshOptions{
		BaseDir: f.base,
		Remote:  true,
		Sync:    true,
		remote: &remoteHooks{
			token:      "secret",
			httpClient: server.Client(),
		},
	}

	_, err = RefreshWorkspace(t.Context(), offlineClient(f), file, failed)
	if err == nil || !errors.Is(err, errRemoteInventory) || !sameBytes(readBytes(t, file), before) {
		t.Fatal(err)
	}
}

func remoteOptions(f *fixture, sync, remove bool, present string) *WorkspaceRefreshOptions {
	return &WorkspaceRefreshOptions{
		BaseDir: f.base,
		Remote:  true,
		Sync:    sync,
		Delete:  remove,
		catalog: &refreshCatalog{
			projects: []*gitlab.Project{remoteProject(1, present, "")},
		},
	}
}

func remoteProjects(paths ...string) []*ProjectSpec {
	projects := make([]*ProjectSpec, 0, len(paths))
	for i, path := range paths {
		project := &ProjectSpec{
			Name: "p" + string(rune('a'+i)),
			Path: path,
		}
		projects = append(projects, project)
	}

	return projects
}

func remoteFile(t *testing.T, f *fixture, projects []*ProjectSpec) string {
	t.Helper()

	spec := &WorkspaceSpec{
		SchemaVersion: 1,
		BaseDir:       f.base,
		DefaultRevision: &RevisionSpec{
			Branch: "master",
		},
		GitLab: &GitLabSource{
			URL:           "https://gitlab.example",
			Groups:        []string{"group"},
			CloneProtocol: cloneProtocolSSH,
		},
		Projects: projects,
	}

	return saveWorkspace(t, spec)
}

func remoteServerFile(t *testing.T, f *fixture, rawURL string) string {
	t.Helper()

	gone := filepath.Join(f.base, "group", "gone")
	if err := os.MkdirAll(gone, 0o700); err != nil {
		t.Fatal(err)
	}

	spec := &WorkspaceSpec{
		SchemaVersion: 1,
		BaseDir:       f.base,
		DefaultRevision: &RevisionSpec{
			Branch: "master",
		},
		GitLab: &GitLabSource{
			URL:           rawURL,
			Groups:        []string{"group"},
			CloneProtocol: cloneProtocolSSH,
		},
		Projects: remoteProjects("group/repo with spaces", "group/gone"),
	}

	return saveWorkspace(t, spec)
}

func remotePinnedFile(t *testing.T) string {
	t.Helper()

	body := "manifest:\n  defaults:\n    revision: refs/heads/master\n  projects:\n" +
		"    # keep me\n    - name: existing\n      path: group/repo with spaces\n" +
		"      revision: refs/tags/v1\n" +
		"    # gone\n    - name: extra\n      path: group/gone\n" +
		"    - name: outside\n      path: other/kept\n" +
		"release-align:\n  schema-version: 1\n  gitlab:\n    url: https://gitlab.example\n" +
		"    groups:\n      - group\n    clone-protocol: ssh\n"
	file := filepath.Join(t.TempDir(), "workspace.yml")
	write(t, file, body)

	return file
}

func samePaths(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}

	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}

	return true
}

func sameBytes(got, want []byte) bool {
	if len(got) != len(want) {
		return false
	}

	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}

	return true
}
