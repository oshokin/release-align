package app

import (
	"context"
	"strings"
)

// checkoutPlanned moves HEAD to the planned object id.
func (r *runner) checkoutPlanned(ctx context.Context, item *workspaceItem) error {
	if item.resolved.Kind != revisionBranch {
		return r.checkoutOID(ctx, item.repo, item.resolved.OID)
	}

	remote := "refs/remotes/origin/" + item.resolved.Value
	if err := r.remoteStill(ctx, item.repo, remote, item.resolved.OID); err != nil {
		return err
	}

	local := "refs/heads/" + item.resolved.Value

	exists, err := r.refExists(ctx, item.repo, local)
	if err != nil {
		return err
	}

	if err = r.switchTo(ctx, item.repo, item.resolved.Value, remote, !exists); err != nil {
		return err
	}

	_, err = r.git.Local(
		ctx,
		item.dir,
		"merge",
		"--ff-only",
		gitNoOverwriteIgnore,
		"--no-edit",
		"--quiet",
		"--end-of-options",
		item.resolved.OID,
	)

	return err
}

// remoteStill checks that the remote ref still points at the planned object.
func (r *runner) remoteStill(ctx context.Context, repo *repository, ref, oid string) error {
	got, err := r.git.Local(ctx, repo.path, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return err
	}

	if got != oid {
		return errWorkspaceTargetChanged
	}

	return nil
}

// checkoutOID detaches HEAD at an exact object id.
func (r *runner) checkoutOID(ctx context.Context, repo *repository, oid string) error {
	_, err := r.git.Local(
		ctx,
		repo.path,
		"-c",
		"advice.detachedHead=false",
		"checkout",
		"--detach",
		gitNoOverwriteIgnore,
		"--quiet",
		"--end-of-options",
		oid,
	)

	return err
}

// switchTo checks out branch, creating it from the origin ref when it is missing locally.
func (r *runner) switchTo(ctx context.Context, repo *repository, branch, remote string, create bool) error {
	args := []string{"switch", gitNoOverwriteIgnore}
	if create {
		args = append(args, "--create", branch, "--track", remote)
	} else {
		args = append(args, "--", branch)
	}

	_, err := r.git.Local(ctx, repo.path, args...)

	return err
}

// branchBusy reports whether another worktree has the target branch checked out.
func (r *runner) branchBusy(ctx context.Context, repo *repository, branch string) (bool, error) {
	out, err := r.git.Local(ctx, repo.path, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return false, err
	}

	return r.worktreeUsesBranch(out, repo.path, "refs/heads/"+branch)
}

// worktreeUsesBranch reports a different worktree that already uses the branch.
func (r *runner) worktreeUsesBranch(out, repoPath, branchRef string) (bool, error) {
	var worktree, current string

	flush := func() (bool, error) {
		if current != branchRef || worktree == "" {
			return false, nil
		}

		same, err := r.pathsSame(worktree, repoPath)
		if err != nil {
			return false, err
		}

		return !same, nil
	}

	for record := range strings.SplitSeq(strings.Trim(out, "\x00"), "\x00") {
		if record != "" {
			key, value, _ := strings.Cut(record, " ")
			r.recordBranch(key, value, &worktree, &current)

			continue
		}

		busy, err := flush()
		if busy || err != nil {
			return busy, err
		}

		worktree, current = "", ""
	}

	return flush()
}

// recordBranch stores one porcelain worktree field.
func (*runner) recordBranch(key, value string, worktree, current *string) {
	switch key {
	case "worktree":
		*worktree = value
	case "branch":
		*current = value
	}
}
