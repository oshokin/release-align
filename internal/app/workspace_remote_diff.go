package app

import (
	"os"
	"slices"

	"github.com/oshokin/release-align/internal/gitlab"
)

// remoteDiffQuery is the input for one catalog classification.
type remoteDiffQuery struct {
	// spec is the workspace inventory. It may be nil when only case folding is probed.
	spec *WorkspaceSpec
	// roots are local clones keyed by workspace path.
	roots map[string]*localRoot
	// base is the directory that contains the clones.
	base string
	// allowLocal matches filesystem clone URLs.
	allowLocal bool
	// count is the expected number of API projects, used to size the seen set.
	count int
}

// remoteDiff classifies one complete catalog against disk and the workspace file.
type remoteDiff struct {
	// spec is the workspace inventory.
	spec *WorkspaceSpec
	// base is the directory that contains the clones.
	base string
	// roots are local clones keyed by workspace path.
	roots map[string]*localRoot
	// byKey maps a remote identity to local relative paths.
	byKey map[string][]string
	// byOrigin maps a cleaned filesystem path to local relative paths.
	byOrigin map[string][]string
	// allowLocal matches filesystem clone URLs.
	allowLocal bool
	// seen records API paths so leftovers can be computed.
	seen map[string]bool
	// blocked records paths that must not be cloned.
	blocked map[string]bool
	// catalog is the diff being filled.
	catalog *RemoteCatalog
}

// newRemoteDiff indexes local origins before any project is classified.
func newRemoteDiff(query *remoteDiffQuery) *remoteDiff {
	if query == nil {
		query = &remoteDiffQuery{}
	}

	diff := &remoteDiff{
		spec:       query.spec,
		base:       query.base,
		roots:      query.roots,
		allowLocal: query.allowLocal,
		seen:       make(map[string]bool, query.count),
		blocked:    make(map[string]bool),
		catalog:    emptyCatalog(),
		byKey:      make(map[string][]string),
		byOrigin:   make(map[string][]string),
	}

	for _, root := range query.roots {
		diff.addOrigin(root)
	}

	return diff
}

// classifyAll records every visible project.
func (d *remoteDiff) classifyAll(projects []*gitlab.Project) {
	for _, project := range projects {
		d.classify(project)
	}
}

// leftovers records listed projects the complete catalog did not return.
func (d *remoteDiff) leftovers() {
	if d.spec == nil {
		return
	}

	for _, project := range d.spec.Projects {
		if project == nil || d.seen[project.Path] {
			continue
		}

		if d.spec.GitLab != nil && gitlabScope(project.Path, d.spec.GitLab.Groups) {
			d.catalog.NotReturned = append(d.catalog.NotReturned, project.Path)

			continue
		}

		d.catalog.OutsideScope = append(d.catalog.OutsideScope, project.Path)
	}
}

// finish sorts every name list. Empty lists stay empty arrays.
func (d *remoteDiff) finish() {
	d.catalog.NotCloned = sortNames(d.catalog.NotCloned)
	d.catalog.LocalUnlisted = sortNames(d.catalog.LocalUnlisted)
	d.catalog.Conflicts = sortNames(d.catalog.Conflicts)
	d.catalog.NotReturned = sortNames(d.catalog.NotReturned)
	d.catalog.DifferentPath = sortNames(d.catalog.DifferentPath)
	d.catalog.OutsideScope = sortNames(d.catalog.OutsideScope)
	d.catalog.SkippedEmpty = sortNames(d.catalog.SkippedEmpty)
}

// classify puts one API project into a single primary bucket, plus an empty-branch note.
func (d *remoteDiff) classify(project *gitlab.Project) {
	if project == nil {
		return
	}

	path := project.PathWithNamespace
	d.seen[path] = true

	if !canonicalProjectPath(path) || d.blocked[path] {
		d.catalog.Conflicts = append(d.catalog.Conflicts, path)

		return
	}

	if project.DefaultBranch == "" {
		d.catalog.SkippedEmpty = append(d.catalog.SkippedEmpty, path)
	}

	matches := d.matches(project)
	exists := d.targetExists(path)
	d.place(path, matches, exists, project.DefaultBranch == "")
}

