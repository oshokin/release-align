package app

import (
	"context"
	"fmt"
	"sync"

	"github.com/oshokin/release-align/internal/gitter"
)

// processAll updates ready repositories in parallel and serializes shared worktrees.
func (r *runner) processAll(
	ctx context.Context,
	cancel context.CancelCauseFunc,
	repos []*repository,
	summary *Summary,
) {
	// Git worktrees share refs and objects, so serialize them by common directory.
	locks := map[string]*sync.Mutex{}
	for _, repo := range repos {
		if locks[repo.common] == nil {
			locks[repo.common] = &sync.Mutex{}
		}
	}

	jobs := make(chan *repository)
	results := make(chan *result, len(repos))

	var recoveryMu sync.Mutex

	recoverNetwork := func(repo *repository) error {
		recoveryMu.Lock()
		defer recoveryMu.Unlock()

		if ctx.Err() != nil {
			return context.Cause(ctx)
		}

		err := r.probe(ctx, repo)
		if gitter.NetworkError(err) {
			cancel(fmt.Errorf("remote became unavailable; remaining work canceled: %w", err))
		}

		return err
	}

	var wg sync.WaitGroup

	for range min(r.cfg.Jobs, len(repos)) {
		wg.Go(func() {
			for repo := range jobs {
				lock := locks[repo.common]
				lock.Lock()

				res := &result{repo, statusCanceled, messageIdleCancel}
				if ctx.Err() == nil {
					res = r.process(ctx, repo, recoverNetwork)
				}

				results <- res
				lock.Unlock()
			}
		})
	}

	go func() {
		defer close(jobs)

		for _, repo := range repos {
			jobs <- repo
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	for res := range results {
		r.record(ctx, summary, res)
	}
}
