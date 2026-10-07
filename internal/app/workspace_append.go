package app

import (
	"errors"
	"slices"
	"strings"
)

// errWorkspaceAppendCandidate means a discovered project cannot be appended.
var errWorkspaceAppendCandidate = errors.New("invalid discovered workspace project")

// AppendDiscoveredProjects preserves existing entries and appends selected discoveries.
// The caller verifies local roots and selects candidates before calling this pure function.
// No Git command or file write is performed; an empty added slice means no file write is needed.
func AppendDiscoveredProjects(current *WorkspaceSpec, selected []*ProjectSpec) (*WorkspaceSpec, []string, error) {
	if err := current.Validate(); err != nil {
		return nil, nil, err
	}

	next := current.clone()
	known := make(map[string]bool, len(next.Projects))

	for _, project := range next.Projects {
		known[project.Path] = true
	}

	additions, err := discoveredAdditions(selected, known)
	if err != nil {
		return nil, nil, err
	}

	slices.SortFunc(additions, func(left, right *ProjectSpec) int {
		return strings.Compare(left.Path, right.Path)
	})

	added := make([]string, 0, len(additions))
	for _, project := range additions {
		next.Projects = append(next.Projects, project)
		added = append(added, project.Path)
	}

	if err = next.Validate(); err != nil {
		return nil, nil, err
	}

	return next, added, nil
}

// discoveredAdditions copies new projects in caller order. Existing paths are skipped.
func discoveredAdditions(selected []*ProjectSpec, known map[string]bool) ([]*ProjectSpec, error) {
	additions := make([]*ProjectSpec, 0, len(selected))

	for _, candidate := range selected {
		if candidate == nil || !canonicalProjectPath(candidate.Path) || candidate.Revision != nil {
			return nil, errWorkspaceAppendCandidate
		}

		if known[candidate.Path] {
			continue
		}

		known[candidate.Path] = true
		additions = append(additions, candidate.clone())
	}

	return additions, nil
}
