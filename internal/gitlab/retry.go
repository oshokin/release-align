package gitlab

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/oshokin/release-align/internal/retry"
)

// gitlabDelay is the pause for one withRetry call. It is not stored on Client.
type gitlabDelay struct {
	// configured is Client.RetryDelay.
	configured time.Duration
	// server is the latest Retry-After value. It is cleared at the start of each attempt.
	server time.Duration
}

// Delay returns the longer of the configured pause and Retry-After.
func (d *gitlabDelay) Delay(uint64) time.Duration {
	if d == nil {
		return 0
	}

	return max(d.configured, d.server)
}

// withRetry runs op at most Attempts times. Attempts counts the first try.
func (c *Client) withRetry(ctx context.Context, op func(context.Context) error) error {
	attempts := max(c.Attempts, 1)

	if attempts == 1 {
		return op(ctx)
	}

	wait := &gitlabDelay{
		configured: c.RetryDelay,
	}
	cfg := &retry.EngineConfig{
		MaxRetries:  uint64(attempts - 1),
		DelayPolicy: wait,
		IsRetryable: c.retryableGitLab,
	}
	wrapped := func(ctx context.Context) error {
		wait.server = 0
		err := op(ctx)

		if status, ok := errors.AsType[*statusError](err); ok {
			wait.server = status.wait
		}

		return err
	}

	return retry.Do(ctx, cfg, wrapped)
}

// retryableGitLab allows another try for a timeout, a dropped connection, 429, or 5xx.
func (c *Client) retryableGitLab(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, errGitLabRedirect) {
		return false
	}

	if errors.Is(err, errGitLabRateLimited) || errors.Is(err, errGitLabIncomplete) || errors.Is(err, errGitLabPage) {
		return false
	}

	if c.tlsTrustFailure(err) {
		return false
	}

	if status, ok := errors.AsType[*statusError](err); ok {
		return status.status == 429 || status.status >= 500
	}

	if unknown, ok := errors.AsType[*x509.UnknownAuthorityError](err); ok {
		return unknown != nil
	}

	if value, ok := errors.AsType[x509.UnknownAuthorityError](err); ok {
		return value.Error() != ""
	}

	if record, ok := errors.AsType[tls.RecordHeaderError](err); ok {
		return record.Error() != ""
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	netErr, ok := errors.AsType[net.Error](err)

	return ok && netErr != nil
}

// tlsTrustFailure reports a certificate the process will not retry.
func (c *Client) tlsTrustFailure(err error) bool {
	if err == nil {
		return false
	}

	if unknown, ok := errors.AsType[*x509.UnknownAuthorityError](err); ok && unknown != nil {
		return true
	}

	if value, ok := errors.AsType[x509.UnknownAuthorityError](err); ok && value.Error() != "" {
		return true
	}

	text := strings.ToLower(err.Error())

	return strings.Contains(text, "x509:") || strings.Contains(text, "failed to verify certificate")
}

// sortProjects orders a finished catalog by namespace path.
func (c *Client) sortProjects(projects []*Project) {
	slices.SortFunc(projects, func(left, right *Project) int {
		if left == nil || right == nil {
			return 0
		}

		return strings.Compare(left.PathWithNamespace, right.PathWithNamespace)
	})
}
