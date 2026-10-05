package app

import (
	"context"
	"strconv"
	"strings"

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

	if !r.cfg.DryRun {
		if res := r.fetch(ctx, repo, fail, recoverNetwork); res != nil {
			return res
		}
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
	if tag != "" {
		if r.cfg.DryRun {
			return r.detach(ctx, repo, tag, why, fail)
		}

		sha, stopped := r.prepareLocal(ctx, repo, fail)
		if stopped != nil {
			return stopped
		}

		return r.finish(ctx, repo, sha, r.detach(ctx, repo, tag, why, fail), fail)
	}

	return r.updateBranch(ctx, repo, branch, remote, why, fail)
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

	if exists {
		var skipped *result

		reset, skipped = r.branchReset(ctx, repo, branch, local, remote, fail)
		if skipped != nil {
			return skipped
		}
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
	var err error

	if exists {
		err = r.switchTo(ctx, repo, branch, remote, false)
	} else {
		err = r.switchTo(ctx, repo, branch, remote, true)
	}

	if err != nil {
		return fail(err)
	}

	if exists && r.cfg.Local == localReset {
		if err = r.trackOrigin(ctx, repo, branch, remote); err != nil {
			return fail(err)
		}
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
	if !gitter.NetworkError(err) {
		if err != nil {
			return fail(err)
		}

		return nil
	}

	checkErr := recoverNetwork(repo)
	if checkErr != nil {
		return fail(checkErr)
	}

	// One retry after successful reachability confirmation, never an infinite loop.
	err = r.git.Fetch(ctx, repo.path, r.cfg.FetchTimeout)
	if !gitter.NetworkError(err) {
		if err != nil {
			return fail(err)
		}

		return nil
	}

	checkErr = recoverNetwork(repo)
	if checkErr != nil {
		return fail(checkErr)
	}

	return fail(err)
}

// detach checks out the version tag, or only reports that plan during a dry run.
func (r *runner) detach(ctx context.Context, repo *repository, tag, why string, fail func(error) *result) *result {
	if r.cfg.DryRun {
		return &result{repo, statusPlanned, "detach at tag " + tag + " (" + why + ")"}
	}

	args := []string{"-c", "advice.detachedHead=false", "checkout", "--detach", gitNoOverwriteIgnore}
	if r.cfg.Local == localReset {
		args = append(args, "--force")
	}

	args = append(args, "--quiet", "refs/tags/"+tag)

	_, err := r.git.Local(ctx, repo.path, args...)
	if err != nil {
		return fail(err)
	}

	return &result{repo, statusUpdated, "detached at tag " + tag}
}

// branchReset reports whether the local branch must be moved onto origin.
// A non-nil result means the repository is skipped or the check failed.
func (r *runner) branchReset(
	ctx context.Context,
	repo *repository,
	branch, local, remote string,
	fail func(error) *result,
) (bool, *result) {
	ok, err := r.ancestor(ctx, repo, local, remote)
	if err != nil {
		return false, fail(err)
	}

	if !ok {
		if r.cfg.Local != localReset {
			message := "target branch " + branch + " has local commits or diverges from origin"
			skipped := &result{repo, statusSkipped, message}

			return false, skipped
		}

		return true, nil
	}

	upstream, err := r.git.Local(ctx, repo.path, "for-each-ref", "--format=%(upstream)", local)
	if err != nil {
		return false, fail(err)
	}

	if upstream != remote && r.cfg.Local != localReset {
		skipped := &result{repo, statusSkipped, "target branch " + branch + " has no matching origin upstream"}

		return false, skipped
	}

	return false, nil
}

// planBranch describes the branch update a dry run would perform.
func (r *runner) planBranch(
	ctx context.Context,
	repo *repository,
	branch, local, remote, why string,
	reset bool,
	fail func(error) *result,
) *result {
	prefix, err := r.discardedEditsPrefix(ctx, repo)
	if err != nil {
		return fail(err)
	}

	if !reset {
		message := prefix + "switch to " + branch + " and fast-forward from origin (" + why + ")"
		planned := &result{repo, statusPlanned, message}

		return planned
	}

	n, err := r.localOnlyCount(ctx, repo, local, remote)
	if err != nil {
		return fail(err)
	}

	message := prefix + "reset " + branch + " to origin, discarding " + r.localCommitPhrase(n) + " (" + why + ")"
	planned := &result{repo, statusPlanned, message}

	return planned
}

// resetBranch checks out the local branch and moves it to the fetched origin ref.
func (r *runner) resetBranch(
	ctx context.Context,
	repo *repository,
	branch, local, remote, why string,
	fail func(error) *result,
) *result {
	n, err := r.localOnlyCount(ctx, repo, local, remote)
	if err != nil {
		return fail(err)
	}

	logger.Debug(ctx, "Resetting "+branch+" to origin")

	if err = r.switchTo(ctx, repo, branch, remote, false); err != nil {
		return fail(err)
	}

	_, err = r.git.Local(ctx, repo.path, "reset", "--hard", remote)
	if err != nil {
		return fail(err)
	}

	if err = r.trackOrigin(ctx, repo, branch, remote); err != nil {
		return fail(err)
	}

	updated := &result{
		repo,
		statusUpdated,
		branch + " reset to origin, discarded " + r.localCommitPhrase(n) + " (" + why + ")",
	}

	return updated
}

// prepareLocal stashes edits for keep, or deletes untracked files for reset.
func (r *runner) prepareLocal(ctx context.Context, repo *repository, fail func(error) *result) (string, *result) {
	switch r.cfg.Local {
	case localKeep:
		return r.beginChanges(ctx, repo, fail)
	case localReset:
		if err := r.discardEdits(ctx, repo); err != nil {
			return "", fail(err)
		}
	}

	return "", nil
}

// discardEdits removes untracked files and directories. Ignored files stay.
func (r *runner) discardEdits(ctx context.Context, repo *repository) error {
	_, err := r.git.Local(ctx, repo.path, "clean", "-fd", "--quiet")

	return err
}

// switchTo checks out branch. reset also throws away tracked edits.
func (r *runner) switchTo(ctx context.Context, repo *repository, branch, remote string, create bool) error {
	args := []string{"switch", gitNoOverwriteIgnore}
	if r.cfg.Local == localReset {
		args = append(args, "--discard-changes")
	}

	if create {
		args = append(args, "--create", branch, "--track", remote)
	} else {
		args = append(args, "--", branch)
	}

	_, err := r.git.Local(ctx, repo.path, args...)

	return err
}

// discardedEditsPrefix reports a dry-run clause when reset would remove uncommitted files.
func (r *runner) discardedEditsPrefix(ctx context.Context, repo *repository) (string, error) {
	if r.cfg.Local != localReset {
		return "", nil
	}

	status, err := r.porcelain(ctx, repo)
	if err != nil || status == "" {
		return "", err
	}

	return "discard local edits and ", nil
}

// trackOrigin sets the local branch upstream to the selected origin ref.
func (r *runner) trackOrigin(ctx context.Context, repo *repository, branch, remote string) error {
	_, err := r.git.Local(ctx, repo.path, "branch", "--set-upstream-to="+remote, branch)

	return err
}

// localOnlyCount counts commits reachable from the local branch and not from origin.
func (r *runner) localOnlyCount(ctx context.Context, repo *repository, local, remote string) (int, error) {
	out, err := r.git.Local(ctx, repo.path, "rev-list", "--count", local, "^"+remote)
	if err != nil {
		return 0, err
	}

	return strconv.Atoi(strings.TrimSpace(out))
}

// localCommitPhrase formats a discarded-commit count for a plan or a result line.
func (r *runner) localCommitPhrase(n int) string {
	if n == 1 {
		return "1 local commit"
	}

	return strconv.Itoa(n) + " local commits"
}
