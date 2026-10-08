package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/oshokin/release-align/internal/gitter"
)

// WorkspaceInitOptions describes local inventory discovery, not repository synchronization.
type WorkspaceInitOptions struct {
	// BaseDir is the root walked for existing clones.
	BaseDir string
	// Branch is stored as the workspace default branch.
	Branch string
	// Release is an optional label stored in the new file.
	Release string
	// GitLabURL is saved when set. An empty value uses the base directory name when that name is a DNS host.
	GitLabURL string
	// GitLabGroups are saved when set. They are not inferred from directories.
	GitLabGroups []string
}

// workspaceScanner accumulates existing repository roots without entering their contents.
type workspaceScanner struct {
	// git runs local Git commands.
	git LocalGit
	// base is the directory being walked.
	base string
	// projects are the clones found so far.
	projects []*ProjectSpec
	// gaps are paths whose origin URL was not stored.
	gaps []string
}

const (
	// gitlabURLScheme is the API origin prefix stored for a directory-name host.
	gitlabURLScheme = "https://"
	// dnsHostMaxLen is the longest accepted DNS name, without a trailing dot.
	dnsHostMaxLen = 253
	// dnsLabelMaxLen is the longest accepted DNS label.
	dnsLabelMaxLen = 63
)

var (
	// errWorkspaceInitOptions means the base directory or branch was omitted.
	errWorkspaceInitOptions = errors.New("workspace init requires base-dir and branch")
	// errWorkspaceInitBaseRepo means the base directory itself is a repository.
	errWorkspaceInitBaseRepo = errors.New("base-dir must contain repositories; it must not itself be a repository")
	// errWorkspaceInitMarker means .git is a symbolic link.
	errWorkspaceInitMarker = errors.New(".git must be a directory or regular gitfile, not a symbolic link")
	// errWorkspaceInitEmpty means the walk found no worktrees.
	errWorkspaceInitEmpty = errors.New("no Git working trees found under base-dir")
	// errWorkspaceInitOrigin means a discovered repository has no usable origin.
	errWorkspaceInitOrigin = errors.New("discovered repository has no usable origin URL")
	// errWorkspaceListedInvalid means a listed path is not a usable repository.
	errWorkspaceListedInvalid = errors.New("listed workspace path is not a usable repository")
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

	base, err := canonicalWorkspaceBase(options.BaseDir)
	if err != nil {
		return nil, err
	}

	if _, err = g.Local(ctx, base, "check-ref-format", "refs/heads/"+options.Branch); err != nil {
		return nil, err
	}

	projects, gaps, err := discoverWorkspaceProjects(ctx, g, base)
	if err != nil {
		return nil, err
	}

	if len(projects) == 0 {
		return nil, errWorkspaceInitEmpty
	}

	spec := &WorkspaceSpec{
		SchemaVersion: 1,
		Release:       options.Release,
		BaseDir:       base,
		DefaultRevision: &RevisionSpec{
			Branch: options.Branch,
		},
		Projects: projects,
		URLGaps:  gaps,
	}

	gitlabURL, fromBase := gitlabURLForInit(options, base)
	if gitlabURL != "" || len(options.GitLabGroups) > 0 {
		source := &GitLabSource{
			URL:    gitlabURL,
			Groups: slices.Clone(options.GitLabGroups),
		}
		spec.GitLab = source
		spec.GitLabURLFromBase = fromBase
	}

	if err = spec.Validate(); err != nil {
		return nil, err
	}

	return spec, nil
}

// DiscoverWorkspaceProjects lists local working trees under baseDir.
// An empty directory is a successful empty result. The caller supplies any default branch.
func DiscoverWorkspaceProjects(ctx context.Context, g LocalGit, baseDir string) ([]*ProjectSpec, error) {
	base, err := canonicalWorkspaceBase(baseDir)
	if err != nil {
		return nil, err
	}

	projects, _, err := discoverWorkspaceProjects(ctx, g, base)

	return projects, err
}

// canonicalWorkspaceBase resolves a directory that contains clones and is not followed past its real path.
func canonicalWorkspaceBase(baseDir string) (string, error) {
	base, err := filepath.Abs(baseDir)
	if err != nil {
		return "", err
	}

	base, err = filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(base)
	if err != nil {
		return "", err
	}

	if !info.IsDir() {
		return "", errBaseDirNotDirectory
	}

	return base, nil
}

// gitlabURLForInit returns an explicit --gitlab-url, or https:// plus the base directory name.
// The second result is true only when the URL was taken from that directory name.
func gitlabURLForInit(options *WorkspaceInitOptions, base string) (string, bool) {
	if options.GitLabURL != "" {
		return options.GitLabURL, false
	}

	host, ok := directoryGitLabHost(base)
	if !ok {
		return "", false
	}

	return gitlabURLScheme + host, true
}

