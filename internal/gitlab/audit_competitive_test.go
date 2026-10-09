package gitlab

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

// A timed-out HTTP attempt must be retried while the overall budget is alive.
func TestAuditAttemptTimeoutUsesThreeAttempts(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++

		return nil, context.DeadlineExceeded
	})
	httpClient := &http.Client{Transport: transport}
	client := testClient("https://gitlab.example.invalid", httpClient)
	client.Attempts = 3
	client.RetryDelay = 0
	client.Budget = time.Minute
	_, err := client.ListProjects(t.Context(), []string{"mailion"})

	if !errors.Is(err, context.DeadlineExceeded) || calls != 3 {
		t.Fatalf("attempt timeout consumed %d attempts, want 3; err=%v", calls, err)
	}
}

func TestAuditCanceledParentDoesNotRetry(t *testing.T) {
	calls := 0
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++

		return nil, context.DeadlineExceeded
	})
	httpClient := &http.Client{Transport: transport}
	client := testClient("https://gitlab.example.invalid", httpClient)
	client.Attempts = 3
	_, err := client.ListProjects(ctx, []string{"mailion"})

	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}
