package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/oshokin/release-align/internal/gitter"
)

// hostScan is one clone under a mixed-case DNS directory, plus a second remote.
type hostScan struct {
	// root is the temporary parent of the host directory.
	root string
	// host is the base directory whose name supplies the GitLab URL.
	host string
	// remote is the origin URL that must stay on the project.
	remote string
	// client runs local Git during the scan.
	client *gitter.Client
}

// TestScanWorkspaceFindsNestedGroups verifies deterministic paths and directory-prefix groups.
func TestScanWorkspaceFindsNestedGroups(t *testing.T) {
	f := setup(t)
	nested := filepath.Join(f.base, "lamiona", "search", "service")
	git(t, f.base, "clone", f.remote, nested)
	git(t, f.base, "clone", f.remote, filepath.Join(f.base, "standalone"))
	git(t, f.base, "init", "--bare", filepath.Join(f.base, "bare.git"))
	git(t, f.base, "clone", f.remote, filepath.Join(f.base, ".hidden", "ignored"))
	// A nested repository is deliberately excluded once its parent root is found.
	git(t, f.repo, "clone", f.remote, filepath.Join(f.repo, "vendor", "nested"))
	// A failing transport proves discovery does not contact origin.
	git(t, nested, "remote", "set-url", "origin", "git@unreachable.invalid:group/repo.git")
	options := &WorkspaceInitOptions{
		BaseDir: f.base,
		Branch:  "Release-not-fetched",
		Release: "Lamiona",
	}
	client := &gitter.Client{
		LocalTimeout: f.cfg.LocalTimeout,
	}

	spec, err := ScanWorkspace(t.Context(), client, options)
	if err != nil {
		t.Fatal(err)
	}

	if len(spec.Projects) != 3 {
		t.Fatalf("got %+v", spec.Projects)
	}

	if spec.Projects[0].Path != "group/repo with spaces" || spec.Projects[0].URL != f.remote ||
		spec.Projects[0].Name != "repo with spaces" {
		t.Fatalf("first: %+v", spec.Projects[0])
	}

	if spec.Projects[1].Path != "lamiona/search/service" ||
		spec.Projects[1].URL != "git@unreachable.invalid:group/repo.git" ||
		!reflect.DeepEqual(spec.Projects[1].Groups, []string{"lamiona", "lamiona/search"}) {
		t.Fatalf("nested: %+v", spec.Projects[1])
	}

	if spec.Projects[2].Path != "standalone" || spec.Projects[2].URL != f.remote || len(spec.Projects[2].Groups) != 0 {
		t.Fatalf("standalone: %+v", spec.Projects[2])
	}

	second, err := ScanWorkspace(t.Context(), client, options)
	if err != nil || !cmp.Equal(spec, second, specCompare) {
		t.Fatalf("not deterministic: %v", err)
	}

	selected, err := spec.SelectProjects(nil, []string{"lamiona"})
	if err != nil || len(selected) != 1 || selected[0].Path != "lamiona/search/service" {
		t.Fatal(selected, err)
	}

	if git(t, f.repo, "branch", "--show-current") != "master" {
		t.Fatal("scan switched a branch")
	}
}

// TestScanWorkspaceValidatesGitfilesAndIgnoresDirectorySymlinks checks repository boundaries.
func TestScanWorkspaceValidatesGitfilesAndIgnoresDirectorySymlinks(t *testing.T) {
	f := setup(t)
	linked := filepath.Join(f.base, "linked")
	git(t, f.repo, "worktree", "add", "--detach", linked, "HEAD")
	alias := filepath.Join(f.base, "alias")
	if err := os.Symlink(filepath.Join(f.base, "group"), alias); err != nil {
		t.Skip(err)
	}

	options := &WorkspaceInitOptions{
		BaseDir: f.base,
		Branch:  "master",
	}
	client := &gitter.Client{
		LocalTimeout: f.cfg.LocalTimeout,
	}
	spec, err := ScanWorkspace(t.Context(), client, options)

	if err != nil || len(spec.Projects) != 2 {
		t.Fatal(spec, err)
	}

	if spec.Projects[1].Path != "linked" {
		t.Fatal(spec.Projects[1])
	}
}

