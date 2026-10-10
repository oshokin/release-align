package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/oshokin/release-align/internal/gitlab"
	"github.com/oshokin/release-align/internal/logger"
)

// WorkspaceRefreshOptions selects local clones to compare or reconcile.
// Add and Sync are mutually exclusive. An empty selection previews without writing.
type WorkspaceRefreshOptions struct {
	// BaseDir is the root that contains the clones.
	BaseDir string
	// Add lists workspace paths to append. It is mutually exclusive with Sync.
	Add []string
	// Sync appends discovered clones and drops listed paths that are gone.
	Sync bool
	// SyncPaths limits Sync to those paths. An empty list applies the whole diff.
	SyncPaths []string
	// Remote compares the file with the non-archived GitLab catalog.
	Remote bool
	// Delete removes checkouts that this same --remote --sync run dropped from the file.
	Delete bool
	// Apply stores GitLab archive marks for projects already listed. It requires Remote.
	Apply bool
	// catalog replaces the GitLab listing. Nil means call the API.
	catalog *refreshCatalog
	// remote replaces the token and HTTP client used for that listing.
	remote *remoteHooks
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
	// Removed are listed paths dropped because their directories are gone.
	Removed []string
	// Remote reports that the diff came from the GitLab catalog.
	Remote bool
	// RemoteAbsent are listed paths the non-archived catalog did not return.
	RemoteAbsent []string
	// Deleted are checkouts removed after this run dropped them from the file.
	Deleted []string
	// BecameArchived are listed projects GitLab now reports as archived.
	BecameArchived []string
	// BecameActive are listed projects GitLab now reports as active.
	BecameActive []string
	// Written reports that the file was replaced.
	Written bool
}

// refreshCatalog is a finished non-archived listing supplied by a test.
type refreshCatalog struct {
	// projects are the projects the listing returned. An empty slice is a successful empty catalog.
	projects []*gitlab.Project
}

// refreshedInventory is the local diff and the clones found under the base directory.
type refreshedInventory struct {
	// result is the counts and path lists.
	result *WorkspaceRefreshResult
	// discovered are the working trees found on disk.
	discovered []*ProjectSpec
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
	// errWorkspaceRefreshExclusive means both --add and --sync were set.
	errWorkspaceRefreshExclusive = errors.New("workspace refresh accepts either repeated --add or --sync")
	// errWorkspaceRefreshUnknown means a requested path is not a new local clone.
	errWorkspaceRefreshUnknown = errors.New("local repository is not a new clone under base-dir")
	// errWorkspaceRefreshSyncPath means --sync named a path that is neither listed nor a local clone.
	errWorkspaceRefreshSyncPath = errors.New("path is not a listed repository or a new local clone")
	// errWorkspaceRefreshEmpty means --sync would drop every listed repository.
	errWorkspaceRefreshEmpty = errors.New("refusing to drop every listed repository; base-dir has no clones")
	// errWorkspaceRefreshRemoteDelete means --delete was set without --remote and --sync.
	errWorkspaceRefreshRemoteDelete = errors.New("workspace refresh --delete requires --remote and --sync")
	// errWorkspaceRefreshRemoteAdd means --remote was combined with --add.
	errWorkspaceRefreshRemoteAdd = errors.New("workspace refresh --remote cannot be combined with --add")
	// errWorkspaceRefreshRemotePaths means --remote was given disk --sync path arguments.
	errWorkspaceRefreshRemotePaths = errors.New("workspace refresh --remote does not take paths")
	// errWorkspaceRefreshApply means --apply was used without a catalog-only --remote run.
	errWorkspaceRefreshApply = errors.New("workspace refresh --apply requires --remote and cannot change membership")
	// errWorkspaceRefreshEmptyRemote means a successful empty catalog would drop every listed project.
	errWorkspaceRefreshEmptyRemote = errors.New(
		"refusing to drop listed repositories; GitLab returned no projects",
	)
	// errWorkspaceRefreshDeleteNested means --delete would remove a directory that still holds a listed project.
	errWorkspaceRefreshDeleteNested = errors.New("refusing to delete a directory that contains a listed project")
)

