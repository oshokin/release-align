package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/oshokin/release-align/internal/gitlab"
)

// reviewCanceledOrigin emulates cancellation during origin inspection after discovery succeeded.
type reviewCanceledOrigin struct{}

// TestReviewYAMLSelectedRevisions ignores missing repositories outside the requested group.
func TestReviewYAMLSelectedRevisions(t *testing.T) {
	f := setup(t)
	file := reviewYAML(t, reviewShortManifest())

	spec, err := LoadWorkspace(file)
	if err != nil {
		t.Fatal(err)
	}
	f.cfg.Groups = []string{"chosen"}
	report, err := RunWorkspace(t.Context(), f.cfg, spec, ModeStatus)

	if err != nil || !report.Ready || report.ExpectedCount != 1 {
		t.Fatalf("status: %+v, %v", report, err)
	}
	opts := archiveOpts(f, file, filepath.Join(t.TempDir(), "selected.zip"), nil, []string{"chosen"})
	if _, err = ArchiveWorkspace(t.Context(), opts); err != nil {
		t.Fatal("archive:", err)
	}
}

// TestReviewYAMLRefreshMissingShortRevision reports missing clones instead of resolving their refs.
func TestReviewYAMLRefreshMissingShortRevision(t *testing.T) {
	f := setup(t)
	file := reviewYAML(t, reviewShortManifest())
	opts := &WorkspaceRefreshOptions{
		BaseDir: f.base,
	}
	report, err := RefreshWorkspace(t.Context(), offlineClient(f), file, opts)

	if err != nil || len(report.Missing) != 1 || report.Missing[0] != "group/missing" {
		t.Fatalf("%+v, %v", report, err)
	}

	cloneRel(t, f, "group/added")
	opts.AddAll = true
	report, err = RefreshWorkspace(t.Context(), offlineClient(f), file, opts)

	if err != nil || !report.Written || len(report.Added) != 1 {
		t.Fatalf("append: %+v, %v", report, err)
	}
	spec, err := LoadWorkspace(file)
	if err != nil || spec.shortDefault != "master" || spec.Projects[0].Revision != nil {
		t.Fatal("refresh changed inheritance", err)
	}
}

// TestReviewYAMLCloneMissingShortRevision does not require refs in a directory it must create.
func TestReviewYAMLCloneMissingShortRevision(t *testing.T) {
	f := setup(t)
	git(t, f.repo, "remote", "set-url", "origin", "git@gitlab.example:group/existing.git")
	project := &gitlab.Project{
		ID:                1,
		PathWithNamespace: "group/missing",
		SSHURLToRepo:      f.remote,
		DefaultBranch:     "master",
	}
	projects := []*gitlab.Project{project}
	server := catalogServer(t, projects)

	body := reviewShortManifest() + "release-align:\n  schema-version: 1\n  gitlab:\n    url: " +
		server.URL + "\n    groups: [group]\n"
	file := reviewYAML(t, body)
	opts := cloneOptions(t, f.base, file, server)
	opts.Repos = []string{"group/missing"}
	report, err := CloneWorkspace(t.Context(), opts)

	if err != nil || report.Cloned != 1 {
		t.Fatalf("%+v, %v", report, err)
	}
}

// TestReviewYAMLRemoteBranchAndOverride covers detached west checkouts and inherited branch overrides.
func TestReviewYAMLRemoteBranchAndOverride(t *testing.T) {
	f := setup(t)
	git(t, f.repo, "checkout", "--detach")
	git(t, f.repo, "branch", "-D", "master")
	file := reviewYAML(t, strings.ReplaceAll(reviewShortManifest(),
		"    - name: missing\n      path: group/missing\n      groups: [other]\n", ""))

	spec, err := LoadWorkspace(file)
	if err != nil {
		t.Fatal(err)
	}

	if err = ResolveWorkspaceRevisions(t.Context(), offlineClient(f), f.base, spec, spec.Projects); err != nil {
		t.Fatal(err)
	}

	if got := spec.RevisionFor(spec.Projects[0]); got == nil || got.Branch != "master" {
		t.Fatalf("resolved: %+v", got)
	}
	overridden, err := spec.WithDefaultBranch("Release-26.3.0")
	if err != nil || overridden.RevisionFor(overridden.Projects[0]).Branch != "Release-26.3.0" {
		t.Fatal("lookup converted inherited default into a pin", err)
	}

	git(t, f.repo, "tag", "master")

	if _, err = classifyShortRevision(t.Context(), offlineClient(f), f.repo, "master"); err == nil {
		t.Fatal("ambiguous branch/tag accepted")
	}
}

