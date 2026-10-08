package app

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// WorkspaceSpec is the project inventory loaded from a YAML manifest.
type WorkspaceSpec struct {
	// SchemaVersion is the release-align document version. A plain manifest still uses 1.
	SchemaVersion int
	// Release is an optional label stored with the inventory.
	Release string
	// BaseDir is the absolute root recorded by workspace init.
	BaseDir string
	// DefaultRevision is the inherited revision. Nil with implicitMaster means west's master.
	DefaultRevision *RevisionSpec
	// shortDefault is a defaults.revision that still needs a local ref lookup.
	shortDefault string
	// implicitMaster reports that the file omitted defaults.revision, so west's master applies.
	implicitMaster bool
	// GitLab is the optional server scope for --remote and workspace clone.
	GitLab *GitLabSource
	// Timeouts holds optional duration overrides. Omitted fields keep the program defaults.
	Timeouts *WorkspaceTimeouts
	// GroupFilter is the west group-filter in file order. Nil leaves every group enabled.
	GroupFilter []*GroupFilter
	// Projects are the repositories in file order, including inactive ones.
	Projects []*ProjectSpec
	// URLGaps lists paths whose origin URL was left empty.
	URLGaps []string
	// GitLabURLFromBase reports that init took the GitLab URL from the base directory name.
	GitLabURLFromBase bool
}

// GroupFilter is one west group-filter entry. The last entry for a name wins.
type GroupFilter struct {
	// Name is the group without a leading sign.
	Name string
	// Disable reports that this entry turns the group off.
	Disable bool
}

// GitLabSource is the optional server scope used by --remote and workspace clone.
// It is not inferred from project groups.
type GitLabSource struct {
	// URL is the HTTPS origin of one GitLab host.
	URL string
	// Groups are exact namespace paths. Subgroups are included.
	Groups []string
	// CloneProtocol is ssh or https. Empty means ssh.
	CloneProtocol string
}

// ProjectSpec is one repository path, its groups, and an optional exact revision.
type ProjectSpec struct {
	// Name is the unique west project name. It is not the --repo selector.
	Name string
	// Path is the repository path relative to the base directory.
	Path string
	// URL is the clone URL recorded for west. Empty means it was not known.
	URL string
	// Groups are directory prefixes used by --group. They are not GitLab groups.
	Groups []string
	// Revision pins one branch, tag, or commit. Nil uses the workspace default.
	Revision *RevisionSpec
	// shortRevision is a project revision that still needs a local ref lookup.
	shortRevision string
	// resolvedRevision is a per-run lookup result, never an explicit pin in the document.
	resolvedRevision *RevisionSpec
	// CloneDepth is west metadata. workspace clone refuses a file that sets it.
	CloneDepth *int
}

// RevisionSpec has exactly one of branch, tag, commit.
type RevisionSpec struct {
	// Branch is a branch name to check out, without refs/heads/.
	Branch string
	// Tag is a tag name to check out, without refs/tags/.
	Tag string
	// Commit is a full commit id to check out.
	Commit string
}

const (
	// workspaceMaxBytes is the largest workspace document the decoder accepts.
	workspaceMaxBytes = 1 << 20
	// westDefaultBranch is the revision west uses when a manifest omits one.
	westDefaultBranch = "master"
	// yamlNestLimit is the deepest mapping or sequence the reader accepts.
	yamlNestLimit = 32
)

// workspaceOID matches a full Git object id of 40 or 64 hex digits.
var workspaceOID = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// RevisionFor returns the project pin or the inherited revision.
// A short west name is filled in by ResolveWorkspaceRevisions before this is used.
func (w *WorkspaceSpec) RevisionFor(p *ProjectSpec) *RevisionSpec {
	if p != nil && p.Revision != nil {
		return p.Revision.clone()
	}

	if p != nil && p.resolvedRevision != nil {
		return p.resolvedRevision.clone()
	}

	if w != nil && w.DefaultRevision != nil {
		return w.DefaultRevision.clone()
	}

	if w != nil && w.implicitMaster {
		revision := &RevisionSpec{
			Branch: westDefaultBranch,
		}

		return revision
	}

	return nil
}

// SelectProjects uses a union of explicit paths and groups on the active projects.
// Empty filters select every active project. Inactive projects stay in the file.
// Returned pointers share configuration with w.
func (w *WorkspaceSpec) SelectProjects(paths, groups []string) ([]*ProjectSpec, error) {
	if err := w.Validate(); err != nil {
		return nil, err
	}

	if err := w.checkSelection(paths, groups); err != nil {
		return nil, err
	}

	selected := make([]*ProjectSpec, 0, len(w.Projects))
	for _, p := range w.Projects {
		if w.projectActive(p) && projectSelected(p, paths, groups) {
			selected = append(selected, p)
		}
	}

	slices.SortFunc(selected, func(a, b *ProjectSpec) int { return strings.Compare(a.Path, b.Path) })

	return selected, nil
}

// disabledGroups applies group-filter in order. The last entry for a name wins.
func disabledGroups(filters []*GroupFilter) map[string]bool {
	disabled := make(map[string]bool)

	for _, filter := range filters {
		if filter == nil {
			continue
		}

		if filter.Disable {
			disabled[filter.Name] = true

			continue
		}

		delete(disabled, filter.Name)
	}

	return disabled
}

