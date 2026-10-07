package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

// WorkspaceRefreshOptions selects local clones to compare or append.
// Add and AddAll are mutually exclusive. An empty selection previews without writing.
type WorkspaceRefreshOptions struct {
	// BaseDir is the root that contains the clones.
	BaseDir string
	// Add lists workspace paths to append. It is mutually exclusive with AddAll.
	Add []string
	// AddAll appends every discovered clone that is not already listed.
	AddAll bool
	// publish replaces publication steps in tests.
	publish *workspacePublishHooks
}

// WorkspaceRefreshResult is a local inventory diff. It does not say whether revisions are ready.
type WorkspaceRefreshResult struct {
	// Listed is the number of projects already in the workspace file.
	Listed int
	// Unlisted are local clones that are not in the file.
	Unlisted []*ProjectSpec
	// Missing are listed paths that are not on disk.
	Missing []string
	// Added are the paths appended by this call.
	Added []string
	// Written reports that the file was replaced.
	Written bool
}

// localTimeoutGit is a Git client whose local command limit can be replaced.
type localTimeoutGit interface {
	// SetLocalTimeout sets the limit used for commands that do not pass their own timeout.
	SetLocalTimeout(timeout time.Duration)
}

var (
	// ErrWorkspaceRefreshUsage marks flags or a workspace document that refresh will not apply.
	ErrWorkspaceRefreshUsage = errors.New("invalid workspace refresh")
	// errWorkspaceRefreshFlags means the base directory or file was omitted.
	errWorkspaceRefreshFlags = errors.New("workspace refresh requires base-dir and file")
	// errWorkspaceRefreshExclusive means both --add and --add-all were set.
	errWorkspaceRefreshExclusive = errors.New("workspace refresh accepts either repeated --add or --add-all")
	// errWorkspaceRefreshUnknown means a requested path is not a new local clone.
	errWorkspaceRefreshUnknown = errors.New("local repository is not a new clone under base-dir")
)

// RefreshWorkspace compares a workspace file with local clones.
// Without Add or AddAll it only reports differences. Selected new clones are appended.
// Missing entries stay in the file. The call does not fetch, switch branches, or contact GitLab.
func RefreshWorkspace(
	ctx context.Context,
	g LocalGit,
	filename string,
	options *WorkspaceRefreshOptions,
) (*WorkspaceRefreshResult, error) {
	if err := validateRefreshOptions(filename, options); err != nil {
		return nil, err
	}

	if refreshWrites(options) {
		return writeWorkspaceRefresh(ctx, g, filename, options)
	}

	return previewWorkspaceRefresh(ctx, g, filename, options)
}

// validateRefreshOptions rejects a request that cannot be interpreted.
func validateRefreshOptions(filename string, options *WorkspaceRefreshOptions) error {
	if filename == "" || options == nil || options.BaseDir == "" {
		return refreshUsage(errWorkspaceRefreshFlags)
	}

	if options.AddAll && len(options.Add) > 0 {
		return refreshUsage(errWorkspaceRefreshExclusive)
	}

	return nil
}

// applyRefreshLocalTimeout copies timeouts.local onto a client that can store it.
// Refresh has no timeout flag, so the workspace value replaces the built-in default.
func applyRefreshLocalTimeout(g LocalGit, spec *WorkspaceSpec) error {
	client, ok := g.(localTimeoutGit)
	if !ok || spec == nil || spec.Timeouts == nil || spec.Timeouts.Local == nil {
		return nil
	}

	local, err := unlockedDuration(false, "local", spec.Timeouts.Local, 0)
	if err != nil {
		return err
	}

	client.SetLocalTimeout(local)

	return nil
}

// refreshWrites reports whether the caller asked to add repositories.
func refreshWrites(options *WorkspaceRefreshOptions) bool {
	return options.AddAll || len(options.Add) > 0
}

