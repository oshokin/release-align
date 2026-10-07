package app

import (
	"context"
	"strconv"
	"strings"
)

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
