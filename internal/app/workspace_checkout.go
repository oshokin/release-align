package app

import (
	"context"
	"strings"
)

// branchSeen is the worktree path and branch ref read from one porcelain record.
type branchSeen struct {
	// worktree is the path of that worktree.
	worktree string
	// current is its branch ref.
	current string
}

// branchField is one porcelain worktree line.
type branchField struct {
	// key is the porcelain field name.
	key string
	// value is the porcelain field value.
	value string
}

// branchSwitch is a local branch checkout onto an already verified remote tip.
type branchSwitch struct {
	// repo is the local clone.
	repo *repository
	// branch is the short branch name.
	branch string
	// remote is the matching refs/remotes/origin ref.
	remote string
	// create is true when the local branch does not exist yet.
	create bool
}

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

	sw := &branchSwitch{
		repo:   item.repo,
		branch: item.resolved.Value,
		remote: remote,
		create: !exists,
	}
	if err = r.switchTo(ctx, sw); err != nil {
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
func (r *runner) switchTo(ctx context.Context, sw *branchSwitch) error {
	args := []string{"switch", gitNoOverwriteIgnore}
	if sw.create {
		args = append(args, "--create", sw.branch, "--track", sw.remote)
	} else {
		args = append(args, "--", sw.branch)
	}

	_, err := r.git.Local(ctx, sw.repo.path, args...)

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
	seen := new(branchSeen)

	flush := func() (bool, error) {
		if seen.current != branchRef || seen.worktree == "" {
			return false, nil
		}

		same, err := r.pathsSame(seen.worktree, repoPath)
		if err != nil {
			return false, err
		}

		return !same, nil
	}

	for record := range strings.SplitSeq(strings.Trim(out, "\x00"), "\x00") {
		if record != "" {
			key, value, _ := strings.Cut(record, " ")
			field := &branchField{
				key:   key,
				value: value,
			}
			r.recordBranch(seen, field)

			continue
		}

		busy, err := flush()
		if busy || err != nil {
			return busy, err
		}

		seen.worktree = ""
		seen.current = ""
	}

	return flush()
}

// recordBranch copies one porcelain field onto the record read so far.
func (*runner) recordBranch(seen *branchSeen, field *branchField) {
	if seen == nil || field == nil {
		return
	}

	switch field.key {
	case "worktree":
		seen.worktree = field.value
	case "branch":
		seen.current = field.value
	}
}
