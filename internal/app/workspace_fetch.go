package app

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/oshokin/release-align/internal/gitter"
	"github.com/oshokin/release-align/internal/logger"
)

// fetchWorkspace fetches every pending repository and records failures.
func (r *runner) fetchWorkspace(ctx context.Context, cancel context.CancelCauseFunc, items []*workspaceItem) error {
	repos := make([]*repository, 0, len(items))

	for _, item := range items {
		if item.repo != nil && rowPending(item.row) {
			repos = append(repos, item.repo)
		}
	}

	if len(repos) == 0 {
		return nil
	}

	r.beginPhase(ctx, "fetch", len(repos))

	outcomes := r.fetchRepos(ctx, cancel, repos)

	for _, outcome := range outcomes {
		if outcome == nil || outcome.err == nil {
			continue
		}

		item := r.itemByRepo(items, outcome.repo)
		if item == nil {
			continue
		}

		code, message := r.workspaceGitReason(outcome.err)
		rowOutcome := outcomeBlocked

		if code == reasonCanceled || code == reasonNetwork {
			rowOutcome = outcomeCanceled
		}

		blockRow(item.row, rowOutcome, code, message)
	}

	if ctx.Err() != nil {
		markPending(items, outcomeCanceled, reasonCanceled, messageRunStopped)

		return context.Cause(ctx)
	}

	return nil
}

// fetchRepos fetches repositories in parallel, one at a time per common directory.
func (r *runner) fetchRepos(ctx context.Context, cancel context.CancelCauseFunc, repos []*repository) []*fetchOutcome {
	locks := map[string]*sync.Mutex{}

	for _, repo := range repos {
		if locks[repo.common] == nil {
			locks[repo.common] = new(sync.Mutex)
		}
	}

	jobs := make(chan *repository)
	results := make(chan *fetchOutcome, len(repos))

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
				results <- r.fetchLocked(ctx, repo, locks[repo.common], recoverNetwork)
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

	outcomes := make([]*fetchOutcome, 0, len(repos))

	for outcome := range results {
		outcomes = append(outcomes, outcome)
		r.noteFetch(ctx, outcome)
	}

	return outcomes
}

// noteFetch records one finished fetch in the phase counter.
func (r *runner) noteFetch(ctx context.Context, outcome *fetchOutcome) {
	if outcome == nil || outcome.repo == nil {
		return
	}

	if errors.Is(outcome.err, context.Canceled) {
		r.step(ctx, outcome.repo.relative, "fetch interrupted")

		return
	}

	if outcome.err != nil {
		r.progress.Error(ctx, outcome.repo.relative, "fetch failed")

		if r.report != nil {
			r.report.ProgressDone = r.progress.Done()
		}

		return
	}

	r.step(ctx, outcome.repo.relative, "fetched")
}

// fetchLocked fetches one repository while holding its common-directory lock.
func (r *runner) fetchLocked(
	ctx context.Context,
	repo *repository,
	lock *sync.Mutex,
	recoverNetwork func(*repository) error,
) *fetchOutcome {
	lock.Lock()
	defer lock.Unlock()

	if ctx.Err() != nil {
		stopped := &fetchOutcome{
			repo: repo,
			err:  context.Cause(ctx),
		}

		return stopped
	}

	fetchErr := r.fetch(ctx, repo, recoverNetwork)
	if fetchErr == nil {
		fetched := &fetchOutcome{
			repo: repo,
		}

		return fetched
	}

	if ctx.Err() == nil {
		failed := &fetchOutcome{
			repo: repo,
			err:  r.gitTextError(fetchErr.Error()),
		}

		return failed
	}

	err := r.canceledFetchError(ctx)
	if err == nil {
		err = r.gitTextError(fetchErr.Error())
	}

	canceled := &fetchOutcome{
		repo: repo,
		err:  err,
	}

	return canceled
}

// fetch updates origin once, and retries a single time after the remote is confirmed reachable.
func (r *runner) fetch(ctx context.Context, repo *repository, recoverNetwork func(*repository) error) error {
	logger.DebugKV(ctx, "Fetching origin branches and tags", "repo", repo.relative)

	err := r.git.Fetch(ctx, repo.path, r.cfg.FetchTimeout)
	if err == nil {
		return nil
	}

	if !gitter.NetworkError(err) {
		return err
	}

	if checkErr := recoverNetwork(repo); checkErr != nil {
		return checkErr
	}

	err = r.git.Fetch(ctx, repo.path, r.cfg.FetchTimeout)
	if err == nil {
		return nil
	}

	if !gitter.NetworkError(err) {
		return err
	}

	if checkErr := recoverNetwork(repo); checkErr != nil {
		return checkErr
	}

	return err
}

// canceledFetchError returns the cancellation cause when a fetch was stopped.
func (*runner) canceledFetchError(ctx context.Context) error {
	err := context.Cause(ctx)
	if err != nil {
		return err
	}

	return ctx.Err()
}