// directoryGitLabHost returns the lowercased last path component when it is a DNS host.
// Parent directories are ignored.
func directoryGitLabHost(base string) (string, bool) {
	name := strings.ToLower(filepath.Base(base))
	if !dnsHostname(name) {
		return "", false
	}

	return name, true
}

// dnsHostname reports a dotted name whose labels are letters, digits, and hyphens.
func dnsHostname(name string) bool {
	if name == "" || len(name) > dnsHostMaxLen || !strings.Contains(name, ".") {
		return false
	}

	for label := range strings.SplitSeq(name, ".") {
		if !dnsLabel(label) {
			return false
		}
	}

	return true
}

// dnsLabel reports one non-empty LDH label that does not start or end with a hyphen.
func dnsLabel(label string) bool {
	if label == "" || len(label) > dnsLabelMaxLen {
		return false
	}

	if label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}

	for _, char := range label {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
			return false
		}
	}

	return true
}

// discoverWorkspaceProjects walks one canonical base directory.
func discoverWorkspaceProjects(ctx context.Context, g LocalGit, base string) ([]*ProjectSpec, []string, error) {
	scanner := &workspaceScanner{
		git:      g,
		base:     base,
		projects: []*ProjectSpec{},
	}

	err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if ctx.Err() != nil {
			return context.Cause(ctx)
		}

		return scanner.visit(ctx, path, entry)
	})
	if err != nil {
		return nil, nil, err
	}

	slices.SortFunc(scanner.projects, func(left, right *ProjectSpec) int {
		return strings.Compare(left.Path, right.Path)
	})
	slices.Sort(scanner.gaps)

	return scanner.projects, scanner.gaps, nil
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
	if err := confirmWorktree(ctx, s.git, dir); err != nil {
		return err
	}

	relative, err := filepath.Rel(s.base, dir)
	if err != nil {
		return err
	}

	projectPath := filepath.ToSlash(relative)

	origin, originErr := readOriginURL(ctx, s.git, dir)
	if originErr != nil {
		if !errors.Is(originErr, errWorkspaceInitOrigin) && !errors.Is(originErr, errWorkspaceCredentialURL) {
			return originErr
		}

		origin = ""

		s.gaps = append(s.gaps, projectPath)
	}

	project := &ProjectSpec{
		Path:   projectPath,
		Groups: directoryGroups(projectPath),
		URL:    origin,
	}
	s.projects = append(s.projects, project)

	return nil
}

// directoryGroups returns full ancestor paths, avoiding ambiguous subgroup basenames.
func directoryGroups(projectPath string) []string {
	var groups []string

	for index, char := range projectPath {
		if char == '/' {
			groups = append(groups, projectPath[:index])
		}
	}

	return groups
}

// confirmWorktree checks the git marker and the real worktree root.
func confirmWorktree(ctx context.Context, g LocalGit, dir string) error {
	marker, err := os.Lstat(filepath.Join(dir, ".git"))
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s: %w", dir, errWorkspaceListedInvalid)
	}

	if err != nil {
		return err
	}

	if !marker.IsDir() && !marker.Mode().IsRegular() {
		return fmt.Errorf("%s: %w", dir, errWorkspaceInitMarker)
	}

	top, err := g.Local(ctx, dir, "rev-parse", "--show-toplevel")
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

	return nil
}

// readOriginURL returns a credential-free origin and preserves operational Git failures.
func readOriginURL(ctx context.Context, g LocalGit, dir string) (string, error) {
	remote, err := g.Local(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		if gitter.ExitCode(err) == 2 {
			return "", errWorkspaceInitOrigin
		}

		return "", err
	}

	if strings.TrimSpace(remote) == "" {
		return "", errWorkspaceInitOrigin
	}

	if !acceptableOrigin(remote) {
		return "", errWorkspaceCredentialURL
	}

	return strings.TrimSpace(remote), nil
}

// acceptableOrigin rejects a password or an HTTPS userinfo. ssh://git@host stays.
func acceptableOrigin(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\r\n\t ") {
		return false
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return !strings.Contains(raw, "://")
	}

	if parsed.User == nil {
		return true
	}

	if _, hasPassword := parsed.User.Password(); hasPassword {
		return false
	}

	return parsed.Scheme != "http" && parsed.Scheme != "https"
}

// listedProjectMissing reports a workspace path whose directory is absent.
// A present but unusable path is an error, including a root discovery would not visit.
func listedProjectMissing(ctx context.Context, g LocalGit, base, relative string) (bool, error) {
	dir, err := ResolveProjectDirectory(base, relative)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}

	if err != nil {
		return false, err
	}

	if err = confirmWorktree(ctx, g, dir); err != nil {
		return false, err
	}

	return false, nil
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