// RefreshWorkspace compares a workspace file with local clones.
// Without Add or Sync it only reports differences.
// Sync appends new clones and drops listed paths whose directories are gone.
// Add appends selected clones and leaves missing entries in the file.
// The disk comparison does not fetch or switch branches.
// Remote compares the file with the non-archived GitLab catalog and does not scan for new clones.
func RefreshWorkspace(
	ctx context.Context,
	g LocalGit,
	filename string,
	options *WorkspaceRefreshOptions,
) (*WorkspaceRefreshResult, error) {
	if err := validateRefreshOptions(filename, options); err != nil {
		return nil, err
	}

	if options.Remote {
		return refreshFromRemote(ctx, filename, options)
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

	if options.Sync && len(options.Add) > 0 {
		return refreshUsage(errWorkspaceRefreshExclusive)
	}

	if options.Delete && (!options.Remote || !options.Sync) {
		return refreshUsage(errWorkspaceRefreshRemoteDelete)
	}

	if options.Remote && len(options.Add) > 0 {
		return refreshUsage(errWorkspaceRefreshRemoteAdd)
	}

	if options.Remote && len(options.SyncPaths) > 0 {
		return refreshUsage(errWorkspaceRefreshRemotePaths)
	}

	if options.Apply && (!options.Remote || options.Sync || len(options.Add) > 0 || options.Delete) {
		return refreshUsage(errWorkspaceRefreshApply)
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

// refreshWrites reports whether the caller asked to change the file.
func refreshWrites(options *WorkspaceRefreshOptions) bool {
	return options.Sync || len(options.Add) > 0
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

	refreshed, err := refreshInventory(ctx, g, options.BaseDir, document.spec)
	if err != nil {
		return nil, err
	}

	return refreshed.result, nil
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

	return publishRefresh(ctx, g, document, options)
}

// publishRefresh scans under the lock and writes when the selected diff is non-empty.
func publishRefresh(
	ctx context.Context,
	g LocalGit,
	document *workspaceDocument,
	options *WorkspaceRefreshOptions,
) (*WorkspaceRefreshResult, error) {
	refreshed, err := refreshInventory(ctx, g, options.BaseDir, document.spec)
	if err != nil {
		return nil, err
	}

	if err = refuseEmptySync(options, document.spec, refreshed); err != nil {
		return nil, err
	}

	return publishRefreshDiff(ctx, document, options, refreshed)
}

// publishRefreshDiff appends selected clones and, for Sync, drops confirmed-absent paths.
func publishRefreshDiff(
	ctx context.Context,
	document *workspaceDocument,
	options *WorkspaceRefreshOptions,
	refreshed *refreshedInventory,
) (*WorkspaceRefreshResult, error) {
	result := refreshed.result
	discovered := refreshed.discovered

	selected, err := selectRefreshProjects(document.spec, discovered, options)
	if err != nil {
		return nil, err
	}

	appended, err := AppendDiscoveredProjects(document.spec, selected)
	if err != nil {
		return nil, refreshUsage(err)
	}

	next := appended.spec
	removed := dropSyncedProjects(options, next, result.Missing)

	if len(appended.added) == 0 && len(removed) == 0 {
		return result, nil
	}

	if err = next.Validate(); err != nil {
		return nil, err
	}

	if err = publishWorkspace(ctx, document, next, options.publish); err != nil {
		return nil, err
	}

	result.Listed = len(next.Projects)
	result.Unlisted = unlistedProjects(discovered, next)
	result.Added = appended.added
	result.Removed = removed
	result.Written = true

	if options.Sync {
		result.Missing = pathsExcept(result.Missing, removed)
	}

	return result, nil
}

// refuseEmptySync stops a sync that would delete every listed project from an empty tree.
func refuseEmptySync(options *WorkspaceRefreshOptions, spec *WorkspaceSpec, refreshed *refreshedInventory) error {
	if options == nil || !options.Sync || len(options.SyncPaths) > 0 || spec == nil ||
		refreshed == nil || refreshed.result == nil {
		return nil
	}

	if len(refreshed.discovered) > 0 || len(spec.Projects) == 0 {
		return nil
	}

	if len(refreshed.result.Missing) != len(spec.Projects) {
		return nil
	}

	return refreshUsage(errWorkspaceRefreshEmpty)
}

// dropSyncedProjects removes missing paths from a cloned inventory when Sync is set.
func dropSyncedProjects(options *WorkspaceRefreshOptions, spec *WorkspaceSpec, missing []string) []string {
	if options == nil || !options.Sync || spec == nil || len(missing) == 0 {
		return nil
	}

	gone := make(map[string]struct{}, len(missing))
	for _, path := range missing {
		gone[path] = struct{}{}
	}

	chosen := syncPathSet(options.SyncPaths)
	kept := make([]*ProjectSpec, 0, len(spec.Projects))
	removed := make([]string, 0)

	for _, project := range spec.Projects {
		if project == nil {
			continue
		}

		_, missingPath := gone[project.Path]
		if missingPath && syncPathChosen(chosen, project.Path) {
			removed = append(removed, project.Path)

			continue
		}

		kept = append(kept, project)
	}

	spec.Projects = kept

	return removed
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
) (*refreshedInventory, error) {
	base, err := canonicalWorkspaceBase(baseDir)
	if err != nil {
		return nil, err
	}

	discovered, err := DiscoverWorkspaceProjects(ctx, g, base)
	if err != nil {
		return nil, err
	}

	missing, err := missingListedProjects(ctx, g, base, spec)
	if err != nil {
		return nil, err
	}

	result := &WorkspaceRefreshResult{
		Listed:   len(spec.Projects),
		Unlisted: unlistedProjects(discovered, spec),
		Missing:  missing,
		Added:    []string{},
	}
	refreshed := &refreshedInventory{
		result:     result,
		discovered: discovered,
	}

	return refreshed, nil
}

// missingListedProjects checks each saved path directly, including roots the directory walk skips.
func missingListedProjects(ctx context.Context, g LocalGit, base string, spec *WorkspaceSpec) ([]string, error) {
	missing := make([]string, 0)
	phase := logger.NewProgress("refresh", len(spec.Projects))

	for _, project := range spec.Projects {
		gone, err := listedProjectMissing(ctx, g, base, project.Path)
		if err != nil {
			return nil, err
		}

		message := "present"

		if gone {
			missing = append(missing, project.Path)
			message = "missing"
		}

		phase.Advance(ctx, project.Path, message)
	}

	return missing, nil
}

// unlistedProjects returns discovered roots that the workspace does not already name.
func unlistedProjects(discovered []*ProjectSpec, spec *WorkspaceSpec) []*ProjectSpec {
	listed := make(map[string]struct{}, len(spec.Projects))

	for _, project := range spec.Projects {
		listed[project.Path] = struct{}{}
	}

	unlisted := make([]*ProjectSpec, 0)

	for _, project := range discovered {
		if _, found := listed[project.Path]; !found {
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
	if options.Sync && len(options.SyncPaths) == 0 {
		return unlistedProjects(discovered, spec), nil
	}

	if options.Sync {
		return namedSyncProjects(spec, discovered, options.SyncPaths)
	}

	return namedRefreshProjects(spec, discovered, options.Add)
}

// namedRefreshProjects rejects the whole request when any new path is unknown or unsafe.
func namedRefreshProjects(spec *WorkspaceSpec, discovered []*ProjectSpec, add []string) ([]*ProjectSpec, error) {
	listed := make(map[string]struct{}, len(spec.Projects))

	for _, project := range spec.Projects {
		listed[project.Path] = struct{}{}
	}

	known := make(map[string]*ProjectSpec, len(discovered))

	for _, project := range discovered {
		known[project.Path] = project
	}

	seen := make(map[string]struct{}, len(add))
	selected := make([]*ProjectSpec, 0)

	for _, path := range add {
		if _, found := seen[path]; found {
			continue
		}

		seen[path] = struct{}{}

		if _, found := listed[path]; found {
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

// namedSyncProjects adds the named new clones and accepts listed paths for a later drop.
func namedSyncProjects(spec *WorkspaceSpec, discovered []*ProjectSpec, paths []string) ([]*ProjectSpec, error) {
	listed := make(map[string]struct{}, len(spec.Projects))

	for _, project := range spec.Projects {
		listed[project.Path] = struct{}{}
	}

	known := make(map[string]*ProjectSpec, len(discovered))

	for _, project := range discovered {
		known[project.Path] = project
	}

	seen := make(map[string]struct{}, len(paths))
	selected := make([]*ProjectSpec, 0)

	for _, path := range paths {
		if _, found := seen[path]; found {
			continue
		}

		seen[path] = struct{}{}

		if _, found := listed[path]; found {
			continue
		}

		if !canonicalProjectPath(path) {
			return nil, refreshUsage(fmt.Errorf("%w: %s", errWorkspacePath, path))
		}

		project, ok := known[path]
		if !ok {
			return nil, refreshUsage(fmt.Errorf("%w: %s", errWorkspaceRefreshSyncPath, path))
		}

		selected = append(selected, project)
	}

	return selected, nil
}

// syncPathSet is nil when every missing path is eligible.
func syncPathSet(paths []string) map[string]struct{} {
	if len(paths) == 0 {
		return nil
	}

	chosen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		chosen[path] = struct{}{}
	}

	return chosen
}

// syncPathChosen reports that a missing path is inside the requested sync set.
func syncPathChosen(chosen map[string]struct{}, path string) bool {
	if chosen == nil {
		return true
	}

	_, ok := chosen[path]

	return ok
}

// pathsExcept returns paths that were not dropped.
func pathsExcept(paths, dropped []string) []string {
	if len(dropped) == 0 {
		return paths
	}

	gone := make(map[string]struct{}, len(dropped))
	for _, path := range dropped {
		gone[path] = struct{}{}
	}

	kept := make([]string, 0, len(paths))

	for _, path := range paths {
		if _, found := gone[path]; !found {
			kept = append(kept, path)
		}
	}

	return kept
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