// previewWorkspaceRefresh reads the file without a lock and does not publish a replacement.
func previewWorkspaceRefresh(
	ctx context.Context,
	g LocalGit,
	filename string,
	options *WorkspaceRefreshOptions,
) (*WorkspaceRefreshResult, error) {
	document, err := loadRefreshDocument(filename)
	if err != nil {
		return nil, err
	}

	if err = applyRefreshLocalTimeout(g, document.spec); err != nil {
		return nil, err
	}

	if err = ResolveWorkspaceRevisions(ctx, g, options.BaseDir, document.spec); err != nil {
		return nil, err
	}

	result, _, err := refreshInventory(ctx, g, options.BaseDir, document.spec)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// writeWorkspaceRefresh holds the sibling lock from the reread through publication.
func writeWorkspaceRefresh(
	ctx context.Context,
	g LocalGit,
	filename string,
	options *WorkspaceRefreshOptions,
) (*WorkspaceRefreshResult, error) {
	path, err := refreshPath(filename)
	if err != nil {
		return nil, err
	}

	unlock, err := lockWorkspaceFile(path)
	if err != nil {
		return nil, err
	}

	defer unlock()

	document, err := loadRefreshDocument(path)
	if err != nil {
		return nil, err
	}

	if err = applyRefreshLocalTimeout(g, document.spec); err != nil {
		return nil, err
	}

	if err = ResolveWorkspaceRevisions(ctx, g, options.BaseDir, document.spec); err != nil {
		return nil, err
	}

	return publishRefresh(ctx, g, document, options)
}

// publishRefresh scans under the lock and writes only when at least one path is new.
func publishRefresh(
	ctx context.Context,
	g LocalGit,
	document *workspaceDocument,
	options *WorkspaceRefreshOptions,
) (*WorkspaceRefreshResult, error) {
	result, discovered, err := refreshInventory(ctx, g, options.BaseDir, document.spec)
	if err != nil {
		return nil, err
	}

	selected, err := selectRefreshProjects(document.spec, discovered, options)
	if err != nil {
		return nil, err
	}

	next, added, err := AppendDiscoveredProjects(document.spec, selected)
	if err != nil {
		return nil, refreshUsage(err)
	}

	if len(added) == 0 {
		return result, nil
	}

	if err = publishWorkspace(ctx, document, next, options.publish); err != nil {
		return nil, err
	}

	result.Listed = len(next.Projects)
	result.Unlisted = unlistedProjects(discovered, next)
	result.Added = added
	result.Written = true

	return result, nil
}

// loadRefreshDocument classifies a missing or invalid document as a usage error.
func loadRefreshDocument(filename string) (*workspaceDocument, error) {
	path, err := refreshPath(filename)
	if err != nil {
		return nil, err
	}

	document, err := readWorkspaceDocument(path)
	if err != nil {
		return nil, refreshDocumentError(err)
	}

	return document, nil
}

// refreshPath returns the absolute regular-file path, without following a symlink at the final name.
func refreshPath(filename string) (string, error) {
	path, err := canonicalWorkspaceFilename(filename)
	if errors.Is(err, os.ErrNotExist) {
		return "", refreshUsage(err)
	}

	return path, err
}

// refreshDocumentError keeps a permission failure operational and rejects the document otherwise.
func refreshDocumentError(err error) error {
	if errors.Is(err, os.ErrPermission) {
		return err
	}

	return refreshUsage(err)
}

// refreshInventory checks Git once, then discovers clones and missing listed paths.
func refreshInventory(
	ctx context.Context,
	g LocalGit,
	baseDir string,
	spec *WorkspaceSpec,
) (*WorkspaceRefreshResult, []*ProjectSpec, error) {
	base, err := canonicalWorkspaceBase(baseDir)
	if err != nil {
		return nil, nil, err
	}

	discovered, err := DiscoverWorkspaceProjects(ctx, g, base)
	if err != nil {
		return nil, nil, err
	}

	missing, err := missingListedProjects(ctx, g, base, spec)
	if err != nil {
		return nil, nil, err
	}

	result := &WorkspaceRefreshResult{
		Listed:   len(spec.Projects),
		Unlisted: unlistedProjects(discovered, spec),
		Missing:  missing,
		Added:    []string{},
	}

	return result, discovered, nil
}

// missingListedProjects checks each saved path directly, including roots the directory walk skips.
func missingListedProjects(ctx context.Context, g LocalGit, base string, spec *WorkspaceSpec) ([]string, error) {
	missing := make([]string, 0)

	for _, project := range spec.Projects {
		gone, err := listedProjectMissing(ctx, g, base, project.Path)
		if err != nil {
			return nil, err
		}

		if gone {
			missing = append(missing, project.Path)
		}
	}

	return missing, nil
}

// unlistedProjects returns discovered roots that the workspace does not already name.
func unlistedProjects(discovered []*ProjectSpec, spec *WorkspaceSpec) []*ProjectSpec {
	listed := make(map[string]bool, len(spec.Projects))

	for _, project := range spec.Projects {
		listed[project.Path] = true
	}

	unlisted := make([]*ProjectSpec, 0)

	for _, project := range discovered {
		if !listed[project.Path] {
			unlisted = append(unlisted, project)
		}
	}

	return unlisted
}

// selectRefreshProjects returns the clones the caller agreed to add.
func selectRefreshProjects(
	spec *WorkspaceSpec,
	discovered []*ProjectSpec,
	options *WorkspaceRefreshOptions,
) ([]*ProjectSpec, error) {
	if options.AddAll {
		return unlistedProjects(discovered, spec), nil
	}

	return namedRefreshProjects(spec, discovered, options.Add)
}

// namedRefreshProjects rejects the whole request when any new path is unknown or unsafe.
func namedRefreshProjects(spec *WorkspaceSpec, discovered []*ProjectSpec, add []string) ([]*ProjectSpec, error) {
	listed := make(map[string]bool, len(spec.Projects))

	for _, project := range spec.Projects {
		listed[project.Path] = true
	}

	known := make(map[string]*ProjectSpec, len(discovered))

	for _, project := range discovered {
		known[project.Path] = project
	}

	seen := make(map[string]bool, len(add))
	selected := make([]*ProjectSpec, 0)

	for _, path := range add {
		if seen[path] {
			continue
		}

		seen[path] = true

		if listed[path] {
			continue
		}

		project, err := refreshProject(path, known)
		if err != nil {
			return nil, err
		}

		selected = append(selected, project)
	}

	return selected, nil
}

// refreshProject returns one discovered clone. A repeat or an already listed path is handled by the caller.
func refreshProject(path string, known map[string]*ProjectSpec) (*ProjectSpec, error) {
	if !canonicalProjectPath(path) {
		return nil, refreshUsage(fmt.Errorf("%w: %s", errWorkspacePath, path))
	}

	project, ok := known[path]
	if !ok {
		return nil, refreshUsage(fmt.Errorf("%w: %s", errWorkspaceRefreshUnknown, path))
	}

	return project, nil
}

// refreshUsage marks an error that the command should report as usage.
func refreshUsage(err error) error {
	return fmt.Errorf("%w: %w", ErrWorkspaceRefreshUsage, err)
}