// TestReviewYAMLScalarRoundTrip preserves strings which YAML otherwise interprets as typed values.
func TestReviewYAMLScalarRoundTrip(t *testing.T) {
	for _, value := range []string{"123", "true", "null", "2026-10-07", "on"} {
		t.Run(value, func(t *testing.T) {
			spec := oneProject(t, "group/"+value, nil)
			spec.Release = value
			spec.Projects[0].Groups = []string{value}
			file := saveWorkspace(t, spec)
			loaded, err := LoadWorkspace(file)

			if err != nil || loaded.Release != value || loaded.Projects[0].Name != value ||
				loaded.Projects[0].Groups[0] != value {
				t.Fatal(loaded, err)
			}
		})
	}
}

// TestReviewYAMLNumericCommit accepts a full numeric object id without converting its lexical value.
func TestReviewYAMLNumericCommit(t *testing.T) {
	commit := strings.Repeat("0", 39) + "1"
	body := "manifest:\n  projects:\n    - name: a\n      revision: " + commit + "\n"
	spec, err := DecodeWorkspace(strings.NewReader(body))

	if err != nil || spec.Projects[0].Revision.Commit != commit {
		t.Fatal(spec, err)
	}
}

// TestReviewYAMLFalseSubmodules accepts the explicitly disabled west option.
func TestReviewYAMLFalseSubmodules(t *testing.T) {
	body := "manifest:\n  group-filter: []\n  projects:\n    - name: a\n      submodules: false\n"
	if _, err := DecodeWorkspace(strings.NewReader(body)); err != nil {
		t.Fatal(err)
	}
}

