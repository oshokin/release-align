package app

import (
	"context"

	"github.com/oshokin/release-align/internal/gitter"
)

// refExists reports whether a local ref is present. Git exit code 1 means it is absent.
func (r *runner) refExists(ctx context.Context, repo *repository, ref string) (bool, error) {
	_, err := r.git.Local(ctx, repo.path, "show-ref", "--verify", "--quiet", ref)
	if err == nil {
		return true, nil
	}

	if gitter.ExitCode(err) == 1 {
		return false, nil
	}

	return false, err
}

// ancestor reports whether commit a is an ancestor of commit b.
func (r *runner) ancestor(ctx context.Context, repo *repository, a, b string) (bool, error) {
	_, err := r.git.Local(ctx, repo.path, "merge-base", "--is-ancestor", a, b)
	if err == nil {
		return true, nil
	}

	if gitter.ExitCode(err) == 1 {
		return false, nil
	}

	return false, err
}
