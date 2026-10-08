package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/oshokin/release-align/internal/gitter"
)

// TestRefreshPreviewSeesNewClone checks that a preview reports a new clone and leaves the file and Git alone.
func TestRefreshPreviewSeesNewClone(t *testing.T) {
	f := setup(t)
	file := workspaceFromScan(t, f)
	before := readBytes(t, file)
	stamp := setPastMtime(t, file)
	snapshot := localGitSnapshot(t, f.repo)
	rel := cloneRel(t, f, "lamiona/search/new-indexer")
	added := localGitSnapshot(t, filepath.Join(f.base, filepath.FromSlash(rel)))
	options := &WorkspaceRefreshOptions{
		BaseDir: f.base,
	}

	result, err := RefreshWorkspace(t.Context(), offlineClient(f), file, options)
	if err != nil {
		t.Fatal(err)
	}

	wantGroups := []string{"lamiona", "lamiona/search"}
	if result.Written || len(result.Unlisted) != 1 || result.Unlisted[0].Path != rel ||
		!reflect.DeepEqual(result.Unlisted[0].Groups, wantGroups) || len(result.Missing) != 0 {
		t.Fatalf("%+v", result)
	}

	if !bytes.Equal(readBytes(t, file), before) || !mtimeEqual(t, file, stamp) {
		t.Fatal("preview changed the workspace file")
	}

	if _, statErr := os.Lstat(file + ".lock"); !os.IsNotExist(statErr) {
		t.Fatal("preview created a lock", statErr)
	}

	if localGitSnapshot(t, f.repo) != snapshot ||
		localGitSnapshot(t, filepath.Join(f.base, filepath.FromSlash(rel))) != added {
		t.Fatal("preview changed Git state")
	}
}

// TestRefreshAddOneOfTwo appends only the requested new clone.
func TestRefreshAddOneOfTwo(t *testing.T) {
	f := setup(t)
	file := workspaceFromScan(t, f)
	first := cloneRel(t, f, "lamiona/search/new-indexer")
	second := cloneRel(t, f, "experiments/sandbox")
	options := &WorkspaceRefreshOptions{
		BaseDir: f.base,
		Add:     []string{first, first},
	}

	result, err := RefreshWorkspace(t.Context(), offlineClient(f), file, options)
	if err != nil {
		t.Fatal(err)
	}

	if !result.Written || !reflect.DeepEqual(result.Added, []string{first}) || len(result.Unlisted) != 1 ||
		result.Unlisted[0].Path != second {
		t.Fatalf("%+v", result)
	}

	loaded, err := LoadWorkspace(file)
	if err != nil || loaded.Projects[len(loaded.Projects)-1].Path != first ||
		loaded.Projects[len(loaded.Projects)-1].Revision != nil {
		t.Fatal(loaded, err)
	}
}

// TestRefreshAddAllIsIdempotent checks the first write and a byte-for-byte second run.
func TestRefreshAddAllIsIdempotent(t *testing.T) {
	f := setup(t)
	file := workspaceFromScan(t, f)
	rel := cloneRel(t, f, "lamiona/search/new-indexer")
	options := &WorkspaceRefreshOptions{
		BaseDir: f.base,
		AddAll:  true,
	}

	result, err := RefreshWorkspace(t.Context(), offlineClient(f), file, options)
	if err != nil || !result.Written || !reflect.DeepEqual(result.Added, []string{rel}) || len(result.Unlisted) != 0 {
		t.Fatal(result, err)
	}

	written := readBytes(t, file)
	stamp := setPastMtime(t, file)

	again, err := RefreshWorkspace(t.Context(), offlineClient(f), file, options)
	if err != nil || again.Written || len(again.Added) != 0 || len(again.Unlisted) != 0 {
		t.Fatal(again, err)
	}

	if !bytes.Equal(readBytes(t, file), written) || !mtimeEqual(t, file, stamp) {
		t.Fatal("repeat --add-all rewrote the file")
	}
}

