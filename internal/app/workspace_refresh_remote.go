package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/oshokin/release-align/internal/gitlab"
)

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
	result := &WorkspaceRefreshResult{
		Listed:       len(spec.Projects),
		Remote:       true,
		RemoteAbsent: absent,
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

	written, err := writeRemoteDrops(ctx, filename, options, absent)
	if err != nil || written == nil || !options.Delete || !written.Written {
		return written, err
	}

	written.Deleted, err = deleteDroppedCheckouts(options.BaseDir, written.Removed)

	return written, err
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
