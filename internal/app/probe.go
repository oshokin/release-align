package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/oshokin/release-align/internal/gitter"
	"github.com/oshokin/release-align/internal/logger"
	"github.com/oshokin/release-align/internal/retry"
)

// A per-command timeout is retryable while the parent run is still alive.
// Deliberately no Unwrap: the original engine never retries context errors.
// probe restores the original error before returning to the outage classifier.
type probeTimeoutError struct {
	// cause is the deadline error from one probe attempt.
	cause error
}

// Error returns the deadline error from one probe attempt.
func (e *probeTimeoutError) Error() string {
	return e.cause.Error()
}

// probe retries an origin check until the attempt budget is spent or the run is canceled.
func (r *runner) probe(ctx context.Context, repo *repository) error {
	ctx = logger.WithKV(ctx, "repo", repo.relative)
	attempt := 0
	operation := func(ctx context.Context) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		attempt++
		logger.Infof(ctx, "Checking origin: attempt %d/%d", attempt, r.cfg.Attempts)

		err := r.git.Probe(ctx, repo.path, r.cfg.ProbeTimeout)
		if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
			return &probeTimeoutError{cause: err}
		}

		return err
	}

	var err error

	if r.probeRetry == nil {
		// In the source engine MaxRetries=0 means unlimited, never use it for one attempt.
		err = operation(ctx)
	} else {
		req := &retry.Request{
			Operation: operation,
			OnRetry: func(ctx context.Context, info *retry.AttemptInfo) {
				logger.Warnf(ctx, "Origin check failed; next attempt in %s: %v", info.Delay, info.Err)
			},
		}
		err = r.probeRetry.Run(ctx, req)
	}

	if timeout, ok := errors.AsType[*probeTimeoutError](err); ok {
		err = timeout.cause
	}

	if err != nil && gitter.NetworkError(err) {
		return fmt.Errorf("origin unreachable after %d attempts: %w", attempt, err)
	}

	return err
}

// retryableProbe reports whether a failed origin check should be tried again.
func (r *runner) retryableProbe(err error) bool {
	var timeout *probeTimeoutError

	return errors.As(err, &timeout) || gitter.NetworkError(err)
}
