// Package gitlab reads project lists from one GitLab host.
package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Project is one non-archived project returned by the groups API.
type Project struct {
	// ID is the numeric GitLab project id used for deduplication.
	ID int `json:"id"`
	// PathWithNamespace is the group and project path.
	PathWithNamespace string `json:"path_with_namespace"`
	// SSHURLToRepo is the SSH clone URL from the API.
	SSHURLToRepo string `json:"ssh_url_to_repo"`
	// HTTPURLToRepo is the HTTPS clone URL from the API.
	HTTPURLToRepo string `json:"http_url_to_repo"`
	// DefaultBranch is the repository default branch. Empty means the project is not cloned implicitly.
	DefaultBranch string `json:"default_branch"`
	// Archived reports a project the listing asked GitLab to exclude.
	Archived bool `json:"archived"`
}

// Client lists projects for explicitly configured groups.
type Client struct {
	// BaseURL is the HTTPS origin of one GitLab host.
	BaseURL string
	// Token is sent as PRIVATE-TOKEN. It is not logged.
	Token string
	// HTTP is an optional client. Tests set it. Production may leave it nil.
	HTTP *http.Client
	// AttemptTimeout limits one HTTP request.
	AttemptTimeout time.Duration
	// Budget limits the whole catalog read.
	Budget time.Duration
	// Attempts is the total number of tries for one transient request.
	Attempts int
	// RetryDelay is the pause between transient attempts.
	RetryDelay time.Duration
	// MaxPages is the most pages read for one group.
	MaxPages int
	// MaxBody is the most bytes read from one response.
	MaxBody int64
}

const (
	// projectsPerPage is the page size requested from GitLab.
	projectsPerPage = 100
	// defaultMaxPages stops a listing that never ends.
	defaultMaxPages = 100
	// defaultMaxBody is the response cap when MaxBody is unset.
	defaultMaxBody = 1 << 20
)

var (
	// errGitLabPage means the next page did not advance.
	errGitLabPage = errors.New("GitLab pagination did not advance")
	// errGitLabIncomplete means the catalog must not be treated as complete.
	errGitLabIncomplete = errors.New("GitLab project list is incomplete")
	// errGitLabInconsistent means one project id was returned with two paths.
	errGitLabInconsistent = errors.New("GitLab returned one project id with two paths")
	// errGitLabRedirect means a redirect was refused so the token is not forwarded.
	errGitLabRedirect = errors.New("GitLab API redirect was not followed")
	// errGitLabRateLimited means Retry-After does not fit the remaining budget.
	errGitLabRateLimited = errors.New("GitLab API rate limit exceeds the remaining deadline")
	// errGitLabStatus means the request failed and the body was not shown.
	errGitLabStatus = errors.New("GitLab API request failed")
)

// ListProjects reads every configured group before returning a catalog.
// A failure discards pages already read. Projects are deduplicated by numeric id.
func (c *Client) ListProjects(ctx context.Context, groups []string) ([]*Project, error) {
	if c == nil || c.BaseURL == "" || c.Token == "" {
		return nil, errGitLabStatus
	}

	budget := c.Budget
	if budget <= 0 {
		budget = time.Minute
	}

	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	seen := make(map[int]*Project)
	all := make([]*Project, 0)

	for _, group := range groups {
		projects, err := c.listGroup(ctx, group)
		if err != nil {
			return nil, err
		}

		merged, err := mergeProjects(seen, projects)
		if err != nil {
			return nil, err
		}

		all = append(all, merged...)
	}

	sortProjects(all)

	return all, nil
}

// mergeProjects keeps the first path for an id and rejects a contradiction.
func mergeProjects(seen map[int]*Project, projects []*Project) ([]*Project, error) {
	added := make([]*Project, 0)

	for _, project := range projects {
		if project == nil || project.Archived {
			continue
		}

		prev, ok := seen[project.ID]
		if ok && prev.PathWithNamespace != project.PathWithNamespace {
			return nil, fmt.Errorf("%w: %d", errGitLabInconsistent, project.ID)
		}

		if ok {
			continue
		}

		seen[project.ID] = project
		added = append(added, project)
	}

	return added, nil
}
