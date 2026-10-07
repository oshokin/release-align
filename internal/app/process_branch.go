package app

import (
	"context"

	"github.com/oshokin/release-align/internal/logger"
)

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

	if !ok && r.cfg.Local == localReset {
		return true, nil
	}

	if !ok {
		message := "target branch " + branch + " has local commits or diverges from origin"
		skipped := &result{repo, statusSkipped, message}

		return false, skipped
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
