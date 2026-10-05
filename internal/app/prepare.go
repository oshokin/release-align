package app

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/oshokin/release-align/internal/gitter"
)

// prepare keeps repositories that are safe to update and have an origin remote.
func (r *runner) prepare(ctx context.Context, paths []string, summary *Summary) ([]*repository, error) {
	var ready []*repository

	for _, path := range paths {
		if ctx.Err() != nil {
			summary.Canceled += len(paths) - summary.Skipped - summary.Failed
			return nil, context.Cause(ctx)
		}

		rel, err := filepath.Rel(r.cfg.BaseDir, path)
		if err != nil {
			return nil, err
		}

		repo := &repository{path: path, name: filepath.Base(path), relative: filepath.ToSlash(rel)}

		reason, err := r.safeCurrent(ctx, repo)
		if err != nil {
			res := &result{repo, statusFailed, err.Error()}
			r.record(ctx, summary, res)

			continue
		}

		if reason != "" {
			res := &result{repo, statusSkipped, reason}
			r.record(ctx, summary, res)

			continue
		}

		remote, err := r.git.Local(ctx, path, "remote", "get-url", "origin")
		if err != nil {
			res := &result{repo, statusSkipped, "origin is missing"}
			r.record(ctx, summary, res)

			continue
		}

		repo.endpoint = r.endpoint(remote)

		common, err := r.git.Local(ctx, path, "rev-parse", "--git-common-dir")
		if err != nil {
			res := &result{repo, statusFailed, err.Error()}
			r.record(ctx, summary, res)

			continue
		}

		if !filepath.IsAbs(common) {
			common = filepath.Join(path, common)
		}

		repo.common, err = filepath.EvalSymlinks(common)
		if err != nil {
			res := &result{repo, statusFailed, err.Error()}
			r.record(ctx, summary, res)

			continue
		}

		ready = append(ready, repo)
	}

	return ready, nil
}

// preflight checks each distinct remote once before any repository is updated.
func (r *runner) preflight(ctx context.Context, repos []*repository, summary *Summary) ([]*repository, error) {
	checked := map[string]struct{}{}
	ready := make([]*repository, 0, len(repos))
	failed := 0

	for _, repo := range repos {
		if _, seen := checked[repo.endpoint]; seen {
			ready = append(ready, repo)
			continue
		}

		err := r.probe(ctx, repo)
		if err == nil {
			checked[repo.endpoint] = struct{}{}
			ready = append(ready, repo)

			continue
		}

		if gitter.NetworkError(err) || ctx.Err() != nil {
			summary.Canceled += len(repos) - failed
			return nil, fmt.Errorf("remote unavailable; no repository updates started: %w", err)
		}

		res := &result{repo, statusFailed, "preflight: " + err.Error()}
		r.record(ctx, summary, res)

		failed++
	}

	return ready, nil
}