// TestScanWorkspaceRefusesIncompleteInventory checks that a corrupt clone is never silently omitted.
func TestScanWorkspaceRefusesIncompleteInventory(t *testing.T) {
	f := setup(t)
	broken := filepath.Join(f.base, "broken")

	if err := os.MkdirAll(broken, 0o700); err != nil {
		t.Fatal(err)
	}

	write(t, filepath.Join(broken, ".git"), "not a gitfile")
	options := &WorkspaceInitOptions{
		BaseDir: f.base,
		Branch:  "master",
	}
	client := &gitter.Client{
		LocalTimeout: f.cfg.LocalTimeout,
	}
	spec, err := ScanWorkspace(t.Context(), client, options)

	if err == nil || spec != nil {
		t.Fatal("corrupt .git was ignored")
	}
}

// TestScanWorkspaceRejectsMissingOrigin checks that every emitted entry works with workspace validation.
func TestScanWorkspaceRejectsMissingOrigin(t *testing.T) {
	f := setup(t)
	git(t, f.repo, "remote", "remove", "origin")
	options := &WorkspaceInitOptions{
		BaseDir: f.base,
		Branch:  "master",
	}
	client := &gitter.Client{
		LocalTimeout: f.cfg.LocalTimeout,
	}

	spec, err := ScanWorkspace(t.Context(), client, options)
	if err != nil || len(spec.Projects) != 1 || spec.Projects[0].URL != "" ||
		len(spec.URLGaps) != 1 {
		t.Fatal(spec, err)
	}
}

// TestScanWorkspaceRejectsRootAndCancellation checks invalid roots and interrupted scans.
func TestScanWorkspaceRejectsRootAndCancellation(t *testing.T) {
	f := setup(t)
	client := &gitter.Client{
		LocalTimeout: f.cfg.LocalTimeout,
	}
	options := &WorkspaceInitOptions{
		BaseDir: f.repo,
		Branch:  "master",
	}

	if _, err := ScanWorkspace(t.Context(), client, options); !errors.Is(err, errWorkspaceInitBaseRepo) {
		t.Fatal(err)
	}

	options.BaseDir = t.TempDir()
	if _, err := ScanWorkspace(t.Context(), client, options); !errors.Is(err, errWorkspaceInitEmpty) {
		t.Fatal(err)
	}

	options.BaseDir = f.base
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := ScanWorkspace(ctx, client, options); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}

	options.Branch = "bad..branch"
	if _, err := ScanWorkspace(t.Context(), client, options); err == nil {
		t.Fatal("invalid ref accepted")
	}
}

// TestAbsentWorkspaceFileAllowsAMissingPath accepts a path that can still be created.
func TestAbsentWorkspaceFileAllowsAMissingPath(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "workspace.yml")

	if err := AbsentWorkspaceFile(dest); err != nil {
		t.Fatal(err)
	}

	spec := oneProject(t, "group/service", nil)
	if err := CreateWorkspaceFile(dest, spec); err != nil {
		t.Fatal(err)
	}
}

