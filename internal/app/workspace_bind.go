package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/oshokin/release-align/internal/gitter"
)

// bindWorkspaceDirs resolves every selected path before any fetch starts.
func (r *runner) bindWorkspaceDirs(ctx context.Context, items []*workspaceItem) error {
	seen := make([]string, 0, len(items))

	for _, item := range items {
		if ctx.Err() != nil {
			markPending(items, outcomeCanceled, reasonCanceled, messageRunStopped)

			return context.Cause(ctx)
		}

		next, err := r.bindWorkspaceDir(ctx, item, seen)
		if err != nil {
			return err
		}

		seen = next
	}

	return nil
}

// bindWorkspaceDir resolves one path and attaches its Git repository.
func (r *runner) bindWorkspaceDir(ctx context.Context, item *workspaceItem, seen []string) ([]string, error) {
	if item.spec != nil && item.spec.Archived {
		blockRow(item.row, outcomeSkipped, reasonArchived, "archived, excluded from release alignment")

		return seen, nil
	}

	dir, err := ResolveProjectDirectory(r.cfg.BaseDir, item.spec.Path)
	if errors.Is(err, os.ErrNotExist) {
		blockRow(item.row, outcomeBlocked, reasonMissingRepository, "repository directory is absent")

		return seen, nil
	}

	if errors.Is(err, errWorkspacePathKind) || errors.Is(err, errWorkspacePath) {
		return nil, err
	}

	if err != nil {
		blockRow(item.row, outcomeBlocked, reasonGitFailed, redactGitText(err.Error()))

		return seen, nil
	}

	seen, err = r.rememberDir(dir, seen)
	if err != nil {
		return nil, err
	}

	item.dir = dir

	return seen, r.bindGitRepo(ctx, item)
}

// rememberDir rejects a second path that names the same directory.
func (r *runner) rememberDir(dir string, seen []string) ([]string, error) {
	for _, previous := range seen {
		same, err := r.pathsSame(dir, previous)
		if err != nil {
			return nil, err
		}

		if same {
			return nil, errWorkspaceDuplicate
		}
	}

	return append(seen, dir), nil
}

// bindGitRepo checks that the path is a repository root with origin.
func (r *runner) bindGitRepo(ctx context.Context, item *workspaceItem) error {
	top, err := r.git.Local(ctx, item.dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return r.noteRepoProbe(ctx, item, err)
	}

	same, err := r.pathsSame(item.dir, top)
	if err != nil {
		blockRow(item.row, outcomeBlocked, reasonGitFailed, redactGitText(err.Error()))

		return nil
	}

	if !same {
		blockRow(item.row, outcomeBlocked, reasonNotRepositoryRoot, "path is not the repository root")

		return nil
	}

	remote, err := r.git.Local(ctx, item.dir, "remote", "get-url", "origin")
	if err != nil {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}

		// Exit 2 is the documented missing-remote result. Any other failure is local Git, not a dead GitLab.
		if gitter.ExitCode(err) != 2 {
			blockRow(item.row, outcomeBlocked, reasonGitFailed, redactGitText(err.Error()))

			return nil
		}

		return r.noteMissingOrigin(ctx, item)
	}

	expected := ""
	if item.spec != nil {
		expected = item.spec.URL
	}

	if mismatch := remoteIdentity(expected, remote); mismatch != "" {
		blockRow(item.row, outcomeBlocked, reasonRemoteMismatch, mismatch)

		return nil
	}

	item.repo = &repository{
		path:     item.dir,
		name:     filepath.Base(item.dir),
		relative: item.spec.Path,
		endpoint: r.endpoint(remote),
	}

	common, err := r.gitCommonDir(ctx, item.repo)
	if err != nil {
		return r.noteCommonDir(ctx, item, err)
	}

	item.repo.common = common

	return nil
}

// noteMissingOrigin blocks a repository that has no origin remote.
func (r *runner) noteMissingOrigin(ctx context.Context, item *workspaceItem) error {
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}

	blockRow(item.row, outcomeBlocked, reasonMissingOrigin, "origin is missing")

	return nil
}

// noteCommonDir blocks a repository whose common Git directory cannot be read.
func (r *runner) noteCommonDir(ctx context.Context, item *workspaceItem, err error) error {
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}

	blockRow(item.row, outcomeBlocked, reasonGitFailed, redactGitText(err.Error()))
	item.repo = nil

	return nil
}

// noteRepoProbe classifies a failed repository probe.
func (r *runner) noteRepoProbe(ctx context.Context, item *workspaceItem, err error) error {
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}

	if strings.Contains(strings.ToLower(err.Error()), "not a git repository") {
		blockRow(item.row, outcomeBlocked, reasonNotRepositoryRoot, "path is not a Git work tree")

		return nil
	}

	code, message := r.workspaceGitReason(err)
	blockRow(item.row, outcomeBlocked, code, message)

	return nil
}

// gitCommonDir returns the absolute shared Git directory.
func (r *runner) gitCommonDir(ctx context.Context, repo *repository) (string, error) {
	common, err := r.git.Local(ctx, repo.path, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}

	if !filepath.IsAbs(common) {
		common = filepath.Join(repo.path, common)
	}

	resolved, err := filepath.EvalSymlinks(common)
	if err != nil {
		return "", err
	}

	return resolved, nil
}