// TestRefreshPreservesManualIntent keeps pins, manual groups, and the existing project order.
func TestRefreshPreservesManualIntent(t *testing.T) {
	f := setup(t)
	cloneRel(t, f, "z/old")
	cloneRel(t, f, "a/keep")
	revision := &RevisionSpec{
		Tag: "v26.3.0",
	}
	older := &ProjectSpec{
		Path:     "z/old",
		Groups:   []string{"manual-team"},
		Revision: revision,
	}
	keeper := &ProjectSpec{
		Path:   "a/keep",
		Groups: []string{"manual-team"},
	}
	spec := &WorkspaceSpec{
		SchemaVersion: 1,
		Release:       "Lamiona 26.3",
		DefaultRevision: &RevisionSpec{
			Branch: "master",
		},
		Projects: []*ProjectSpec{older, keeper},
	}
	file := saveWorkspace(t, spec)
	rel := cloneRel(t, f, "m/new")
	options := &WorkspaceRefreshOptions{
		BaseDir: f.base,
		AddAll:  true,
	}

	result, err := RefreshWorkspace(t.Context(), offlineClient(f), file, options)
	if err != nil || !result.Written {
		t.Fatal(result, err)
	}

	loaded, err := LoadWorkspace(file)
	if err != nil {
		t.Fatal(err)
	}

	if loaded.Release != spec.Release || loaded.DefaultRevision.Branch != spec.DefaultRevision.Branch ||
		len(loaded.Projects) != 4 ||
		loaded.Projects[0].Path != "z/old" || loaded.Projects[1].Path != "a/keep" ||
		loaded.Projects[2].Path != "group/repo with spaces" || loaded.Projects[3].Path != rel ||
		loaded.Projects[0].Revision.Tag != "v26.3.0" ||
		!reflect.DeepEqual(loaded.Projects[0].Groups, []string{"manual-team"}) ||
		loaded.Projects[3].Revision != nil {
		t.Fatalf("%+v", loaded.Projects)
	}
}

// TestRefreshKeepsMissingPath reports a removed clone and does not rename it to a new directory.
func TestRefreshKeepsMissingPath(t *testing.T) {
	f := setup(t)
	file := workspaceFromScan(t, f)
	before := readBytes(t, file)
	rel := cloneRel(t, f, "lamiona/search/moved")

	if err := os.RemoveAll(f.repo); err != nil {
		t.Fatal(err)
	}

	options := &WorkspaceRefreshOptions{
		BaseDir: f.base,
	}

	result, err := RefreshWorkspace(t.Context(), offlineClient(f), file, options)
	if err != nil {
		t.Fatal(err)
	}

	if result.Written || !reflect.DeepEqual(result.Missing, []string{"group/repo with spaces"}) ||
		len(result.Unlisted) != 1 || result.Unlisted[0].Path != rel || !bytes.Equal(readBytes(t, file), before) {
		t.Fatalf("%+v", result)
	}

	options.Add = []string{rel}

	added, err := RefreshWorkspace(t.Context(), offlineClient(f), file, options)
	if err != nil || !added.Written || !reflect.DeepEqual(added.Missing, []string{"group/repo with spaces"}) {
		t.Fatal(added, err)
	}

	loaded, err := LoadWorkspace(file)
	if err != nil || loaded.Projects[0].Path != "group/repo with spaces" || loaded.Projects[1].Path != rel {
		t.Fatal(loaded, err)
	}
}

// TestStatusIgnoresUnlistedClone checks that status still covers only the workspace file.
func TestStatusIgnoresUnlistedClone(t *testing.T) {
	f := setup(t)
	file := workspaceFromScan(t, f)
	rel := cloneRel(t, f, "lamiona/search/new-indexer")

	spec, err := LoadWorkspace(file)
	if err != nil {
		t.Fatal(err)
	}

	report, err := runWorkspace(t, f, spec, ModeStatus)
	if err != nil || !report.Ready || len(report.Rows) != 1 || report.Rows[0].Path == rel {
		t.Fatalf("%+v %v", report, err)
	}
}

