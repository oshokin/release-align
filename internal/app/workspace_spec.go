package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
)

// WorkspaceSpec is an explicit inventory; it never inherits defaults.json.
type WorkspaceSpec struct {
	SchemaVersion int            `json:"schema_version"`
	Release       string         `json:"release,omitempty"`
	DefaultBranch string         `json:"default_branch,omitempty"`
	Projects      []*ProjectSpec `json:"projects"`
}

// ProjectSpec is one repository path, its groups, and an optional exact revision.
type ProjectSpec struct {
	Path     string        `json:"path"`
	Groups   []string      `json:"groups,omitempty"`
	Revision *RevisionSpec `json:"revision,omitempty"`
}

// RevisionSpec has exactly one of branch, tag, commit.
type RevisionSpec struct {
	Branch string `json:"branch,omitempty"`
	Tag    string `json:"tag,omitempty"`
	Commit string `json:"commit,omitempty"`
}

const workspaceMaxBytes = 1 << 20

var workspaceOID = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// DecodeWorkspace parses one workspace document and rejects duplicate keys.
func DecodeWorkspace(src io.Reader) (*WorkspaceSpec, error) {
	data, err := io.ReadAll(io.LimitReader(src, workspaceMaxBytes+1))
	if err != nil {
		return nil, err
	}

	if len(data) > workspaceMaxBytes {
		return nil, errWorkspaceSize
	}
	// encoding/json otherwise accepts repeated keys using the last value.
	if err = uniqueJSONKeys(json.NewDecoder(bytes.NewReader(data)), 0); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var spec *WorkspaceSpec

	if err = dec.Decode(&spec); err != nil {
		return nil, err
	}

	var trailing any

	if err = dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errWorkspaceJSON
	}

	if err = spec.Validate(); err != nil {
		return nil, err
	}

	return spec, nil
}

// LoadWorkspace reads one bounded workspace document from filename.
func LoadWorkspace(filename string) (*WorkspaceSpec, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	defer file.Close()

	loaded, err := DecodeWorkspace(file)
	if err != nil {
		return nil, err
	}

	return loaded, nil
}

// RevisionFor returns the project revision or the workspace default branch.
func (w *WorkspaceSpec) RevisionFor(p *ProjectSpec) *RevisionSpec {
	if p.Revision == nil {
		return &RevisionSpec{Branch: w.DefaultBranch}
	}

	return &RevisionSpec{Branch: p.Revision.Branch, Tag: p.Revision.Tag, Commit: p.Revision.Commit}
}

// SelectProjects uses a union of explicit paths and groups. Empty filters select all.
// Returned pointers share immutable configuration with w.
func (w *WorkspaceSpec) SelectProjects(paths, groups []string) ([]*ProjectSpec, error) {
	if err := w.Validate(); err != nil {
		return nil, err
	}
	knownPaths, knownGroups := make(map[string]bool), make(map[string]bool)
	for _, p := range w.Projects {
		knownPaths[p.Path] = true
		for _, group := range p.Groups {
			knownGroups[group] = true
		}
	}

	for _, name := range paths {
		if !knownPaths[name] {
			return nil, fmt.Errorf("%w: %q", errWorkspaceUnknownProject, name)
		}
	}

	for _, name := range groups {
		if !knownGroups[name] {
			return nil, fmt.Errorf("%w: %q", errWorkspaceUnknownGroup, name)
		}
	}
	selected := make([]*ProjectSpec, 0, len(w.Projects))
	for _, p := range w.Projects {
		include := len(paths)+len(groups) == 0 || slices.Contains(paths, p.Path)
		for _, group := range groups {
			include = include || slices.Contains(p.Groups, group)
		}

		if include {
			selected = append(selected, p)
		}
	}

	slices.SortFunc(selected, func(a, b *ProjectSpec) int { return strings.Compare(a.Path, b.Path) })

	return selected, nil
}

// WithDefaultBranch returns a copy whose implicit branch is branch.
// Explicit project revisions are unchanged.
func (w *WorkspaceSpec) WithDefaultBranch(branch string) (*WorkspaceSpec, error) {
	if w == nil {
		return nil, errWorkspaceSchema
	}

	next := w.clone()
	next.DefaultBranch = branch

	if err := next.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", errWorkspaceBranchOverride, err)
	}

	return &next, nil
}

// clone returns an independent inventory. Each project, group list, and revision is its own value.
func (w *WorkspaceSpec) clone() WorkspaceSpec {
	var projects []*ProjectSpec

	if w.Projects != nil {
		projects = make([]*ProjectSpec, len(w.Projects))
		for i, project := range w.Projects {
			projects[i] = project.clone()
		}
	}

	return WorkspaceSpec{
		SchemaVersion: w.SchemaVersion,
		Release:       w.Release,
		DefaultBranch: w.DefaultBranch,
		Projects:      projects,
	}
}

// clone returns an independent project.
func (p *ProjectSpec) clone() *ProjectSpec {
	if p == nil {
		return nil
	}

	return &ProjectSpec{
		Path:     p.Path,
		Groups:   slices.Clone(p.Groups),
		Revision: p.Revision.clone(),
	}
}

// clone returns an independent revision.
func (r *RevisionSpec) clone() *RevisionSpec {
	if r == nil {
		return nil
	}

	return &RevisionSpec{
		Branch: r.Branch,
		Tag:    r.Tag,
		Commit: r.Commit,
	}
}
