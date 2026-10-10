package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oshokin/release-align/internal/gitlab"
)

func TestReviewListedArchivedClonePersistsFlag(t *testing.T) {
	f := setup(t)
	path := "group/archived"

	git(t, f.repo, "remote", "set-url", "origin", "git@unrelated.invalid:other/service.git")
	spec := oneProject(t, path, nil)
	project := &gitlab.Project{
		ID:                1,
		PathWithNamespace: path,
		SSHURLToRepo:      f.remote,
		DefaultBranch:     "master",
		Archived:          true,
	}
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

	after, err := LoadWorkspace(file)
	if err != nil {
		t.Fatal(err)
	}

	if !after.Projects[0].Archived {
		t.Fatalf(
			"successful archived clone left listed project active: cloned=%d added=%d",
			report.Cloned,
			report.Added,
		)
	}
}

func TestReviewReusedArchivedClonePinsOriginDefault(t *testing.T) {
	f := setup(t)
	path := "group/repo with spaces"
	defaultOID := git(t, f.repo, "rev-parse", "refs/remotes/origin/master")
	git(t, f.repo, "switch", "-c", "local-work")
	write(t, filepath.Join(f.repo, "private-work"), "not the archived server version\n")
	git(t, f.repo, "add", "private-work")
	git(t, f.repo, "commit", "-m", "local work")
	head := git(t, f.repo, "rev-parse", "HEAD")
	spec := oneProject(t, "group/another", nil)
	project := &gitlab.Project{
		ID:                1,
		PathWithNamespace: path,
		SSHURLToRepo:      f.remote,
		DefaultBranch:     "master",
		Archived:          true,
	}
	server := catalogServer(t, []*gitlab.Project{project})
	spec.GitLab = &GitLabSource{URL: server.URL, Groups: []string{"group"}}
	file := saveWorkspace(t, spec)
	opts := cloneOptions(t, f.base, file, server)
	opts.Repos = []string{path}
	opts.IncludeArchived = true

	if _, err := CloneWorkspace(t.Context(), opts); err != nil {
		t.Fatal(err)
	}

	after, err := LoadWorkspace(file)
	if err != nil {
		t.Fatal(err)
	}

	var got *ProjectSpec

	for _, p := range after.Projects {
		if p.Path == path {
			got = p
		}
	}

	if got == nil || got.Revision == nil || got.Revision.Commit != defaultOID {
		t.Fatalf("reused archived entry=%+v want origin default=%s local HEAD=%s", got, defaultOID, head)
	}
}

func TestReviewArchiveLocalProjectWithoutOrigin(t *testing.T) {
	f := setup(t)
	oid := git(t, f.repo, "rev-parse", "HEAD")
	git(t, f.repo, "remote", "remove", "origin")
	pin := &RevisionSpec{Commit: oid}
	spec := oneProject(t, "group/repo with spaces", pin)
	spec.Projects[0].URL = ""
	file := saveWorkspace(t, spec)
	in := &archiveOptInput{fixture: f, workspace: file, dest: filepath.Join(t.TempDir(), "local.zip")}

	if _, err := ArchiveWorkspace(t.Context(), archiveOpts(in)); err != nil {
		t.Fatalf("local archive with empty URL acquired an origin dependency: %v", err)
	}
}

func TestReviewDeleteMustNotRemoveRetainedNestedProject(t *testing.T) {
	f := setup(t)
	parent := "group/old"
	child := parent + "/kept"
	childDir := filepath.Join(f.base, filepath.FromSlash(child))

	if err := os.MkdirAll(childDir, 0o700); err != nil {
		t.Fatal(err)
	}

	git(t, filepath.Dir(childDir), "init", "--initial-branch=master")
	git(t, childDir, "init", "--initial-branch=master")
	write(t, filepath.Join(childDir, "keep-me"), "retained child repository\n")
	file := remoteFile(t, f, remoteProjects(parent, child))
	before := string(readBytes(t, file))
	opts := remoteOptions(f, true, true, child)
	report, err := RefreshWorkspace(t.Context(), offlineClient(f), file, opts)

	if _, statErr := os.Stat(filepath.Join(childDir, "keep-me")); statErr != nil {
		t.Fatalf("delete removed retained nested project: report=%+v err=%v stat=%v", report, err, statErr)
	}

	if err == nil || string(readBytes(t, file)) != before {
		t.Fatalf("unsafe deletion should fail before workspace publication: %+v %v", report, err)
	}
}

func TestReviewArchiveRefreshAllowsUntouchedOpaqueUserdata(t *testing.T) {
	f := setup(t)
	body := "manifest:\n  defaults:\n    revision: refs/heads/master\n  projects:\n" +
		"    - name: live\n      path: group/live\n      userdata: opaque\n" +
		"    - name: old\n      path: group/old\n" +
		"release-align:\n  schema-version: 1\n  gitlab:\n    url: https://gitlab.example\n    groups: [group]\n"
	file := filepath.Join(t.TempDir(), "workspace.yml")
	write(t, file, body)
	live := &gitlab.Project{ID: 1, PathWithNamespace: "group/live", DefaultBranch: "master"}
	old := &gitlab.Project{ID: 2, PathWithNamespace: "group/old", DefaultBranch: "master", Archived: true}
	catalog := &refreshCatalog{projects: []*gitlab.Project{live, old}}
	opts := &WorkspaceRefreshOptions{BaseDir: f.base, Remote: true, Apply: true, catalog: catalog}

	if _, err := RefreshWorkspace(t.Context(), offlineClient(f), file, opts); err != nil {
		t.Fatalf("unrelated active project with opaque userdata blocks archive metadata update: %v", err)
	}
}

