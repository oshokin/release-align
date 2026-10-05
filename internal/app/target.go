package app

import (
	"context"
	"errors"
	"strings"

	"github.com/oshokin/release-align/internal/logger"
)

// target separates selection data from its human-readable explanation.
type target struct {
	// branch is the local branch to check out. Empty when detaching at a tag.
	branch string
	// tag is the tag to detach at. Empty when staying on a branch.
	tag string
	// remote is the origin ref to fast-forward.
	remote string
	// reason explains why this target was selected.
	reason string
}

// chooseTarget selects the release branch, a branch containing the version commit, a tag, or the current branch.
func (r *runner) chooseTarget(ctx context.Context, repo *repository) (*target, error) {
	exists, err := r.refExists(ctx, repo, "refs/remotes/origin/"+r.cfg.Branch)
	if err != nil {
		return nil, err
	}

	if exists {
		return &target{
			branch: r.cfg.Branch,
			remote: "refs/remotes/origin/" + r.cfg.Branch,
			reason: "preferred branch",
		}, nil
	}

	if v, ok := r.manifest.Lookup(repo.relative, repo.name); ok {
		picked, pickErr := r.targetFromRelease(ctx, repo, v)
		if pickErr != nil && !errors.Is(pickErr, errTargetNotFound) {
			return nil, pickErr
		}

		if picked != nil {
			return picked, nil
		}
	}

	return r.currentUpstream(ctx, repo)
}

// targetFromRelease prefers an origin branch that contains the version commit, then the version tag.
func (r *runner) targetFromRelease(ctx context.Context, repo *repository, v *Release) (*target, error) {
	// Resolve only validated hexadecimal object IDs, not arbitrary rev expressions.
	commit, err := r.git.Local(ctx, repo.path, "rev-parse", "--verify", v.Commit+"^{commit}")
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}

	if err == nil {
		picked, pickErr := r.originBranchContaining(ctx, repo, commit, v.Commit)
		if pickErr != nil && !errors.Is(pickErr, errTargetNotFound) {
			return nil, pickErr
		}

		if picked != nil {
			return picked, nil
		}
	}

	exists, err := r.refExists(ctx, repo, "refs/tags/"+v.Tag)
	if err != nil {
		return nil, err
	}

	if exists {
		return &target{tag: v.Tag, reason: "version commit has no origin branch"}, nil
	}

	logger.Warnf(ctx, "Version %s:%s was not found; falling back to current upstream", v.Tag, v.Commit)

	return nil, errTargetNotFound
}

// originBranchContaining returns the first origin branch that contains commit, ignoring origin/HEAD.
func (r *runner) originBranchContaining(ctx context.Context, repo *repository, commit, id string) (*target, error) {
	refs, err := r.git.Local(
		ctx,
		repo.path,
		"for-each-ref",
		"--sort=refname",
		"--contains="+commit,
		"--format=%(refname) %(symref)",
		"refs/remotes/origin/",
	)
	if err != nil {
		return nil, err
	}

	for line := range strings.SplitSeq(refs, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 1 || fields[0] == "refs/remotes/origin/HEAD" {
			continue
		}

		return &target{
			branch: strings.TrimPrefix(fields[0], "refs/remotes/origin/"),
			remote: fields[0],
			reason: "contains " + id,
		}, nil
	}

	return nil, errTargetNotFound
}

// currentUpstream keeps the branch that already tracks origin.
func (r *runner) currentUpstream(ctx context.Context, repo *repository) (*target, error) {
	upstream, err := r.git.Local(ctx, repo.path, "rev-parse", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		return nil, err
	}

	if !strings.HasPrefix(upstream, "refs/remotes/origin/") {
		return nil, errUpstreamNotOrigin
	}

	branch, err := r.git.Local(ctx, repo.path, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return nil, err
	}

	return &target{branch: branch, remote: upstream, reason: "current upstream"}, nil
}
