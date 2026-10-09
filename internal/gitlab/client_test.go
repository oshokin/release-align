package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"go.uber.org/zap/zapcore"

	"github.com/oshokin/release-align/internal/logger"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestListProjectsPaginatesAndDedupes(t *testing.T) {
	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)

		if r.Header.Get("Private-Token") != "secret" {
			t.Errorf("token header = %q", r.Header.Get("Private-Token"))
		}

		query := r.URL.Query()
		if query.Get("include_subgroups") != "true" || query.Get("with_shared") != "false" ||
			query.Get("archived") != "false" || query.Get("per_page") != "100" {
			t.Fatalf("query %s", r.URL.RawQuery)
		}

		page, convErr := strconv.Atoi(query.Get("page"))
		if convErr != nil || page < 1 {
			page = 1
		}

		start := (page - 1) * projectsPerPage
		total := 101
		end := min(start+projectsPerPage, total)

		batch := make([]*Project, 0, end-start)
		for id := start + 1; id <= end; id++ {
			project := &Project{
				ID:                id,
				PathWithNamespace: "lamiona/svc-" + strconv.Itoa(id),
				DefaultBranch:     "master",
			}
			batch = append(batch, project)
		}

		encodeJSON(t, w, batch)
	}))
	t.Cleanup(server.Close)

	client := testClient(server.URL, server.Client())
	client.Attempts = 1
	projects, err := client.ListProjects(context.Background(), []string{"lamiona"})

	if err != nil || len(projects) != 101 || calls.Load() != 2 {
		t.Fatalf("projects %d calls %d err %v", len(projects), calls.Load(), err)
	}
}

func TestListProjectsStopsOnEmptyNextHeader(t *testing.T) {
	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("X-Next-Page", "")
		batch := make([]*Project, projectsPerPage)

		for i := range batch {
			batch[i] = &Project{
				ID:                i + 1,
				PathWithNamespace: "lamiona/full-" + strconv.Itoa(i),
				DefaultBranch:     "master",
			}
		}

		encodeJSON(t, w, batch)
	}))
	t.Cleanup(server.Close)

	client := testClient(server.URL, server.Client())
	projects, err := client.ListProjects(context.Background(), []string{"lamiona"})

	if err != nil || len(projects) != projectsPerPage || calls.Load() != 1 {
		t.Fatalf("len %d calls %d err %v", len(projects), calls.Load(), err)
	}
}

func TestListProjectsSkipsSinglePageProgress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Total-Pages", "1")
		w.Header().Set("X-Next-Page", "")
		encodeJSON(t, w, []*Project{{
			ID:                1,
			PathWithNamespace: "UCS-3DPARTY/arangodb-go-driver",
			DefaultBranch:     "master",
		}})
	}))
	t.Cleanup(server.Close)

	var buf bytes.Buffer

	ctx := logger.ToContext(t.Context(), logger.NewWithWriter(zapcore.InfoLevel, &buf))
	client := testClient(server.URL, server.Client())
	projects, err := client.ListProjects(ctx, []string{"UCS-3DPARTY"})

	plain := buf.String()
	listed := strings.Contains(plain, "page listed")
	fetched := strings.Contains(plain, "GitLab GET")
	failed := err != nil || len(projects) != 1 || !fetched || listed

	if failed {
		t.Fatalf("projects %d err %v log %s", len(projects), err, plain)
	}
}

func TestListProjectsRejectsRepeatedPageAndLimits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Next-Page", r.URL.Query().Get("page"))
		project := &Project{
			ID:                1,
			PathWithNamespace: "lamiona/one",
			DefaultBranch:     "master",
		}
		page := []*Project{project}
		encodeJSON(t, w, page)
	}))
	t.Cleanup(server.Close)

	client := testClient(server.URL, server.Client())
	if _, err := client.ListProjects(context.Background(), []string{"lamiona"}); err == nil {
		t.Fatal("repeated page succeeded")
	}

	limited := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		batch := make([]*Project, projectsPerPage)
		for i := range batch {
			batch[i] = &Project{
				ID:                i + 1,
				PathWithNamespace: "lamiona/p-" + strconv.Itoa(i),
				DefaultBranch:     "master",
			}
		}

		encodeJSON(t, w, batch)
	}))
	t.Cleanup(limited.Close)

	client = testClient(limited.URL, limited.Client())
	client.MaxPages = 1

	if _, err := client.ListProjects(context.Background(), []string{"lamiona"}); err == nil {
		t.Fatal("page limit succeeded")
	}
}