func TestReviewCheckedInventoryReportsArchivedTruthfully(t *testing.T) {
	f := setup(t)
	spec := oneProject(t, "group/repo with spaces", nil)
	spec.GitLab = &GitLabSource{URL: "https://gitlab.example", Groups: []string{"group"}}
	project := &gitlab.Project{
		ID:                1,
		PathWithNamespace: "group/old",
		SSHURLToRepo:      "git@gitlab.example:group/old.git",
		DefaultBranch:     "master",
		Archived:          true,
	}
	query := &projectCompare{git: offlineClient(f), base: f.base, spec: spec, projects: []*gitlab.Project{project}}

	result, err := compareProjects(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}

	if result.IncludeArchived == nil {
		t.Fatal("include_archived missing")
	}

	if !*result.IncludeArchived {
		t.Fatalf(
			"catalog contains archived project, reports include_archived=%v; catalog=%+v",
			*result.IncludeArchived,
			result.Catalog,
		)
	}
}

func TestReviewNewArchivedClonePersistsPinnedEntry(t *testing.T) {
	f := setup(t)
	path := "group/archived-new"

	git(t, f.repo, "remote", "set-url", "origin", "git@unrelated.invalid:other/service.git")
	spec := oneProject(t, "group/repo with spaces", nil)
	project := &gitlab.Project{
		ID:                1,
		PathWithNamespace: path,
		SSHURLToRepo:      f.remote,
		DefaultBranch:     "master",
		Archived:          true,
	}
	server := catalogServer(t, []*gitlab.Project{project})
	spec.GitLab = &GitLabSource{URL: server.URL, Groups: []string{"group"}}
	file := saveWorkspace(t, spec)
	opts := cloneOptions(t, f.base, file, server)
	opts.Repos = []string{path}
	opts.IncludeArchived = true

	if _, err := CloneWorkspace(t.Context(), opts); err != nil {
		t.Fatal(err)
	}

	after, err := LoadWorkspace(file)
	if err != nil {
		t.Fatal(err)
	}

	var got *ProjectSpec

	for _, p := range after.Projects {
		if p.Path == path {
			got = p
		}
	}

	if got == nil || !got.Archived || got.Revision == nil || got.Revision.Commit == "" {
		t.Fatalf("new archived clone not persisted: %+v", got)
	}
}

func TestReviewReusedArchivedCandidateUsesCachedDefault(t *testing.T) {
	f := setup(t)
	path := "group/repo with spaces"
	defaultOID := git(t, f.repo, "rev-parse", "refs/remotes/origin/master")
	git(t, f.repo, "switch", "-c", "local-work")
	write(t, filepath.Join(f.repo, "private-work"), "unpublished change\n")
	git(t, f.repo, "add", "private-work")
	git(t, f.repo, "commit", "-m", "local work")
	project := &gitlab.Project{
		ID:                1,
		PathWithNamespace: path,
		SSHURLToRepo:      f.remote,
		DefaultBranch:     "master",
		Archived:          true,
	}
	item := &cloneItem{project: project, path: path, reuse: true}
	report := &CloneReport{Paths: []string{path}}
	hooks := &remoteHooks{allowLocalClone: true}
	query := &cloneAdditionQuery{
		items:    []*cloneItem{item},
		report:   report,
		protocol: cloneProtocolSSH,
		hooks:    hooks,
		ctx:      t.Context(),
	}
	job := &cloneJob{base: f.base, client: offlineClient(f)}

	selected, err := job.cloneAdditions(query)
	if err != nil {
		t.Fatal(err)
	}

	if len(selected) != 1 {
		t.Fatalf("selected=%d", len(selected))
	}

	if selected[0].Revision == nil || selected[0].Revision.Commit != defaultOID {
		t.Fatalf(
			"archive candidate pins local HEAD instead of origin default: got=%+v want=%s",
			selected[0].Revision,
			defaultOID,
		)
	}
}

func TestReviewStashRefusesRepositorySubdirectory(t *testing.T) {
	f := setup(t)
	subdir := filepath.Join(f.repo, "ordinary-subdirectory")

	if err := os.MkdirAll(subdir, 0o700); err != nil {
		t.Fatal(err)
	}

	write(t, filepath.Join(f.repo, "file"), "outside selected directory\n")
	file := stashFile(t, "group/repo with spaces/ordinary-subdirectory")
	opts := &WorkspaceStashOptions{BaseDir: f.base}
	report, err := StashWorkspace(t.Context(), stashGit(f), file, opts)

	if err == nil || git(t, f.repo, "stash", "list") != "" ||
		string(readBytes(t, filepath.Join(f.repo, "file"))) != "outside selected directory\n" {
		t.Fatalf("stash in a non-root path changed its parent repository: report=%+v err=%v", report, err)
	}
}
