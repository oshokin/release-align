package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/oshokin/release-align/internal/gitter"
)

// TestScanWorkspaceFindsNestedGroups verifies deterministic paths and directory-prefix groups.
func TestScanWorkspaceFindsNestedGroups(t *testing.T) {
	f := setup(t)
	nested := filepath.Join(f.base, "mailion", "search", "service")
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
		Release: "Mailion",
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

	if spec.Projects[1].Path != "mailion/search/service" ||
		spec.Projects[1].URL != "git@unreachable.invalid:group/repo.git" ||
		!reflect.DeepEqual(spec.Projects[1].Groups, []string{"mailion", "mailion/search"}) {
		t.Fatalf("nested: %+v", spec.Projects[1])
	}

	if spec.Projects[2].Path != "standalone" || spec.Projects[2].URL != f.remote || len(spec.Projects[2].Groups) != 0 {
		t.Fatalf("standalone: %+v", spec.Projects[2])
	}
	second, err := ScanWorkspace(t.Context(), client, options)
	if err != nil || !reflect.DeepEqual(spec, second) {
		t.Fatalf("not deterministic: %v", err)
	}
	selected, err := spec.SelectProjects(nil, []string{"mailion"})
	if err != nil || len(selected) != 1 || selected[0].Path != "mailion/search/service" {
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
	if err != nil || !reflect.DeepEqual(loaded, spec) {
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