// TestReviewYAMLFreshEncodingPreservesIntent keeps filters, unresolved pins, and metadata.
func TestReviewYAMLFreshEncodingPreservesIntent(t *testing.T) {
	body := `manifest:
  defaults:
    revision: release
  group-filter: [-optional]
  projects:
    - name: a
      groups: [optional]
      revision: another-release
      clone-depth: 2
`

	spec, err := DecodeWorkspace(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	file := saveWorkspace(t, spec)
	loaded, err := LoadWorkspace(file)

	if err != nil || loaded.shortDefault != "release" || len(loaded.GroupFilter) != 1 ||
		loaded.Projects[0].shortRevision != "another-release" || loaded.Projects[0].CloneDepth == nil {
		t.Fatalf("lost intent: %+v, %v", loaded, err)
	}
}

// TestReviewYAMLKeyAnchorRejects accepts no anchors, including ones attached to mapping keys.
func TestReviewYAMLKeyAnchorRejects(t *testing.T) {
	body := "manifest:\n  &projects projects:\n    - name: a\n"
	if _, err := DecodeWorkspace(strings.NewReader(body)); err == nil {
		t.Fatal("mapping key anchor accepted")
	}
}

// TestReviewYAMLOriginCancellationPreserved must not downgrade cancellation to a missing URL.
func TestReviewYAMLOriginCancellationPreserved(t *testing.T) {
	client := new(reviewCanceledOrigin)
	_, err := readOriginURL(t.Context(), client, t.TempDir())

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
}

// TestReviewYAMLRefreshWithoutOrigin accepts inventories created by init with missing origin URLs.
func TestReviewYAMLRefreshWithoutOrigin(t *testing.T) {
	f := setup(t)
	git(t, f.repo, "remote", "remove", "origin")
	file := workspaceFromScan(t, f)
	opts := &WorkspaceRefreshOptions{
		BaseDir: f.base,
	}
	result, err := RefreshWorkspace(t.Context(), offlineClient(f), file, opts)

	if err != nil || len(result.Missing) != 0 || result.Written {
		t.Fatal(result, err)
	}
}

// TestReviewYAMLRejectsUnsupportedVersions never silently accepts a newer manifest contract.
func TestReviewYAMLRejectsUnsupportedVersions(t *testing.T) {
	for _, version := range []string{"999.0", "banana", "0.9"} {
		body := "manifest:\n  version: \"" + version + "\"\n  projects:\n    - name: a\n"
		if _, err := DecodeWorkspace(strings.NewReader(body)); err == nil {
			t.Fatal("unsupported version accepted:", version)
		}
	}
}

// TestReviewYAMLNameCollisionSuffix handles existing names that already occupy generated suffixes.
func TestReviewYAMLNameCollisionSuffix(t *testing.T) {
	path := "other/service"
	sum := sha256.Sum256([]byte(path))
	prefix := "service-" + hex.EncodeToString(sum[:4])
	used := map[string]bool{
		"service":     true,
		prefix:        true,
		prefix + "-2": true,
	}

	if got := uniqueProjectName(path, used); got != prefix+"-3" {
		t.Fatal("duplicate generated name:", got)
	}
}

// TestReviewYAMLWriterQuotesWestBooleans keeps YAML 1.1 strings recognizable by west.
func TestReviewYAMLWriterQuotesWestBooleans(t *testing.T) {
	spec := oneProject(t, "on", nil)
	spec.Release = "1:20"
	data, err := encodeWorkspace(spec, nil)

	if err != nil || !strings.Contains(string(data), `name: "on"`) ||
		!strings.Contains(string(data), `release: "1:20"`) {
		t.Fatalf("%s, %v", data, err)
	}

	if !strings.Contains(string(data), "\n  projects:\n    - name:") {
		t.Fatalf("unexpected indentation: %s", data)
	}
}

// TestReviewYAMLAppendPreservesMetadata keeps foreign blocks, timestamps, inactive projects and pins.
func TestReviewYAMLAppendPreservesMetadata(t *testing.T) {
	body := `# workspace comment
manifest:
  version: "1.0"
  defaults:
    revision: refs/heads/master
  group-filter: [-optional]
  self:
    userdata:
      date: 2026-10-07
  projects:
    - name: old
      path: group/old
      groups: [optional]
      revision: refs/tags/v1
      userdata: {owner: search, enabled: true}
release-align:
  schema-version: 1
  timeouts: {clone: 23m, local: 42s}
foreign-tool:
  answer: 42
`
	file := reviewYAML(t, body)

	document, err := readWorkspaceDocument(file)
	if err != nil {
		t.Fatal(err)
	}
	addition := &ProjectSpec{
		Path: "group/new",
	}

	additions := []*ProjectSpec{addition}

	next, _, err := AppendDiscoveredProjects(document.spec, additions)
	if err != nil {
		t.Fatal(err)
	}

	if err = publishWorkspace(t.Context(), document, next, nil); err != nil {
		t.Fatal(err)
	}

	var before, after map[string]any

	if err = yaml.Unmarshal([]byte(body), &before); err != nil {
		t.Fatal(err)
	}
	written := readBytes(t, file)
	if err = yaml.Unmarshal(written, &after); err != nil {
		t.Fatal(err)
	}
	// The only semantic change is one appended project.
	afterManifest, ok := after["manifest"].(map[string]any)
	if !ok {
		t.Fatal("missing manifest mapping")
	}

	projects, ok := afterManifest["projects"].([]any)
	if !ok {
		t.Fatal("missing projects sequence")
	}

	if len(projects) != 2 {
		t.Fatal(projects)
	}
	afterManifest["projects"] = projects[:1]

	beforeBytes, err := yaml.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	afterBytes, err := yaml.Marshal(after)
	if err != nil || string(beforeBytes) != string(afterBytes) ||
		!strings.Contains(string(written), "# workspace comment") {
		t.Fatalf("metadata changed: %s, %v", written, err)
	}
}

// reviewYAML writes an input document without passing through the production encoder.
func reviewYAML(t *testing.T, body string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "workspace.yml")
	write(t, file, body)

	return file
}

// reviewShortManifest includes a missing project to exercise selection and inventory independently.
func reviewShortManifest() string {
	return `manifest:
  defaults:
    revision: master
  projects:
    - name: existing
      path: group/repo with spaces
      groups: [chosen]
    - name: missing
      path: group/missing
      groups: [other]
`
}

// Local returns cancellation for the origin read.
func (*reviewCanceledOrigin) Local(context.Context, string, ...string) (string, error) {
	return "", context.Canceled
}