// TestAbsentWorkspaceFileRejectsAnExistingPath leaves a file and a symlink untouched.
func TestAbsentWorkspaceFileRejectsAnExistingPath(t *testing.T) {
	dir := t.TempDir()

	dest := filepath.Join(dir, "workspace.yml")
	if err := os.WriteFile(dest, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := AbsentWorkspaceFile(dest); !errors.Is(err, os.ErrExist) {
		t.Fatal(err)
	}

	body, err := os.ReadFile(dest)
	if err != nil || string(body) != "keep\n" {
		t.Fatal(string(body), err)
	}

	link := filepath.Join(dir, "link.yml")
	if err = os.Symlink(dest, link); err != nil {
		t.Skip(err)
	}

	if err = AbsentWorkspaceFile(link); !errors.Is(err, os.ErrExist) {
		t.Fatal(err)
	}

	appeared := filepath.Join(dir, "later.yml")
	if err = AbsentWorkspaceFile(appeared); err != nil {
		t.Fatal(err)
	}

	if err = os.WriteFile(appeared, []byte("race\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err = AbsentWorkspaceFile(appeared); !errors.Is(err, os.ErrExist) {
		t.Fatal(err)
	}
}

// TestCreateWorkspaceFileNeverOverwrites verifies byte-for-byte preservation and schema round trips.
func TestCreateWorkspaceFileNeverOverwrites(t *testing.T) {
	spec := oneProject(t, "group/service", nil)
	dest := filepath.Join(t.TempDir(), "workspace.yml")

	if err := CreateWorkspaceFile(dest, spec); err != nil {
		t.Fatal(err)
	}

	before, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadWorkspace(dest)
	if err != nil || !cmp.Equal(loaded, spec, specCompare) {
		t.Fatal(loaded, err)
	}

	if err = CreateWorkspaceFile(dest, spec); !errors.Is(err, os.ErrExist) {
		t.Fatal(err)
	}

	after, err := os.ReadFile(dest)
	if err != nil || string(before) != string(after) {
		t.Fatal("existing file changed")
	}

	link := filepath.Join(t.TempDir(), "link.yml")
	if err = os.Symlink(dest, link); err != nil {
		t.Skip(err)
	}

	if err = CreateWorkspaceFile(link, spec); !errors.Is(err, os.ErrExist) {
		t.Fatal(err)
	}
}

// TestDirectoryGitLabHost accepts a DNS directory name and rejects everything else.
func TestDirectoryGitLabHost(t *testing.T) {
	longLabel := strings.Repeat("a", dnsLabelMaxLen)
	cases := []struct {
		path string
		host string
		ok   bool
	}{
		{path: filepath.Join("src", "git.example.com"), host: "git.example.com", ok: true},
		{path: filepath.Join("src", "Git.Example.COM"), host: "git.example.com", ok: true},
		{path: filepath.Join("src", "git-lab.example.com"), host: "git-lab.example.com", ok: true},
		{path: filepath.Join("src", "a.b"), host: "a.b", ok: true},
		{path: filepath.Join("src", "git1.example.com"), host: "git1.example.com", ok: true},
		{path: filepath.Join("src", "10.1.2.3"), host: "10.1.2.3", ok: true},
		{path: filepath.Join("src", longLabel+".example"), host: longLabel + ".example", ok: true},
		{path: filepath.Join("src", "src")},
		{path: filepath.Join("src", "work")},
		{path: filepath.Join("src", "lamiona")},
		{path: filepath.Join("src", "localhost")},
		{path: filepath.Join("git.example.com", "src")},
		{path: filepath.Join("src", "my_git.example")},
		{path: filepath.Join("src", "-git.example")},
		{path: filepath.Join("src", "git-.example")},
		{path: filepath.Join("src", "git..example")},
		{path: filepath.Join("src", ".git.example")},
		{path: filepath.Join("src", "git.example.")},
		{path: filepath.Join("src", "git.example.com:8443")},
		{path: filepath.Join("src", "git example.com")},
		{path: filepath.Join("src", "гит.example.com")},
		{path: filepath.Join("src", strings.Repeat("a", dnsLabelMaxLen+1)+".example")},
	}

	for _, tc := range cases {
		host, ok := directoryGitLabHost(tc.path)
		if ok != tc.ok || host != tc.host {
			t.Fatalf("%s: got %q %v", tc.path, host, ok)
		}
	}
}

// TestScanWorkspaceTakesGitLabHostFromBaseDirectory stores the directory host and leaves origins alone.
func TestScanWorkspaceTakesGitLabHostFromBaseDirectory(t *testing.T) {
	scan := newHostScan(t)
	options := &WorkspaceInitOptions{
		BaseDir: scan.host,
		Branch:  "master",
	}
	spec := scanWorkspace(t, scan.client, options)

	if spec.GitLab == nil || spec.GitLab.URL != "https://git.example.com" || !spec.GitLabURLFromBase ||
		len(spec.GitLab.Groups) != 0 {
		t.Fatalf("%+v", spec.GitLab)
	}

	if spec.Projects[0].URL != scan.remote ||
		!reflect.DeepEqual(spec.Projects[0].Groups, []string{"lamiona", "lamiona/search"}) {
		t.Fatalf("%+v", spec.Projects[0])
	}

	dest := filepath.Join(scan.root, "workspace.yml")
	if err := CreateWorkspaceFile(dest, spec); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}

	text := string(body)
	if !strings.Contains(text, "url: https://git.example.com") || !strings.Contains(text, "groups:") {
		t.Fatal(text)
	}

	loaded, err := LoadWorkspace(dest)
	if err != nil || loaded.GitLab == nil || loaded.GitLab.URL != "https://git.example.com" ||
		len(loaded.GitLab.Groups) != 0 || loaded.GitLabURLFromBase {
		t.Fatal(loaded, err)
	}

	cfg := &Config{
		Remote: true,
	}
	if err = PrepareRemote(cfg, spec); !errors.Is(err, errGitLabGroups) {
		t.Fatal(err)
	}
}

// TestScanWorkspaceKeepsExplicitGitLabURL does not replace a flag value with the directory name.
func TestScanWorkspaceKeepsExplicitGitLabURL(t *testing.T) {
	scan := newHostScan(t)
	options := &WorkspaceInitOptions{
		BaseDir:   scan.host,
		Branch:    "master",
		GitLabURL: "https://Saved.Example",
	}
	explicit := scanWorkspace(t, scan.client, options)

	if explicit.GitLab == nil || explicit.GitLab.URL != "https://Saved.Example" || explicit.GitLabURLFromBase {
		t.Fatalf("%+v", explicit.GitLab)
	}

	options.GitLabURL = "http://git.example.com"

	_, err := ScanWorkspace(t.Context(), scan.client, options)
	if !errors.Is(err, errGitLabURL) {
		t.Fatal(err)
	}
}

// TestScanWorkspaceKeepsPassedGitLabGroups stores the directory host and the groups that were passed.
func TestScanWorkspaceKeepsPassedGitLabGroups(t *testing.T) {
	scan := newHostScan(t)
	options := &WorkspaceInitOptions{
		BaseDir:      scan.host,
		Branch:       "master",
		GitLabGroups: []string{"lamiona"},
	}
	spec := scanWorkspace(t, scan.client, options)

	if spec.GitLab == nil || spec.GitLab.URL != "https://git.example.com" || !spec.GitLabURLFromBase ||
		!reflect.DeepEqual(spec.GitLab.Groups, []string{"lamiona"}) {
		t.Fatalf("%+v", spec.GitLab)
	}

	checkout := filepath.Join(scan.host, "lamiona", "search", "calyra")
	git(t, checkout, "remote", "set-url", "origin", "git@git.example.com:other/repo.git")
	kept := scanWorkspace(t, scan.client, options)
	if kept.GitLab == nil || !reflect.DeepEqual(kept.GitLab.Groups, []string{"lamiona"}) {
		t.Fatalf("%+v", kept.GitLab)
	}
}

// TestScanWorkspaceGuessesGitLabGroupsFromOrigins stores namespaces from origins on the saved host.
func TestScanWorkspaceGuessesGitLabGroupsFromOrigins(t *testing.T) {
	scan := newHostScan(t)
	checkout := filepath.Join(scan.host, "lamiona", "search", "calyra")
	git(t, checkout, "remote", "set-url", "origin", "git@git.example.com:Lamiona/Search/Calyra.git")

	localName := filepath.Join(scan.host, "local-name", "repo")
	git(t, scan.root, "clone", scan.remote, localName)
	git(t, localName, "remote", "set-url", "origin", "ssh://git@git.example.com/mirrors/github.com/grpc/grpc.git")

	foreign := filepath.Join(scan.host, "foreign", "repo")
	git(t, scan.root, "clone", scan.remote, foreign)
	git(t, foreign, "remote", "set-url", "origin", "git@other.example:skip/me.git")

	options := &WorkspaceInitOptions{
		BaseDir: scan.host,
		Branch:  "master",
	}
	spec := scanWorkspace(t, scan.client, options)

	if spec.GitLab == nil || !reflect.DeepEqual(spec.GitLab.Groups, []string{"Lamiona", "mirrors"}) {
		t.Fatalf("%+v", spec.GitLab)
	}
}

// TestScanWorkspaceIgnoresParentDirectoryHost does not walk above the given base directory.
func TestScanWorkspaceIgnoresParentDirectoryHost(t *testing.T) {
	scan := newHostScan(t)
	parent := filepath.Join(scan.host, "src")
	git(t, scan.root, "clone", scan.remote, filepath.Join(parent, "calyra"))
	options := &WorkspaceInitOptions{
		BaseDir: parent,
		Branch:  "master",
	}
	spec := scanWorkspace(t, scan.client, options)

	if spec.GitLab != nil || spec.GitLabURLFromBase {
		t.Fatalf("%+v", spec.GitLab)
	}
}

// newHostScan clones one repository under a mixed-case DNS directory.
func newHostScan(t *testing.T) *hostScan {
	t.Helper()

	f := setup(t)
	root := filepath.Dir(f.base)
	host := filepath.Join(root, "Git.Example.COM")
	checkout := filepath.Join(host, "lamiona", "search", "calyra")
	git(t, root, "clone", f.remote, checkout)
	git(t, checkout, "remote", "add", "upstream", "https://other.example/lamiona/search/calyra.git")
	client := &gitter.Client{
		LocalTimeout: f.cfg.LocalTimeout,
	}
	scan := &hostScan{
		root:   root,
		host:   host,
		remote: f.remote,
		client: client,
	}

	return scan
}

// scanWorkspace runs init discovery and fails the test on error.
func scanWorkspace(t *testing.T, client *gitter.Client, options *WorkspaceInitOptions) *WorkspaceSpec {
	t.Helper()

	spec, err := ScanWorkspace(t.Context(), client, options)
	if err != nil {
		t.Fatal(err)
	}

	return spec
}
