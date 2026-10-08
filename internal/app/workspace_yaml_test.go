package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWorkspaceYAMLExampleReadsWestFields checks the documented manifest and release-align block.
func TestWorkspaceYAMLExampleReadsWestFields(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "examples", "release-align.yml"))
	if err != nil {
		t.Fatal(err)
	}

	spec, err := DecodeWorkspace(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}

	if spec.Release != "Lamiona 26.3.0" || spec.DefaultRevision.Branch != "Release-26.3.0" {
		t.Fatalf("%+v", spec)
	}

	if spec.Projects[0].Path != "lamiona/search/calyra" || spec.Projects[0].Name != "calyra" ||
		spec.Projects[0].URL != "ssh://git@git.example.com/lamiona/search/calyra.git" ||
		spec.RevisionFor(spec.Projects[0]).Branch != "Release-26.3.0" {
		t.Fatalf("calyra %+v", spec.Projects[0])
	}

	if spec.Projects[1].Revision.Tag != "v26.3.1" || spec.GitLab.CloneProtocol != cloneProtocolSSH ||
		spec.Timeouts == nil || spec.Timeouts.Probe == nil || *spec.Timeouts.Probe != "5s" ||
		spec.Timeouts.Fetch == nil {
		t.Fatalf("pin or timeouts %+v", spec)
	}
}

// TestWorkspaceYAMLRemoteURLDoesNotAddGitSuffix joins url-base and repo-path as west does.
func TestWorkspaceYAMLRemoteURLDoesNotAddGitSuffix(t *testing.T) {
	raw := "" +
		"manifest:\n" +
		"  defaults:\n" +
		"    remote: company\n" +
		"    revision: refs/heads/Release-26.3.0\n" +
		"  remotes:\n" +
		"    - name: company\n" +
		"      url-base: ssh://git@gitlab.example\n" +
		"  projects:\n" +
		"    - name: calyra\n" +
		"      repo-path: lamiona/search/calyra\n" +
		"      path: lamiona/search/calyra\n" +
		"      groups: [lamiona, lamiona/search]\n"

	spec, err := DecodeWorkspace(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}

	if spec.Projects[0].URL != "ssh://git@gitlab.example/lamiona/search/calyra" {
		t.Fatal(spec.Projects[0].URL)
	}

	conflict := strings.Replace(
		raw,
		"repo-path: lamiona/search/calyra\n",
		"url: ssh://git@host/a.git\n      repo-path: a\n",
		1,
	)
	if _, err = DecodeWorkspace(strings.NewReader(conflict)); err == nil {
		t.Fatal("url and repo-path were both accepted")
	}
}

// TestWorkspaceYAMLGroupFilterKeepsInactiveProjects applies the filter without deleting rows.
func TestWorkspaceYAMLGroupFilterKeepsInactiveProjects(t *testing.T) {
	raw := "" +
		"manifest:\n" +
		"  defaults:\n" +
		"    revision: refs/heads/master\n" +
		"  group-filter: [-search]\n" +
		"  projects:\n" +
		"    - name: hidden\n" +
		"      path: search/hidden\n" +
		"      groups: [search]\n" +
		"    - name: shown\n" +
		"      path: storage/shown\n" +
		"      groups: [storage]\n"

	spec, err := DecodeWorkspace(strings.NewReader(raw))
	if err != nil || len(spec.Projects) != 2 {
		t.Fatal(spec, err)
	}

	selected, err := spec.SelectProjects(nil, nil)
	if err != nil || len(selected) != 1 || selected[0].Path != "storage/shown" {
		t.Fatal(selected, err)
	}

	if _, err = spec.SelectProjects([]string{"search/hidden"}, nil); err == nil {
		t.Fatal("inactive project was selected")
	}
}

// TestWorkspaceYAMLQuotedSHAKeepsLeadingZeros and a full branch ref stays a branch.
func TestWorkspaceYAMLQuotedSHAKeepsLeadingZeros(t *testing.T) {
	commit := "00000000000000000000000000000000000000ab"
	raw := "manifest:\n  projects:\n    - name: lib\n      path: libraries/lib\n      revision: \"" +
		commit + "\"\n    - name: hex\n      path: libraries/hex\n      revision: refs/heads/" +
		commit + "\n"

	spec, err := DecodeWorkspace(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}

	if spec.Projects[0].Revision.Commit != commit || spec.Projects[1].Revision.Branch != commit {
		t.Fatalf("%+v %+v", spec.Projects[0].Revision, spec.Projects[1].Revision)
	}

	if spec.RevisionFor(spec.Projects[0]).Commit != commit {
		t.Fatal("omitted default replaced a pin")
	}
}

// TestWorkspaceYAMLRejectsImportAnchorAndPreservesComment stops before Git and keeps a comment.
func TestWorkspaceYAMLRejectsImportAnchorAndPreservesComment(t *testing.T) {
	imported := "manifest:\n  self:\n    import: child.yml\n  projects:\n    - name: a\n      path: a/b\n"
	if _, err := DecodeWorkspace(strings.NewReader(imported)); err == nil {
		t.Fatal("import accepted")
	}

	anchored := "manifest: &m\n  projects: []\n"
	if _, err := DecodeWorkspace(strings.NewReader(anchored)); err == nil {
		t.Fatal("anchor accepted")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "workspace.yml")
	body := "manifest:\n  defaults:\n    revision: refs/heads/master\n  projects:\n    # keep me\n    - name: old\n      path: group/old\nrelease-align:\n  schema-version: 1\n"

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	document, err := readWorkspaceDocument(path)
	if err != nil {
		t.Fatal(err)
	}

	next := document.spec.clone()
	next.Projects = append(next.Projects, &ProjectSpec{
		Name: "added",
		Path: "group/added",
	})

	if err = publishWorkspace(t.Context(), document, next, nil); err != nil {
		t.Fatal(err)
	}

	written, err := os.ReadFile(path)
	text := string(written)
	kept := err == nil && strings.Contains(text, "# keep me") && strings.Contains(text, "group/added")

	if !kept {
		t.Fatalf("%s %v", written, err)
	}
}