// TestRefreshOmittedDefaultInheritsMaster lets a new project inherit west's master.
func TestRefreshOmittedDefaultInheritsMaster(t *testing.T) {
	f := setup(t)
	revision := &RevisionSpec{
		Commit: git(t, f.repo, "rev-parse", "HEAD"),
	}
	spec := oneProject(t, "group/repo with spaces", revision)
	spec.DefaultRevision = nil
	file := saveWorkspace(t, spec)
	rel := cloneRel(t, f, "lamiona/search/new-indexer")
	preview := &WorkspaceRefreshOptions{
		BaseDir: f.base,
	}

	result, err := RefreshWorkspace(t.Context(), offlineClient(f), file, preview)
	if err != nil || result.Written || len(result.Unlisted) != 1 {
		t.Fatal(result, err)
	}

	preview.Add = []string{rel}

	added, err := RefreshWorkspace(t.Context(), offlineClient(f), file, preview)
	if err != nil || !added.Written {
		t.Fatal(added, err)
	}

	loaded, err := LoadWorkspace(file)
	if err != nil || loaded.RevisionFor(loaded.Projects[len(loaded.Projects)-1]).Branch != westDefaultBranch {
		t.Fatal(loaded, err)
	}
}

// TestRefreshBaseDirBoundaries reports every listed path from an empty directory and rejects a missing one.
func TestRefreshBaseDirBoundaries(t *testing.T) {
	f := setup(t)
	file := workspaceFromScan(t, f)
	before := readBytes(t, file)
	empty := &WorkspaceRefreshOptions{
		BaseDir: t.TempDir(),
	}

	result, err := RefreshWorkspace(t.Context(), offlineClient(f), file, empty)
	if err != nil || result.Written || len(result.Unlisted) != 0 ||
		!reflect.DeepEqual(result.Missing, []string{"group/repo with spaces"}) ||
		!bytes.Equal(readBytes(t, file), before) {
		t.Fatal(result, err)
	}

	empty.BaseDir = filepath.Join(t.TempDir(), "missing")
	if _, err = RefreshWorkspace(t.Context(), offlineClient(f), file, empty); err == nil ||
		!bytes.Equal(readBytes(t, file), before) {
		t.Fatal(err)
	}
}

// TestRefreshKeepsHiddenAndNestedRoots does not call a manually listed root missing only because discovery skipped it.
func TestRefreshKeepsHiddenAndNestedRoots(t *testing.T) {
	f := setup(t)
	hidden := cloneRel(t, f, ".hidden/svc")
	nested := cloneRel(t, f, "group/repo with spaces/vendor/nested")
	spec := &WorkspaceSpec{
		SchemaVersion: 1,
		DefaultRevision: &RevisionSpec{
			Branch: "master",
		},
		Projects: []*ProjectSpec{
			{
				Path: "group/repo with spaces",
			},
			{
				Path:   hidden,
				Groups: []string{".hidden"},
			},
			{
				Path:   nested,
				Groups: []string{"group", "group/repo with spaces", "group/repo with spaces/vendor"},
			},
		},
	}
	file := saveWorkspace(t, spec)
	options := &WorkspaceRefreshOptions{
		BaseDir: f.base,
	}

	result, err := RefreshWorkspace(t.Context(), offlineClient(f), file, options)
	if err != nil || len(result.Missing) != 0 || len(result.Unlisted) != 0 {
		t.Fatalf("%+v %v", result, err)
	}
}

// TestRefreshRejectsUnsafeSelection fails the whole request before writing.
func TestRefreshRejectsUnsafeSelection(t *testing.T) {
	f := setup(t)
	file := workspaceFromScan(t, f)
	before := readBytes(t, file)
	rel := cloneRel(t, f, "lamiona/search/new-indexer")
	cases := []*WorkspaceRefreshOptions{
		{
			BaseDir: f.base,
			Add:     []string{"../escape"},
		},
		{
			BaseDir: f.base,
			Add:     []string{rel, "not/a-clone"},
		},
		{
			BaseDir: f.base,
			Add:     []string{rel},
			AddAll:  true,
		},
	}

	for _, options := range cases {
		if _, err := RefreshWorkspace(
			t.Context(),
			offlineClient(f),
			file,
			options,
		); !errors.Is(
			err,
			ErrWorkspaceRefreshUsage,
		) ||
			!bytes.Equal(readBytes(t, file), before) {
			t.Fatal(err)
		}
	}
}