// projectSelected reports that the CLI filters include one active project.
func projectSelected(project *ProjectSpec, paths, groups []string) bool {
	if len(paths)+len(groups) == 0 || slices.Contains(paths, project.Path) {
		return true
	}

	for _, group := range groups {
		if slices.Contains(project.Groups, group) {
			return true
		}
	}

	return false
}

// WithDefaultBranch returns a copy whose implicit branch is branch.
// Explicit project revisions are unchanged.
func (w *WorkspaceSpec) WithDefaultBranch(branch string) (*WorkspaceSpec, error) {
	if w == nil {
		return nil, errWorkspaceSchema
	}

	next := w.clone()
	next.DefaultRevision = &RevisionSpec{
		Branch: branch,
	}
	next.shortDefault = ""
	next.implicitMaster = false

	for _, project := range next.Projects {
		if project.shortRevision == "" {
			project.resolvedRevision = nil
		}
	}

	if err := next.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", errWorkspaceBranchOverride, err)
	}

	return next, nil
}

// clone returns an independent inventory. Each project, group list, and revision is its own value.
func (w *WorkspaceSpec) clone() *WorkspaceSpec {
	var projects []*ProjectSpec

	if w.Projects != nil {
		projects = make([]*ProjectSpec, len(w.Projects))
		for i, project := range w.Projects {
			projects[i] = project.clone()
		}
	}

	cloned := &WorkspaceSpec{
		SchemaVersion:     w.SchemaVersion,
		Release:           w.Release,
		BaseDir:           w.BaseDir,
		DefaultRevision:   w.DefaultRevision.clone(),
		shortDefault:      w.shortDefault,
		implicitMaster:    w.implicitMaster,
		GitLab:            w.GitLab.clone(),
		Timeouts:          w.Timeouts.clone(),
		GroupFilter:       cloneGroupFilters(w.GroupFilter),
		Projects:          projects,
		URLGaps:           slices.Clone(w.URLGaps),
		GitLabURLFromBase: w.GitLabURLFromBase,
	}

	return cloned
}

// hasDefault reports that an unpinned project has a revision to inherit.
func (w *WorkspaceSpec) hasDefault() bool {
	return w != nil && (w.DefaultRevision != nil || w.shortDefault != "" || w.implicitMaster)
}

// checkSelection rejects unknown selectors and an explicit inactive project or group.
func (w *WorkspaceSpec) checkSelection(paths, groups []string) error {
	knownPaths := make(map[string]*ProjectSpec, len(w.Projects))
	knownGroups := make(map[string]bool)
	activeGroups := make(map[string]bool)

	for _, p := range w.Projects {
		knownPaths[p.Path] = p
		active := w.projectActive(p)

		for _, group := range p.Groups {
			knownGroups[group] = true
			if active {
				activeGroups[group] = true
			}
		}
	}

	for _, name := range paths {
		project, ok := knownPaths[name]
		if !ok {
			return fmt.Errorf("%w: %q", errWorkspaceUnknownProject, name)
		}

		if !w.projectActive(project) {
			return fmt.Errorf("%w: %q", errWorkspaceInactive, name)
		}
	}

	for _, name := range groups {
		if !knownGroups[name] {
			return fmt.Errorf("%w: %q", errWorkspaceUnknownGroup, name)
		}

		if !activeGroups[name] {
			return fmt.Errorf("%w: %q", errWorkspaceInactive, name)
		}
	}

	return nil
}

// projectActive reports that group-filter leaves the project enabled.
// A project with no groups stays active.
func (w *WorkspaceSpec) projectActive(project *ProjectSpec) bool {
	if project == nil || len(project.Groups) == 0 {
		return project != nil
	}

	disabled := disabledGroups(w.GroupFilter)
	for _, group := range project.Groups {
		if !disabled[group] {
			return true
		}
	}

	return false
}

// cloneGroupFilters copies the group-filter list.
func cloneGroupFilters(filters []*GroupFilter) []*GroupFilter {
	if filters == nil {
		return nil
	}

	out := make([]*GroupFilter, len(filters))
	for i, filter := range filters {
		if filter == nil {
			continue
		}

		cloned := &GroupFilter{
			Name:    filter.Name,
			Disable: filter.Disable,
		}
		out[i] = cloned
	}

	return out
}

// clone returns an independent GitLab source, including its group slice.
func (s *GitLabSource) clone() *GitLabSource {
	if s == nil {
		return nil
	}

	cloned := &GitLabSource{
		URL:           s.URL,
		Groups:        slices.Clone(s.Groups),
		CloneProtocol: s.CloneProtocol,
	}

	return cloned
}

// clone returns an independent project.
func (p *ProjectSpec) clone() *ProjectSpec {
	if p == nil {
		return nil
	}

	cloned := &ProjectSpec{
		Name:             p.Name,
		Path:             p.Path,
		URL:              p.URL,
		Groups:           slices.Clone(p.Groups),
		Revision:         p.Revision.clone(),
		shortRevision:    p.shortRevision,
		resolvedRevision: p.resolvedRevision.clone(),
		CloneDepth:       cloneDepth(p.CloneDepth),
	}

	return cloned
}

// cloneDepth copies one optional west clone-depth.
func cloneDepth(depth *int) *int {
	if depth == nil {
		return nil
	}

	value := *depth

	return &value
}

// clone returns an independent revision.
func (r *RevisionSpec) clone() *RevisionSpec {
	if r == nil {
		return nil
	}

	cloned := &RevisionSpec{
		Branch: r.Branch,
		Tag:    r.Tag,
		Commit: r.Commit,
	}

	return cloned
}
