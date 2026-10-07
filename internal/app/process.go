package app

import (
	"context"

	"github.com/oshokin/release-align/internal/gitter"
	"github.com/oshokin/release-align/internal/logger"
)

// process fetches one repository and fast-forwards it, or detaches it at the selected tag.
func (r *runner) process(ctx context.Context, repo *repository, recoverNetwork func(*repository) error) *result {
	ctx = logger.WithKV(ctx, "repo", repo.relative)
	fail := func(err error) *result {
		if ctx.Err() != nil {
			return &result{repo, statusCanceled, messageRunStopped}
		}

		return &result{repo, statusFailed, err.Error()}
	}

	fetched := (*result)(nil)
	if !r.cfg.DryRun {
		fetched = r.fetch(ctx, repo, fail, recoverNetwork)
	}

	if fetched != nil {
		return fetched
	}

	reason, err := r.safeCurrent(ctx, repo)
	if err != nil {
		return fail(err)
	}

	if reason != "" {
		return &result{repo, statusSkipped, reason}
	}

	target, err := r.chooseTarget(ctx, repo)
	if err != nil {
		return fail(err)
	}

	branch, tag, remote, why := target.branch, target.tag, target.remote, target.reason
	if tag == "" {
		return r.updateBranch(ctx, repo, branch, remote, why, fail)
	}

	if r.cfg.DryRun {
		return r.detach(ctx, repo, tag, why, fail)
	}

	sha, stopped := r.prepareLocal(ctx, repo, fail)
	if stopped != nil {
		return stopped
	}

	return r.finish(ctx, repo, sha, r.detach(ctx, repo, tag, why, fail), fail)
}

// updateBranch fast-forwards the selected branch, or resets it onto origin when that flag is set.
func (r *runner) updateBranch(
	ctx context.Context,
	repo *repository,
	branch, remote, why string,
	fail func(error) *result,
) *result {
	local := "refs/heads/" + branch

	exists, err := r.refExists(ctx, repo, local)
	if err != nil {
		return fail(err)
	}

	reset := false

	var skipped *result

	if exists {
		reset, skipped = r.branchReset(ctx, repo, branch, local, remote, fail)
	}

	if skipped != nil {
		return skipped
	}

	if r.cfg.DryRun {
		return r.planBranch(ctx, repo, branch, local, remote, why, reset, fail)
	}

	sha, stopped := r.prepareLocal(ctx, repo, fail)
	if stopped != nil {
		return stopped
	}

	var res *result

	if reset {
		res = r.resetBranch(ctx, repo, branch, local, remote, why, fail)
	} else {
		res = r.fastForward(ctx, repo, branch, remote, why, exists, fail)
	}

	return r.finish(ctx, repo, sha, res, fail)
}

// fastForward checks out the selected branch and fast-forwards it from origin.
func (r *runner) fastForward(
	ctx context.Context,
	repo *repository,
	branch, remote, why string,
	exists bool,
	fail func(error) *result,
) *result {
	err := r.switchTo(ctx, repo, branch, remote, !exists)
	if err != nil {
		return fail(err)
	}

	if exists && r.cfg.Local == localReset {
		err = r.trackOrigin(ctx, repo, branch, remote)
	}

	if err != nil {
		return fail(err)
	}

	// No second network request, no implicit other remote, no rebase or merge commit.
	_, err = r.git.Local(ctx, repo.path, "merge", "--ff-only", gitNoOverwriteIgnore, "--no-edit", "--quiet", remote)
	if err != nil {
		return fail(err)
	}

	return &result{repo, statusUpdated, branch + " synchronized with origin (" + why + ")"}
}

// fetch updates origin once, and retries a single time after the remote is confirmed reachable.
func (r *runner) fetch(
	ctx context.Context,
	repo *repository,
	fail func(error) *result,
	recoverNetwork func(*repository) error,
) *result {
	logger.Debug(ctx, "Fetching origin branches and tags")

	err := r.git.Fetch(ctx, repo.path, r.cfg.FetchTimeout)
	if err == nil {
		return nil
	}

	if !gitter.NetworkError(err) {
		return fail(err)
	}

	checkErr := recoverNetwork(repo)
	if checkErr != nil {
		return fail(checkErr)
	}

	// One retry after successful reachability confirmation, never an infinite loop.
	err = r.git.Fetch(ctx, repo.path, r.cfg.FetchTimeout)
	if err == nil {
		return nil
	}

	if !gitter.NetworkError(err) {
		return fail(err)
	}

	checkErr = recoverNetwork(repo)
	if checkErr != nil {
		return fail(checkErr)
	}

	return fail(err)
}