// TestRefreshRejectsBrokenListedPath does not publish when a saved path is present but unusable.
func TestRefreshRejectsBrokenListedPath(t *testing.T) {
	f := setup(t)
	broken := filepath.Join(f.base, "broken")

	if err := os.MkdirAll(broken, 0o700); err != nil {
		t.Fatal(err)
	}

	write(t, filepath.Join(broken, ".git"), "not a gitfile")
	spec := &WorkspaceSpec{
		SchemaVersion: 1,
		DefaultRevision: &RevisionSpec{
			Branch: "master",
		},
		Projects: []*ProjectSpec{
			{
				Path: "group/repo with spaces",
			},
			{
				Path: "broken",
			},
		},
	}
	file := saveWorkspace(t, spec)
	before := readBytes(t, file)
	options := &WorkspaceRefreshOptions{
		BaseDir: f.base,
		AddAll:  true,
	}

	if _, err := RefreshWorkspace(t.Context(), offlineClient(f), file, options); err == nil ||
		errors.Is(err, os.ErrNotExist) || !bytes.Equal(readBytes(t, file), before) {
		t.Fatal(err)
	}
}

// TestRefreshSkipsBareAndDirectorySymlink keeps discovery rules shared with init.
func TestRefreshSkipsBareAndDirectorySymlink(t *testing.T) {
	f := setup(t)
	file := workspaceFromScan(t, f)
	git(t, f.base, "init", "--bare", filepath.Join(f.base, "bare.git"))

	if err := os.Symlink(filepath.Join(f.base, "group"), filepath.Join(f.base, "alias")); err != nil {
		t.Skip(err)
	}

	options := &WorkspaceRefreshOptions{
		BaseDir: f.base,
	}

	result, err := RefreshWorkspace(t.Context(), offlineClient(f), file, options)
	if err != nil || len(result.Unlisted) != 0 || len(result.Missing) != 0 {
		t.Fatalf("%+v %v", result, err)
	}
}

// TestRefreshPartialCloneDoesNotFetch checks that comparing a promisor clone stays offline.
func TestRefreshPartialCloneDoesNotFetch(t *testing.T) {
	run := partialClone(t)
	f := run.fixture
	calls := run.calls
	before := localGitSnapshot(t, f.repo)
	spec := oneProject(t, "group/repo with spaces", nil)
	file := saveWorkspace(t, spec)
	options := &WorkspaceRefreshOptions{
		BaseDir: f.base,
	}

	if _, err := RefreshWorkspace(t.Context(), offlineClient(f), file, options); err != nil {
		t.Fatal(err)
	}

	if _, statErr := os.Stat(calls); !os.IsNotExist(statErr) {
		t.Fatal("refresh contacted origin")
	}

	if localGitSnapshot(t, f.repo) != before {
		t.Fatal("refresh changed Git state")
	}
}

// TestRefreshLockRejectsASecondWriter leaves the first lock and the original bytes in place.
func TestRefreshLockRejectsASecondWriter(t *testing.T) {
	f := setup(t)
	file := workspaceFromScan(t, f)
	before := readBytes(t, file)
	cloneRel(t, f, "lamiona/search/new-indexer")

	path, err := canonicalWorkspaceFilename(file)
	if err != nil {
		t.Fatal(err)
	}

	unlock, err := lockWorkspaceFile(path)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(unlock)

	options := &WorkspaceRefreshOptions{
		BaseDir: f.base,
		AddAll:  true,
	}
	if _, err = RefreshWorkspace(
		t.Context(),
		offlineClient(f),
		file,
		options,
	); !errors.Is(
		err,
		errWorkspaceRefreshLock,
	) ||
		!bytes.Equal(readBytes(t, file), before) {
		t.Fatal(err)
	}

	if _, statErr := os.Lstat(path + ".lock"); statErr != nil {
		t.Fatal("second refresh removed the lock", statErr)
	}
}

