package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/oshokin/release-align/internal/gitlab"
)

// gitMetaDir is the worktree metadata entry. It is not itself a nested checkout.
const gitMetaDir = ".git"

// refreshFromRemote prints or drops listed paths the non-archived catalog did not return.
// Directories stay unless Delete is set on the same --sync run that dropped them.
func refreshFromRemote(
	ctx context.Context,
	filename string,
	options *WorkspaceRefreshOptions,
) (*WorkspaceRefreshResult, error) {
	spec, err := LoadWorkspace(filename)
	if err != nil {
		return nil, err
	}

	if err = requireRefreshGitLab(spec); err != nil {
		return nil, err
	}

	projects, err := refreshCatalogProjects(ctx, spec, options)
	if err != nil {
		return nil, err
	}

	absent := absentFromCatalog(spec, projects)
	becameArchived, becameActive := archiveStateChanges(spec, projects)
	result := &WorkspaceRefreshResult{
		Listed:         len(spec.Projects),
		Remote:         true,
		RemoteAbsent:   absent,
		BecameArchived: becameArchived,
		BecameActive:   becameActive,
	}

	if options.Apply {
		applied, applyErr := applyArchiveMarks(ctx, filename, options, projects)
		if applied != nil {
			applied.BecameArchived = result.BecameArchived
			applied.BecameActive = result.BecameActive
		}

		return applied, applyErr
	}

	if !options.Sync {
		return result, nil
	}

	if len(projects) == 0 && len(spec.Projects) > 0 {
		return nil, refreshUsage(errWorkspaceRefreshEmptyRemote)
	}

	if len(absent) == 0 {
		return result, nil
	}

	if options.Delete {
		if nestErr := deleteWouldRemoveKept(spec, absent); nestErr != nil {
			return nil, nestErr
		}

		if nestErr := deleteWouldRemoveNested(options.BaseDir, absent); nestErr != nil {
			return nil, nestErr
		}
	}

	written, err := writeRemoteDrops(ctx, filename, options, absent)
	if written != nil {
		written.BecameArchived = result.BecameArchived
		written.BecameActive = result.BecameActive
		written.RemoteAbsent = result.RemoteAbsent
	}

	if err != nil || written == nil || !options.Delete || !written.Written {
		return written, err
	}

	written.Deleted, err = deleteDroppedCheckouts(options.BaseDir, written.Removed)

	return written, err
}

// applyArchiveMarks stores the catalog archive flag on projects already in the file.
// A project missing from the catalog is left unchanged.
func applyArchiveMarks(
	ctx context.Context,
	filename string,
	options *WorkspaceRefreshOptions,
	projects []*gitlab.Project,
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

	byPath := make(map[string]bool, len(projects))
	for _, project := range projects {
		if project != nil && project.PathWithNamespace != "" {
			byPath[project.PathWithNamespace] = project.Archived
		}
	}

	changed, err := writeArchiveMarks(document.root, byPath)
	if err != nil {
		return nil, err
	}

	result := &WorkspaceRefreshResult{
		Listed: len(document.spec.Projects),
		Remote: true,
	}

	if !changed {
		return result, nil
	}

	if err = publishWorkspace(ctx, document, document.spec, options.publish); err != nil {
		return nil, err
	}

	result.Written = true

	return result, nil
}

// requireRefreshGitLab rejects a file that cannot be compared with the catalog.
func requireRefreshGitLab(spec *WorkspaceSpec) error {
	if spec == nil || spec.GitLab == nil {
		return refreshUsage(errGitLabSource)
	}

	if err := spec.GitLab.ValidateForAPI(); err != nil {
		return refreshUsage(err)
	}

	return nil
}

// refreshCatalogProjects uses a supplied listing or reads the non-archived catalog.
func refreshCatalogProjects(
	ctx context.Context,
	spec *WorkspaceSpec,
	options *WorkspaceRefreshOptions,
) ([]*gitlab.Project, error) {
	if options != nil && options.catalog != nil {
		return options.catalog.projects, nil
	}

	cfg := DefaultConfig()
	cfg.Remote = true
	cfg.remoteHooks = options.remote

	if err := ApplySpecTimeouts(cfg, spec); err != nil {
		return nil, err
	}

	if gitlabToken(cfg) == "" {
		return nil, refreshUsage(errRemoteToken)
	}

	projects, err := listRemoteProjects(ctx, cfg, spec.GitLab)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errRemoteInventory, err)
	}

	return projects, nil
}

// absentFromCatalog lists in-scope paths the catalog did not return.
// A path outside the saved GitLab groups stays in the file.
func absentFromCatalog(spec *WorkspaceSpec, projects []*gitlab.Project) []string {
	if spec == nil {
		return nil
	}

	seen := make(map[string]struct{}, len(projects))
	for _, project := range projects {
		if project == nil || project.PathWithNamespace == "" {
			continue
		}

		seen[project.PathWithNamespace] = struct{}{}
	}

	var groups []string

	if spec.GitLab != nil {
		groups = spec.GitLab.Groups
	}

	var absent []string

	for _, project := range spec.Projects {
		if project == nil {
			continue
		}

		_, found := seen[project.Path]
		if found || !listedInGitLabGroups(project.Path, groups) {
			continue
		}

		absent = append(absent, project.Path)
	}

	return absent
}

// listedInGitLabGroups reports whether path is one saved group or a project under it.
func listedInGitLabGroups(path string, groups []string) bool {
	for _, group := range groups {
		if path == group || strings.HasPrefix(path, group+"/") {
			return true
		}
	}

	return false
}

