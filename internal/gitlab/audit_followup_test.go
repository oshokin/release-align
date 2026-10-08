package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

// TestAuditSubgroupEscaping requires one round of path escaping.
func TestAuditSubgroupEscaping(t *testing.T) {
	client := &Client{
		BaseURL: "https://gitlab.example.test",
	}

	endpoint, err := client.projectURL("lamiona/search", 1)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(endpoint.raw, "/groups/lamiona%2Fsearch/projects?") {
		t.Fatalf("subgroup URL is incorrectly escaped: %s", endpoint.raw)
	}
}

// TestAuditRetryAfter verifies the interval between HTTP attempts on the fake clock.
func TestAuditRetryAfter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls int

		client := clockClient(5*time.Second, 2, func(w http.ResponseWriter, _ *http.Request) {
			calls++

			if calls == 1 {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)

				return
			}

			fmt.Fprint(w, "[]")
		})
		started := time.Now()

		_, err := client.ListProjects(t.Context(), []string{"lamiona"})
		if err != nil {
			t.Fatal(err)
		}

		if elapsed := time.Since(started); elapsed != time.Second {
			t.Fatalf("Retry-After=1s ignored: actual interval %v", elapsed)
		}
	})
}

// TestAuditRepeatedBody rejects a pagination loop even if its page numbers advance.
func TestAuditRepeatedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "1":
			w.Header().Set("X-Next-Page", "2")
		case "2":
			w.Header().Set("X-Next-Page", "3")
		default:
			fmt.Fprint(w, "[]")

			return
		}

		fmt.Fprint(w, `[{"id":1,"path_with_namespace":"lamiona/one","default_branch":"master"}]`)
	}))
	t.Cleanup(server.Close)

	client := &Client{
		BaseURL:  server.URL,
		Token:    "test-token",
		HTTP:     server.Client(),
		Attempts: 1,
	}

	projects, err := client.ListProjects(context.Background(), []string{"lamiona"})
	if err == nil {
		t.Fatalf("repeated page body accepted as a complete catalog: %d project(s)", len(projects))
	}
}

// TestAuditRetryAfterHTTPDate sleeps until an HTTP-date Retry-After.
func TestAuditRetryAfterHTTPDate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		when := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)

		var calls int

		client := clockClient(5*time.Second, 2, func(w http.ResponseWriter, _ *http.Request) {
			calls++

			if calls == 1 {
				w.Header().Set("Retry-After", when)
				w.WriteHeader(http.StatusTooManyRequests)

				return
			}

			fmt.Fprint(w, "[]")
		})
		started := time.Now()

		_, err := client.ListProjects(t.Context(), []string{"lamiona"})
		if err != nil {
			t.Fatal(err)
		}

		if elapsed := time.Since(started); elapsed != 2*time.Second {
			t.Fatalf("HTTP-date Retry-After ignored: %v", elapsed)
		}
	})
}

// TestAuditRetryAfterCancel stops a Retry-After pause when the context is canceled.
func TestAuditRetryAfterCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())

		client := clockClient(2*time.Minute, 2, func(w http.ResponseWriter, _ *http.Request) {
			time.AfterFunc(20*time.Millisecond, cancel)
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusTooManyRequests)
		})
		started := time.Now()

		_, err := client.ListProjects(ctx, []string{"lamiona"})
		if !errors.Is(err, context.Canceled) || time.Since(started) != 20*time.Millisecond {
			t.Fatalf("pause was not canceled: %v after %s", err, time.Since(started))
		}
	})
}

// TestAuditRetryAfterBudget returns immediately when the pause does not fit.
func TestAuditRetryAfterBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := clockClient(200*time.Millisecond, 3, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusTooManyRequests)
		})
		started := time.Now()

		_, err := client.ListProjects(t.Context(), []string{"lamiona"})
		if !errors.Is(err, errGitLabRateLimited) || time.Since(started) != 0 {
			t.Fatalf("long Retry-After was retried: %v after %s", err, time.Since(started))
		}
	})
}

// TestAuditStopsAtThreeAttempts makes three calls and no fourth.
func TestAuditStopsAtThreeAttempts(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++

		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	client := &Client{
		BaseURL:    server.URL,
		Token:      "test-token",
		HTTP:       server.Client(),
		Budget:     5 * time.Second,
		Attempts:   3,
		RetryDelay: 0,
	}

	_, err := client.ListProjects(context.Background(), []string{"lamiona"})
	if err == nil || calls != 3 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

// clockClient answers HTTP from the caller goroutine so a synctest bubble can advance time.
func clockClient(budget time.Duration, attempts int, handler http.HandlerFunc) *Client {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		handler(rec, r)
		resp := rec.Result()
		resp.Request = r

		return resp, nil
	})

	return &Client{
		BaseURL:        "https://gitlab.example.test",
		Token:          "test-token",
		HTTP:           &http.Client{Transport: transport},
		Budget:         budget,
		AttemptTimeout: time.Second,
		Attempts:       attempts,
		RetryDelay:     0,
	}
}
