package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/oshokin/release-align/internal/gitter"
	"github.com/oshokin/release-align/internal/logger"
)

const (
	// stashMessage marks the entry created by this run so it can be told apart from the user's stash.
	stashMessage = "release-align"
)

// beginChanges stashes a dirty worktree. A clean tree returns an empty commit id.
func (r *runner) beginChanges(ctx context.Context, repo *repository, fail func(error) *result) (string, *result) {
	sha, err := r.hideLocalChanges(ctx, repo)
	if err != nil {
		return "", fail(err)
	}

	return sha, nil
}

// finish applies a stash onto a successful update, or puts the worktree back after a failure.
func (r *runner) finish(
	ctx context.Context,
	repo *repository,
	sha string,
	res *result,
	fail func(error) *result,
) *result {
	if sha == "" {
		return res
	}

	if res.status != statusUpdated {
		return r.rollbackChanges(ctx, repo, sha, res)
	}

	if err := r.overlayLocalChanges(ctx, repo, sha); err != nil {
		return fail(err)
	}

	res.message += "; local changes kept as unstaged edits"

	return res
}

// rollbackChanges restores the index and worktree from the stash after a failed update.
func (r *runner) rollbackChanges(ctx context.Context, repo *repository, sha string, res *result) *result {
	if err := r.restoreLocalChanges(ctx, repo, sha); err != nil {
		return &result{repo, statusFailed, res.message + "; " + err.Error()}
	}

	return res
}

// hideLocalChanges stores staged, unstaged, and untracked files. Ignored files are not included.
func (r *runner) hideLocalChanges(ctx context.Context, repo *repository) (string, error) {
	status, err := r.porcelain(ctx, repo)
	if err != nil || status == "" {
		return "", err
	}

	before, err := r.stashTip(ctx, repo)
	if err != nil {
		return "", err
	}

	logger.Debug(ctx, "Stashing local changes before the update")

	_, err = r.git.Local(
		ctx,
		repo.path,
		"stash",
		"push",
		"--include-untracked",
		"--message",
		stashMessage,
		"--quiet",
	)
	if err != nil {
		return "", err
	}

	after, err := r.stashTip(ctx, repo)
	if err != nil {
		return "", err
	}

	if after == "" || after == before {
		return "", errNothingStashed
	}

	return r.requireCleanTree(ctx, repo, after)
}

// requireCleanTree rolls the stash back when checkout would still see local paths.
func (r *runner) requireCleanTree(ctx context.Context, repo *repository, sha string) (string, error) {
	left, err := r.porcelain(ctx, repo)
	if err != nil {
		return "", r.withRestore(ctx, repo, sha, err)
	}

	if left == "" {
		return sha, nil
	}

	cause := fmt.Errorf("%w: %s", errStillDirty, r.dirtyTreeReason(left, r.dirtyPathLimit(ctx)))

	return "", r.withRestore(ctx, repo, sha, cause)
}

// withRestore appends a restore failure to cause and otherwise returns cause.
func (r *runner) withRestore(ctx context.Context, repo *repository, sha string, cause error) error {
	restoreErr := r.restoreLocalChanges(ctx, repo, sha)
	if restoreErr != nil {
		return fmt.Errorf("%w; %w", cause, restoreErr)
	}

	return cause
}

// overlayLocalChanges applies the stash as unstaged edits and drops it only after a clean apply.
func (r *runner) overlayLocalChanges(ctx context.Context, repo *repository, sha string) error {
	logger.Debug(ctx, "Applying local changes as unstaged edits")

	if err := r.applyStash(ctx, repo, sha, false); err != nil {
		return err
	}

	if _, err := r.git.Local(ctx, repo.path, "reset", "--quiet"); err != nil {
		return fmt.Errorf("%w; applied changes could not be unstaged: %w", errStashKept, err)
	}

	if err := r.dropStash(ctx, repo, sha); err != nil {
		return fmt.Errorf("%w; changes are already in the worktree: %w", errStashKept, err)
	}

	return nil
}

// restoreLocalChanges puts back the staged index and the worktree, then drops the stash.
func (r *runner) restoreLocalChanges(ctx context.Context, repo *repository, sha string) error {
	ctx = context.WithoutCancel(ctx)

	if err := r.applyStash(ctx, repo, sha, true); err != nil {
		return err
	}

	return r.dropStash(ctx, repo, sha)
}

// applyStash replays one stash commit. restoreIndex puts back the staged state from that commit.
func (r *runner) applyStash(ctx context.Context, repo *repository, sha string, restoreIndex bool) error {
	args := []string{"stash", "apply"}
	if restoreIndex {
		args = append(args, "--index")
	}

	args = append(args, "--quiet", sha)

	_, err := r.git.Local(ctx, repo.path, args...)
	if err != nil {
		return fmt.Errorf("%w: %w", errStashKept, err)
	}

	return nil
}

// dropStash removes the entry with this commit id and leaves every other stash entry in place.
func (r *runner) dropStash(ctx context.Context, repo *repository, sha string) error {
	list, err := r.git.Local(ctx, repo.path, "stash", "list", "--format=%H")
	if err != nil {
		return err
	}

	if list == "" {
		return nil
	}

	for i, line := range strings.Split(list, "\n") {
		if line != sha {
			continue
		}

		_, err = r.git.Local(ctx, repo.path, "stash", "drop", "--quiet", fmt.Sprintf("stash@{%d}", i))

		return err
	}

	return nil
}

// stashTip reads refs/stash. An empty string means the repository has no stash yet.
func (r *runner) stashTip(ctx context.Context, repo *repository) (string, error) {
	sha, err := r.git.Local(ctx, repo.path, "rev-parse", "--verify", "--quiet", "refs/stash")
	if err != nil {
		if gitter.ExitCode(err) == 1 {
			return "", nil
		}

		return "", err
	}

	return sha, nil
}