// TestRefreshRefusesExternalEdit keeps the bytes written by someone else and removes the temporary file.
func TestRefreshRefusesExternalEdit(t *testing.T) {
	f := setup(t)
	file := workspaceFromScan(t, f)
	cloneRel(t, f, "lamiona/search/new-indexer")
	user := []byte("user edit\n")
	options := &WorkspaceRefreshOptions{
		BaseDir: f.base,
		AddAll:  true,
		publish: &workspacePublishHooks{
			beforeRename: func(path string) error {
				return os.WriteFile(path, user, 0o600)
			},
		},
	}

	if _, err := RefreshWorkspace(
		t.Context(),
		offlineClient(f),
		file,
		options,
	); !errors.Is(
		err,
		errWorkspaceFileChanged,
	) ||
		!bytes.Equal(readBytes(t, file), user) {
		t.Fatal(err)
	}

	path, err := canonicalWorkspaceFilename(file)
	if err != nil {
		t.Fatal(err)
	}

	if _, statErr := os.Lstat(path + ".lock"); !os.IsNotExist(statErr) {
		t.Fatal("lock left behind", statErr)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".release-align-") {
			t.Fatal("temporary file left behind", entry.Name())
		}
	}
}

// TestRefreshCanceledScanDoesNotWrite stops a canceled invocation before publication.
func TestRefreshCanceledScanDoesNotWrite(t *testing.T) {
	f := setup(t)
	file := workspaceFromScan(t, f)
	before := readBytes(t, file)
	cloneRel(t, f, "lamiona/search/new-indexer")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	options := &WorkspaceRefreshOptions{
		BaseDir: f.base,
		AddAll:  true,
	}

	if _, err := RefreshWorkspace(ctx, offlineClient(f), file, options); !errors.Is(err, context.Canceled) ||
		!bytes.Equal(readBytes(t, file), before) {
		t.Fatal(err)
	}
}

// offlineClient is the read-only Git client used by refresh.
func offlineClient(f *fixture) *gitter.Client {
	return &gitter.Client{
		LocalTimeout: f.cfg.LocalTimeout,
		NoLazyFetch:  true,
	}
}

// workspaceFromScan stores the current base directory as a new workspace file.
func workspaceFromScan(t *testing.T, f *fixture) string {
	t.Helper()

	options := &WorkspaceInitOptions{
		BaseDir: f.base,
		Branch:  "master",
	}

	spec, err := ScanWorkspace(t.Context(), offlineClient(f), options)
	if err != nil {
		t.Fatal(err)
	}

	return saveWorkspace(t, spec)
}

// saveWorkspace writes a new inventory and returns its path.
func saveWorkspace(t *testing.T, spec *WorkspaceSpec) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "workspace.yml")
	if err := CreateWorkspaceFile(path, spec); err != nil {
		t.Fatal(err)
	}

	return path
}

// cloneRel clones the fixture origin at a slash-separated path and returns that path.
func cloneRel(t *testing.T, f *fixture, relative string) string {
	t.Helper()

	dest := filepath.Join(f.base, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		t.Fatal(err)
	}

	git(t, f.base, "clone", f.remote, dest)

	return relative
}

// readBytes returns the full workspace file.
func readBytes(t *testing.T, path string) []byte {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return body
}

// setPastMtime moves the file clock backward and returns the stored timestamp.
func setPastMtime(t *testing.T, path string) time.Time {
	t.Helper()

	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	return info.ModTime()
}

// mtimeEqual reports whether path still has the timestamp captured before a no-op refresh.
func mtimeEqual(t *testing.T, path string, stamp time.Time) bool {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	return info.ModTime().Equal(stamp)
}
