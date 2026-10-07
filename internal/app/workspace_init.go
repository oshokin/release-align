package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// WorkspaceInitOptions describes local inventory discovery, not repository synchronization.
type WorkspaceInitOptions struct {
	BaseDir string
	Branch  string
	Release string
}

// workspaceScanner accumulates existing repository roots without entering their contents.
type workspaceScanner struct {
	git  LocalGit
	base string
	spec *WorkspaceSpec
}

var (
	errWorkspaceInitOptions  = errors.New("workspace init requires base-dir and branch")
	errWorkspaceInitBaseRepo = errors.New("base-dir must contain repositories; it must not itself be a repository")
	errWorkspaceInitMarker   = errors.New(".git must be a directory or regular gitfile, not a symbolic link")
	errWorkspaceInitEmpty    = errors.New("no Git working trees found under base-dir")
	errWorkspaceInitOrigin   = errors.New("discovered repository has no usable origin URL")
)

// ScanWorkspace inventories local clones and derives groups from parent directory prefixes.
// No fetch, branch existence check, checkout, or write is performed.
func ScanWorkspace(ctx context.Context, g LocalGit, options *WorkspaceInitOptions) (*WorkspaceSpec, error) {
	if options == nil || options.BaseDir == "" || options.Branch == "" {
		return nil, errWorkspaceInitOptions
	}
	revision := &RevisionSpec{
		Branch: options.Branch,
	}
	if err := revision.Validate(); err != nil {
		return nil, err
	}

	base, err := filepath.Abs(options.BaseDir)
	if err != nil {
		return nil, err
	}

	base, err = filepath.EvalSymlinks(base)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(base)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		return nil, errBaseDirNotDirectory
	}

	if _, err = g.Local(ctx, base, "check-ref-format", "refs/heads/"+options.Branch); err != nil {
		return nil, err
	}
	spec := &WorkspaceSpec{
		SchemaVersion: 1,
		Release:       options.Release,
		DefaultBranch: options.Branch,
		Projects:      []*ProjectSpec{},
	}
	scanner := &workspaceScanner{
		git:  g,
		base: base,
		spec: spec,
	}

	err = filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if ctx.Err() != nil {
			return context.Cause(ctx)
		}

		return scanner.visit(ctx, path, entry)
	})
	if err != nil {
		return nil, err
	}

	if len(spec.Projects) == 0 {
		return nil, errWorkspaceInitEmpty
	}

	slices.SortFunc(spec.Projects, func(a, b *ProjectSpec) int { return strings.Compare(a.Path, b.Path) })

	if err = spec.Validate(); err != nil {
		return nil, err
	}

	return spec, nil
}

// visit prunes excluded directories and validates every discovered Git working tree.
func (s *workspaceScanner) visit(ctx context.Context, dir string, entry fs.DirEntry) error {
	if !entry.IsDir() {
		return nil
	}

	if dir != s.base && strings.HasPrefix(entry.Name(), ".") {
		return filepath.SkipDir
	}
	marker, err := os.Lstat(filepath.Join(dir, ".git"))
	if errors.Is(err, os.ErrNotExist) {
		return s.skipBare(ctx, dir)
	}

	if err != nil {
		return err
	}

	if dir == s.base {
		return errWorkspaceInitBaseRepo
	}

	if !marker.IsDir() && !marker.Mode().IsRegular() {
		return fmt.Errorf("%s: %w", dir, errWorkspaceInitMarker)
	}

	if err = s.addProject(ctx, dir); err != nil {
		return err
	}

	return filepath.SkipDir
}

// addProject checks the real repository root and origin before recording an exact relative path.
func (s *workspaceScanner) addProject(ctx context.Context, dir string) error {
	top, err := s.git.Local(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}

	want, err := os.Stat(dir)
	if err != nil {
		return err
	}

	got, err := os.Stat(top)
	if err != nil {
		return err
	}

	if !os.SameFile(want, got) {
		return fmt.Errorf("%s: %w", dir, errWorkspaceRoot)
	}

	remote, err := s.git.Local(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		return fmt.Errorf("%s: %w: %w", dir, errWorkspaceInitOrigin, err)
	}

	if strings.TrimSpace(remote) == "" {
		return fmt.Errorf("%s: %w", dir, errWorkspaceInitOrigin)
	}

	relative, err := filepath.Rel(s.base, dir)
	if err != nil {
		return err
	}
	projectPath := filepath.ToSlash(relative)
	project := &ProjectSpec{
		Path:   projectPath,
		Groups: s.directoryGroups(projectPath),
	}
	s.spec.Projects = append(s.spec.Projects, project)

	return nil
}

// directoryGroups returns full ancestor paths, avoiding ambiguous subgroup basenames.
func (*workspaceScanner) directoryGroups(projectPath string) []string {
	var groups []string

	for index, char := range projectPath {
		if char == '/' {
			groups = append(groups, projectPath[:index])
		}
	}

	return groups
}

// skipBare avoids walking an object database when a bare repository is found.
func (s *workspaceScanner) skipBare(ctx context.Context, dir string) error {
	head, err := os.Lstat(filepath.Join(dir, "HEAD"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return err
	}

	if !head.Mode().IsRegular() {
		return nil
	}
	objects, err := os.Lstat(filepath.Join(dir, "objects"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return err
	}

	if !objects.IsDir() {
		return nil
	}

	bare, err := s.git.Local(ctx, dir, "rev-parse", "--is-bare-repository")
	if err != nil {
		return err
	}

	if bare != "true" {
		return nil
	}

	if dir == s.base {
		return errWorkspaceInitBaseRepo
	}

	return filepath.SkipDir
}