// place applies the disk match. An empty default branch is not offered as a normal clone.
func (d *remoteDiff) place(path string, matches []string, exists, empty bool) {
	if len(matches) > 1 || (len(matches) == 1 && matches[0] != path) {
		d.catalog.DifferentPath = append(d.catalog.DifferentPath, path)
		if exists {
			d.catalog.Conflicts = append(d.catalog.Conflicts, path)
		}

		return
	}

	if len(matches) == 1 {
		if !d.listed(path) {
			d.catalog.LocalUnlisted = append(d.catalog.LocalUnlisted, path)
		}

		return
	}

	if exists {
		d.catalog.Conflicts = append(d.catalog.Conflicts, path)

		return
	}

	if !empty {
		d.catalog.NotCloned = append(d.catalog.NotCloned, path)
	}
}

// matches returns local relative paths whose origin is this project.
func (d *remoteDiff) matches(project *gitlab.Project) []string {
	seen := make(map[string]bool)
	found := make([]string, 0)
	urls := []string{project.SSHURLToRepo, project.HTTPURLToRepo}

	for _, raw := range urls {
		if key, ok := parseRemote(raw); ok {
			d.collect(d.byKey[remoteID(key)], seen, &found)
		}

		if d.allowLocal {
			d.collect(d.byOrigin[cleanLocal(raw)], seen, &found)
		}
	}

	slices.Sort(found)

	return found
}

// collect appends unseen paths.
func (d *remoteDiff) collect(paths []string, seen map[string]bool, found *[]string) {
	for _, path := range paths {
		if seen[path] {
			continue
		}

		seen[path] = true
		*found = append(*found, path)
	}
}

// addOrigin indexes one local clone by transport identity and, in tests, by path.
func (d *remoteDiff) addOrigin(root *localRoot) {
	if root == nil {
		return
	}

	if key, ok := parseRemote(root.origin); ok {
		id := remoteID(key)
		d.byKey[id] = append(d.byKey[id], root.path)
	}

	if cleaned := cleanLocal(root.origin); cleaned != "" {
		d.byOrigin[cleaned] = append(d.byOrigin[cleaned], root.path)
	}
}

// listed reports a workspace row with this exact path.
func (d *remoteDiff) listed(path string) bool {
	if d.spec == nil {
		return false
	}

	for _, project := range d.spec.Projects {
		if project != nil && project.Path == path {
			return true
		}
	}

	return false
}

// targetExists reports a directory or file already at the expected layout path.
func (d *remoteDiff) targetExists(path string) bool {
	if d.base == "" || !canonicalProjectPath(path) {
		return d.roots[path] != nil
	}

	_, err := ResolveProjectDirectory(d.base, path)
	if err == nil {
		return true
	}

	if os.IsNotExist(err) {
		return false
	}

	return true
}

// caseFolding probes the base directory. A missing base is treated as case-sensitive.
// An unreadable base is an error so clone does not assume the volume is case-sensitive.
func (d *remoteDiff) caseFolding() (bool, error) {
	if d.base == "" {
		return false, nil
	}

	probe, err := os.CreateTemp(d.base, ".release-align-case-*-a")
	if err != nil {
		return false, err
	}

	name := probe.Name()
	_ = probe.Close()

	defer func() {
		_ = os.Remove(name)
	}()

	_, err = os.Lstat(foldProbe(name))

	return err == nil, nil
}

// foldProbe returns the same path with one letter's case flipped.
func foldProbe(path string) string {
	buf := []byte(path)

	for i := len(buf) - 1; i >= 0; i-- {
		switch {
		case buf[i] >= 'a' && buf[i] <= 'z':
			buf[i] -= 'a' - 'A'

			return string(buf)
		case buf[i] >= 'A' && buf[i] <= 'Z':
			buf[i] += 'a' - 'A'

			return string(buf)
		}
	}

	return path
}

// emptyCatalog returns the arrays required in a checked document.
func emptyCatalog() *RemoteCatalog {
	return &RemoteCatalog{
		NotCloned:     []string{},
		LocalUnlisted: []string{},
		Conflicts:     []string{},
		NotReturned:   []string{},
		DifferentPath: []string{},
		OutsideScope:  []string{},
		SkippedEmpty:  []string{},
	}
}

// sortNames returns a sorted copy and replaces nil with an empty list.
func sortNames(values []string) []string {
	if values == nil {
		return []string{}
	}

	slices.Sort(values)

	return values
}
