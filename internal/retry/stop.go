package retry

import (
	"context"
	"errors"
	"fmt"
)

// retryStopError determines whether retries should stop and return an error.
func (e *Engine) retryStopError(ctx context.Context, operationErr error, retries uint64) error {
	// Context errors take priority and are never retried.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}

	if e.isContextOperationError(operationErr) {
		return operationErr
	}

	if !e.isRetryable(operationErr) {
		return operationErr
	}

	// With maxRetries == 0, retries are unlimited.
	if e.maxRetries != 0 && retries >= e.maxRetries {
		return operationErr
	}

	return nil
}

// scheduleRetry computes delay, invokes OnRetry, and waits before retrying.
func (e *Engine) scheduleRetry(ctx context.Context, req *Request, retries uint64, operationErr error) error {
	delay := e.delayPolicy.Delay(retries)
	if req.OnRetry != nil {
		info := &AttemptInfo{
			Retry:      retries,
			MaxRetries: e.maxRetries,
			Delay:      delay,
			Err:        operationErr,
		}

		req.OnRetry(ctx, info)
	}

	return e.sleeper(ctx, delay)
}

// AlwaysRetryable treats any error as retryable.
// Context errors are handled separately and are never retried.
func AlwaysRetryable(error) bool {
	return true
}

// validateRequest validates incoming request data for Run.
func (e *Engine) validateRequest(req *Request) error {
	if req == nil {
		return fmt.Errorf("%w: request is nil", ErrInvalidRequest)
	}

	if req.Operation == nil {
		return fmt.Errorf("%w: operation is nil", ErrInvalidRequest)
	}

	return nil
}

// isContextOperationError checks whether the error is a context termination error.
func (e *Engine) isContextOperationError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
