package app

import (
	"reflect"
	"testing"
)

// TestAppendDiscoveredProjectsPreservesIntent checks manual groups, pins, order and ownership.
func TestAppendDiscoveredProjectsPreservesIntent(t *testing.T) {
	revision := &RevisionSpec{
		Tag: "v26.3.0",
	}
	existing := &ProjectSpec{
		Path:     "z/old",
		Groups:   []string{"manual-team"},
		Revision: revision,
	}
	current := &WorkspaceSpec{
		SchemaVersion: 1,
		Release:       "Mailion 26.3",
		DefaultRevision: &RevisionSpec{
			Branch: "Release-26.3.0",
		},
		Projects: []*ProjectSpec{existing},
	}
	rediscovered := &ProjectSpec{
		Path:   "z/old",
		Groups: []string{"z"},
	}
	newB := &ProjectSpec{
		Path:   "b/new",
		Groups: []string{"b"},
	}
	newA := &ProjectSpec{
		Path:   "a/new",
		Groups: []string{"a"},
	}
	selected := []*ProjectSpec{rediscovered, newB, newA, newA}

	next, added, err := AppendDiscoveredProjects(current, selected)
	if err != nil {
		t.Fatal(err)
	}

	wantAdded := []string{"a/new", "b/new"}
	if !reflect.DeepEqual(added, wantAdded) || !reflect.DeepEqual(next.Projects[0], existing) {
		t.Fatalf("intent/order lost: next=%+v added=%v", next, added)
	}

	if next.Release != current.Release || next.DefaultRevision.Branch != current.DefaultRevision.Branch ||
		next.Projects[1].Revision != nil {
		t.Fatal("metadata or default revision inheritance changed")
	}

	next.Projects[0].Revision.Tag = "changed"
	next.Projects[0].Groups[0] = "changed"
	next.Projects[1].Groups[0] = "changed"

	if revision.Tag != "v26.3.0" || existing.Groups[0] != "manual-team" || newA.Groups[0] != "a" ||
		len(current.Projects) != 1 {
		t.Fatal("result aliases input structures")
	}
}

// TestAppendDiscoveredProjectsRequiresDefault checks pinned-only workspaces without inventing a branch.
func TestAppendDiscoveredProjectsRequiresDefault(t *testing.T) {
	revision := &RevisionSpec{
		Tag: "v1",
	}
	existing := &ProjectSpec{
		Path:     "old",
		Revision: revision,
	}
	current := &WorkspaceSpec{
		SchemaVersion: 1,
		Projects:      []*ProjectSpec{existing},
	}
	candidate := &ProjectSpec{
		Path: "new",
	}
	selected := []*ProjectSpec{candidate}

	if _, _, err := AppendDiscoveredProjects(current, selected); err == nil {
		t.Fatal("new project without desired revision was accepted")
	}

	if _, added, err := AppendDiscoveredProjects(current, nil); err != nil || len(added) != 0 {
		t.Fatalf("no-op failed: added=%v err=%v", added, err)
	}
}

// TestAppendDiscoveredProjectsRejectsInvalidCandidates keeps unsafe paths and invented pins out.
func TestAppendDiscoveredProjectsRejectsInvalidCandidates(t *testing.T) {
	existing := &ProjectSpec{
		Path: "old",
	}
	current := &WorkspaceSpec{
		SchemaVersion: 1,
		DefaultRevision: &RevisionSpec{
			Branch: "release",
		},
		Projects: []*ProjectSpec{existing},
	}
	unsafe := &ProjectSpec{
		Path: "../escape",
	}
	revision := &RevisionSpec{
		Branch: "invented",
	}
	pinned := &ProjectSpec{
		Path:     "new",
		Revision: revision,
	}
	badGroups := &ProjectSpec{
		Path:   "new",
		Groups: []string{"bad\nname"},
	}
	candidates := []*ProjectSpec{nil, unsafe, pinned, badGroups}

	for _, candidate := range candidates {
		selected := []*ProjectSpec{candidate}
		if _, _, err := AppendDiscoveredProjects(current, selected); err == nil {
			t.Fatalf("accepted invalid discovery: %+v", candidate)
		}
	}
}
