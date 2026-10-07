package app

import (
	"fmt"
	"path"
	"strings"
)

// Validate checks the inventory shape before any Git command runs.
func (w *WorkspaceSpec) Validate() error {
	if w == nil || w.SchemaVersion != 1 || len(w.Projects) == 0 {
		return errWorkspaceSchema
	}

	branch := &RevisionSpec{Branch: w.DefaultBranch}
	if w.DefaultBranch != "" && !branch.safeRevisionName() {
		return errWorkspaceDefaultBranch
	}
	seen := make(map[string]bool)
	for _, p := range w.Projects {
		if err := w.validateProject(p, seen); err != nil {
			return err
		}
	}

	return nil
}

// validateProject checks one project path, revision, and group list.
func (w *WorkspaceSpec) validateProject(p *ProjectSpec, seen map[string]bool) error {
	if p == nil || !canonicalProjectPath(p.Path) || seen[p.Path] {
		return errWorkspaceProjectPaths
	}

	seen[p.Path] = true

	if err := w.validateProjectRevision(p); err != nil {
		return err
	}

	return w.validateProjectGroups(p)
}

// validateProjectRevision requires a revision or a workspace default branch.
func (w *WorkspaceSpec) validateProjectRevision(p *ProjectSpec) error {
	if p.Revision == nil && w.DefaultBranch == "" {
		return fmt.Errorf("%s: %w", p.Path, errWorkspaceNoDefault)
	}

	if p.Revision == nil {
		return nil
	}

	if err := p.Revision.Validate(); err != nil {
		return fmt.Errorf("%s: %w", p.Path, err)
	}

	return nil
}

// validateProjectGroups rejects empty, repeated, or multiline group names.
func (w *WorkspaceSpec) validateProjectGroups(p *ProjectSpec) error {
	groups := make(map[string]bool)

	for _, group := range p.Groups {
		if !w.acceptableGroup(group, groups) {
			return fmt.Errorf("%s: %w", p.Path, errWorkspaceGroup)
		}

		groups[group] = true
	}

	return nil
}

// acceptableGroup reports a group name that can be stored.
func (*WorkspaceSpec) acceptableGroup(group string, seen map[string]bool) bool {
	if group == "" || strings.TrimSpace(group) != group || seen[group] {
		return false
	}

	return !strings.ContainsAny(group, "\x00\r\n")
}

// canonicalProjectPath reports a relative slash path with no escapes.
func canonicalProjectPath(s string) bool {
	if s == "" || s == "." || path.IsAbs(s) || path.Clean(s) != s || strings.TrimSpace(s) != s ||
		strings.ContainsAny(s, "\\:\x00\r\n") {
		return false
	}

	for part := range strings.SplitSeq(s, "/") {
		if part == ".." || part == ".git" {
			return false
		}
	}

	return true
}

// Validate requires exactly one of branch, tag, or a full object id.
func (r *RevisionSpec) Validate() error {
	if r == nil {
		return errWorkspaceRevisionMissing
	}
	n := 0
	fields := []string{r.Branch, r.Tag, r.Commit}

	for _, value := range fields {
		if value != "" {
			n++
		}
	}

	if n != 1 {
		return errWorkspaceRevisionCount
	}

	if r.Commit != "" && !workspaceOID.MatchString(r.Commit) {
		return errWorkspaceCommit
	}

	if r.Commit != "" {
		return nil
	}

	if !r.safeRevisionName() {
		return errWorkspaceRevisionName
	}

	return nil
}

// safeRevisionName rejects names Git would treat as options, ref paths, or whitespace.
// ResolveRevision also asks Git to validate the ref.
func (r *RevisionSpec) safeRevisionName() bool {
	name := r.Branch
	if name == "" {
		name = r.Tag
	}

	return name != "" && !strings.HasPrefix(name, "-") && !strings.HasPrefix(name, "refs/") &&
		!strings.Contains(name, "@{") && !strings.ContainsAny(name, "\x00\r\n\t ")
}