func TestListProjectsPageFailureIsIncomplete(t *testing.T) {
	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)

		if r.URL.Query().Get("page") == "2" {
			http.Error(w, "secret-token-body", http.StatusInternalServerError)

			return
		}

		batch := make([]*Project, projectsPerPage)
		for i := range batch {
			batch[i] = &Project{
				ID:                i + 1,
				PathWithNamespace: "lamiona/p-" + strconv.Itoa(i),
				DefaultBranch:     "master",
			}
		}

		encodeJSON(t, w, batch)
	}))
	t.Cleanup(server.Close)

	client := testClient(server.URL, server.Client())
	client.Attempts = 1
	_, err := client.ListProjects(context.Background(), []string{"lamiona", "other"})

	if err == nil || calls.Load() != 2 || strings.Contains(err.Error(), "secret-token-body") {
		t.Fatal(err, calls.Load())
	}
}

func TestListProjectsDedupesGroupsAndRejectsContradiction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := 7
		path := "lamiona/shared"

		if r.URL.Path == "/api/v4/groups/other/projects" {
			path = "other/shared"
		}

		project := &Project{
			ID:                id,
			PathWithNamespace: path,
			DefaultBranch:     "master",
		}
		page := []*Project{project}
		encodeJSON(t, w, page)
	}))
	t.Cleanup(server.Close)

	client := testClient(server.URL, server.Client())
	if _, err := client.ListProjects(context.Background(), []string{"lamiona", "other"}); err == nil {
		t.Fatal("contradictory paths succeeded")
	}
}

func TestListProjectsAuthIsNotRetried(t *testing.T) {
	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "no", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	client := testClient(server.URL, server.Client())
	client.Attempts = 3

	if _, err := client.ListProjects(context.Background(), []string{"lamiona"}); err == nil || calls.Load() != 1 {
		t.Fatal(err, calls.Load())
	}
}

func TestListProjectsRetriesTransientThenStops(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var first, second int

		client := clockClient(5*time.Second, 3, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v4/groups/other/projects" {
				second++
			} else {
				first++
			}

			http.Error(w, "down", http.StatusBadGateway)
		})
		client.RetryDelay = time.Millisecond
		started := time.Now()

		_, err := client.ListProjects(t.Context(), []string{"lamiona", "other"})
		if err == nil {
			t.Fatal("outage succeeded")
		}

		if first != 3 || second != 0 || time.Since(started) != 2*time.Millisecond {
			t.Fatalf("first %d second %d elapsed %s err %v", first, second, time.Since(started), err)
		}
	})
}

func TestListProjectsDoesNotFollowRedirect(t *testing.T) {
	var leaked atomic.Int32

	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		leaked.Add(1)
	}))
	t.Cleanup(other.Close)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/api/v4/groups/lamiona/projects", http.StatusFound)
	}))
	t.Cleanup(server.Close)

	client := testClient(server.URL, server.Client())
	client.Attempts = 3

	if _, err := client.ListProjects(context.Background(), []string{"lamiona"}); err == nil || leaked.Load() != 0 {
		t.Fatal(err, leaked.Load())
	}
}

func TestListProjectsTLSFailureIsNotRetried(t *testing.T) {
	var calls atomic.Int32

	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(server.Close)

	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatal("default transport")
	}

	transport := base.Clone()
	counting := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)

		return transport.RoundTrip(r)
	})
	httpClient := &http.Client{
		Transport: counting,
	}
	client := testClient(server.URL, httpClient)
	client.Attempts = 3

	if _, err := client.ListProjects(context.Background(), []string{"lamiona"}); err == nil || calls.Load() != 1 {
		t.Fatal(err, calls.Load())
	}
}

func TestListProjectsHonorsCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := testClient(server.URL, server.Client())
	client.Budget = time.Second

	if _, err := client.ListProjects(ctx, []string{"lamiona"}); err == nil {
		t.Fatal("canceled list succeeded")
	}
}

func testClient(base string, httpClient *http.Client) *Client {
	return &Client{
		BaseURL:        base,
		Token:          "secret",
		HTTP:           httpClient,
		AttemptTimeout: time.Second,
		Budget:         5 * time.Second,
		Attempts:       1,
		RetryDelay:     time.Millisecond,
	}
}

func encodeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()

	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Error(err)
	}
}