// writeRemoteDrops removes the absent paths from the workspace file and leaves the directories.
func writeRemoteDrops(
	ctx context.Context,
	filename string,
	options *WorkspaceRefreshOptions,
	absent []string,
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

	removed := dropListedProjects(document.spec, absent)
	result := &WorkspaceRefreshResult{
		Listed:  len(document.spec.Projects) + len(removed),
		Remote:  true,
		Removed: removed,
	}

	if len(removed) == 0 {
		return result, nil
	}

	if err = publishWorkspace(ctx, document, document.spec, options.publish); err != nil {
		return nil, err
	}

	result.Written = true

	return result, nil
}

// dropListedProjects removes the named paths and keeps every other project in order.
func dropListedProjects(spec *WorkspaceSpec, absent []string) []string {
	if spec == nil || len(absent) == 0 {
		return nil
	}

	gone := make(map[string]struct{}, len(absent))
	for _, path := range absent {
		gone[path] = struct{}{}
	}

	kept := make([]*ProjectSpec, 0, len(spec.Projects))
	removed := make([]string, 0)

	for _, project := range spec.Projects {
		if project == nil {
			continue
		}

		_, found := gone[project.Path]
		if found {
			removed = append(removed, project.Path)

			continue
		}

		kept = append(kept, project)
	}

	spec.Projects = kept

	return removed
}

// archiveStateChanges lists saved marks that differ from the catalog. A missing catalog row is not a change.
func archiveStateChanges(spec *WorkspaceSpec, projects []*gitlab.Project) ([]string, []string) {
	byPath := make(map[string]bool, len(projects))
	for _, project := range projects {
		if project != nil && project.PathWithNamespace != "" {
			byPath[project.PathWithNamespace] = project.Archived
		}
	}

	var archived []string

	var active []string

	if spec == nil {
		return archived, active
	}

	for _, project := range spec.Projects {
		if project == nil {
			continue
		}

		server, found := byPath[project.Path]
		if !found || server == project.Archived {
			continue
		}

		if server {
			archived = append(archived, project.Path)

			continue
		}

		active = append(active, project.Path)
	}

	return archived, active
}

// deleteWouldRemoveKept refuses a drop whose directory contains a project this run keeps.
func deleteWouldRemoveKept(spec *WorkspaceSpec, absent []string) error {
	if spec == nil {
		return nil
	}

	gone := make(map[string]struct{}, len(absent))
	for _, path := range absent {
		gone[path] = struct{}{}
	}

	kept := make([]string, 0, len(spec.Projects))
	for _, project := range spec.Projects {
		if project == nil {
			continue
		}

		if _, found := gone[project.Path]; found {
			continue
		}

		kept = append(kept, project.Path)
	}

	for _, path := range absent {
		prefix := path + "/"
		for _, stay := range kept {
			if strings.HasPrefix(stay, prefix) {
				return refreshUsage(fmt.Errorf("%w: %s contains %s", errWorkspaceRefreshDeleteNested, path, stay))
			}
		}
	}

	return nil
}

// deleteWouldRemoveNested refuses a drop that contains a Git checkout this run is not deleting.
// The candidate's own .git is not a nested checkout. Symlinks are not followed.
func deleteWouldRemoveNested(base string, absent []string) error {
	allowed := make(map[string]struct{}, len(absent))
	for _, path := range absent {
		allowed[path] = struct{}{}
	}

	for _, path := range absent {
		dir, err := ResolveProjectDirectory(base, path)
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, errWorkspacePathKind) {
			continue
		}

		if err != nil {
			return err
		}

		if err = nestedGitCheckout(dir, path, allowed); err != nil {
			return err
		}
	}

	return nil
}

// nestedGitCheckout reports a worktree strictly inside root that is not an allowed deletion.
func nestedGitCheckout(root, path string, allowed map[string]struct{}) error {
	walkErr := filepath.WalkDir(root, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}

		if !entry.IsDir() || current == root {
			return nil
		}

		if entry.Name() == gitMetaDir {
			return filepath.SkipDir
		}

		marked, markErr := gitWorktree(current)
		if markErr != nil {
			return markErr
		}

		if !marked {
			return nil
		}

		rel, relErr := filepath.Rel(root, current)
		if relErr != nil {
			return relErr
		}

		nested := path + "/" + filepath.ToSlash(rel)
		if _, ok := allowed[nested]; ok {
			return nil
		}

		return refreshUsage(fmt.Errorf("%w: %s contains %s", errWorkspaceRefreshDeleteNested, path, nested))
	})
	if walkErr == nil || errors.Is(walkErr, ErrWorkspaceRefreshUsage) {
		return walkErr
	}

	return fmt.Errorf("%w: %w", errWorkspaceRefreshDeleteNested, walkErr)
}

// gitWorktree reports a directory whose .git is a real directory or a worktree file.
func gitWorktree(dir string) (bool, error) {
	info, err := os.Lstat(filepath.Join(dir, gitMetaDir))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return false, nil
	}

	return info.IsDir() || info.Mode().IsRegular(), nil
}

// deleteDroppedCheckouts removes only the directories this run just dropped from the file.
func deleteDroppedCheckouts(base string, paths []string) ([]string, error) {
	deleted := make([]string, 0, len(paths))

	for _, path := range paths {
		dir, err := ResolveProjectDirectory(base, path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}

		if err != nil {
			return deleted, err
		}

		if err = os.RemoveAll(dir); err != nil {
			return deleted, err
		}

		deleted = append(deleted, path)
	}

	return deleted, nil
}
