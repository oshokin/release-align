package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/oshokin/release-align/internal/logger"
)

// pageResult is one decoded page and the page number that follows it.
type pageResult struct {
	// projects are the projects on this page.
	projects []*Project
	// next is the following page number. Zero means the listing ended.
	next int
	// pages is X-Total-Pages. Zero means the server did not say how many pages exist.
	pages int
}

// statusError is a GitLab HTTP status without the response body.
type statusError struct {
	// status is the HTTP status code.
	status int
	// path is the request path, without the token.
	path string
	// wait is the Retry-After pause for this response. Zero means the configured delay.
	wait time.Duration
}

// apiEndpoint is one groups API URL and the path written to the log.
type apiEndpoint struct {
	// raw is the full request URL.
	raw string
	// path is the path logged without query secrets.
	path string
}

// gitLabPageNote is one listed page and the counter that records it.
type gitLabPageNote struct {
	// progress is the counter for this group. Nil starts a new one.
	progress *logger.Progress
	// group is the GitLab group path shown on the line.
	group string
	// page is the page number that was just listed.
	page int
	// result is the decoded page. Nil skips the line.
	result *pageResult
}

// Error returns the status and request path.
func (e *statusError) Error() string {
	if e == nil {
		return errGitLabStatus.Error()
	}

	return fmt.Sprintf("%s: %s returned %d", errGitLabStatus.Error(), e.path, e.status)
}

// listGroup reads one group until a short page or an empty X-Next-Page.
func (c *Client) listGroup(ctx context.Context, group string) ([]*Project, error) {
	projects := make([]*Project, 0)
	seen := make(map[int]struct{})
	page := 1
	limit := c.MaxPages

	var progress *logger.Progress

	if limit <= 0 {
		limit = defaultMaxPages
	}

	for {
		if ctx.Err() != nil {
			return nil, context.Cause(ctx)
		}

		if page > limit {
			return nil, errGitLabIncomplete
		}

		result, err := c.fetchPage(ctx, group, page)
		if err != nil {
			return nil, err
		}

		if err = c.newProjectIDs(seen, result.projects); err != nil {
			return nil, err
		}

		projects = append(projects, result.projects...)
		note := &gitLabPageNote{
			progress: progress,
			group:    group,
			page:     page,
			result:   result,
		}
		c.noteGitLabPage(ctx, note)
		progress = note.progress

		if result.next == 0 {
			return projects, nil
		}

		if result.next <= page {
			return nil, errGitLabPage
		}

		page = result.next
	}
}

// fetchPage retries one page. The returned projects are not a complete catalog.
func (c *Client) fetchPage(ctx context.Context, group string, page int) (*pageResult, error) {
	var result *pageResult

	err := c.withRetry(ctx, func(ctx context.Context) error {
		pageResult, pageErr := c.doFetch(ctx, group, page)
		if pageErr != nil {
			return pageErr
		}

		result = pageResult

		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// doFetch performs one GET and does not print the response body.
func (c *Client) doFetch(ctx context.Context, group string, page int) (*pageResult, error) {
	endpoint, err := c.projectURL(group, page)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.raw, http.NoBody)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Private-Token", c.Token)
	logger.Infof(ctx, "GitLab GET %s page %d", endpoint.path, page)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		logger.Warnf(ctx, "GitLab GET %s page %d: %v", endpoint.path, page, err)

		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, c.rateLimit(ctx, resp, endpoint.path)
	}

	if resp.StatusCode != http.StatusOK {
		logger.Warnf(ctx, "GitLab GET %s status %d", endpoint.path, resp.StatusCode)

		failure := &statusError{
			status: resp.StatusCode,
			path:   endpoint.path,
		}

		return nil, failure
	}

	return c.readPage(resp)
}

// projectURL builds the groups API URL. The token stays in the header.
func (c *Client) projectURL(group string, page int) (*apiEndpoint, error) {
	base, err := url.Parse(strings.TrimRight(c.BaseURL, "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, errGitLabStatus
	}

	values := url.Values{}
	values.Set("include_subgroups", "true")
	values.Set("with_shared", "false")
	values.Set("archived", "false")
	values.Set("order_by", "id")
	values.Set("sort", "asc")
	values.Set("per_page", strconv.Itoa(projectsPerPage))
	values.Set("page", strconv.Itoa(page))
	base.Path = "/api/v4/groups/" + group + "/projects"
	base.RawPath = "/api/v4/groups/" + url.PathEscape(group) + "/projects"
	base.RawQuery = values.Encode()
	endpoint := &apiEndpoint{
		raw:  base.String(),
		path: base.RawPath,
	}

	return endpoint, nil
}

// readPage decodes one body and chooses the following page number.
func (c *Client) readPage(resp *http.Response) (*pageResult, error) {
	limit := c.MaxBody
	if limit <= 0 {
		limit = defaultMaxBody
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}

	if int64(len(body)) > limit {
		return nil, errGitLabIncomplete
	}

	var decoded []*Project

	if err = json.Unmarshal(body, &decoded); err != nil {
		return nil, errGitLabIncomplete
	}

	next, err := c.nextPage(resp.Header, len(decoded), c.pageNumber(resp.Request))
	if err != nil {
		return nil, err
	}

	result := &pageResult{
		projects: decoded,
		next:     next,
		pages:    c.totalPages(resp.Header),
	}

	return result, nil
}

// nextPage honors an empty X-Next-Page as the end and a missing header as unknown.
func (c *Client) nextPage(header http.Header, count, page int) (int, error) {
	values := header.Values("X-Next-Page")
	if len(values) > 0 {
		if values[0] == "" {
			return 0, nil
		}

		next, err := strconv.Atoi(values[0])
		if err != nil || next <= page {
			return 0, errGitLabPage
		}

		return next, nil
	}

	if count < projectsPerPage {
		return 0, nil
	}

	return page + 1, nil
}

// pageNumber reads the page query this response answered.
func (c *Client) pageNumber(req *http.Request) int {
	if req == nil || req.URL == nil {
		return 1
	}

	page, err := strconv.Atoi(req.URL.Query().Get("page"))
	if err != nil || page < 1 {
		return 1
	}

	return page
}

// totalPages reads X-Total-Pages. An absent or unusable header means the size is unknown.
func (c *Client) totalPages(header http.Header) int {
	pages, err := strconv.Atoi(header.Get("X-Total-Pages"))
	if err != nil || pages < 1 {
		return 0
	}

	return pages
}

// noteGitLabPage records one listed page. The first page learns the total when the server sends it.
// A group that fits on one page is not given a 1/1 progress line. The GET line already names it.
func (c *Client) noteGitLabPage(ctx context.Context, note *gitLabPageNote) {
	if note == nil || note.result == nil || c.oneGitLabPage(note) {
		return
	}

	if note.progress == nil {
		total := note.result.pages
		if total < note.page {
			total = 0
		}

		note.progress = logger.NewProgress("gitlab", total)
	}

	note.progress.Advance(ctx, note.group, "page listed")
}

// oneGitLabPage reports a listing that finished on its first page.
func (*Client) oneGitLabPage(note *gitLabPageNote) bool {
	if note == nil || note.result == nil || note.page != 1 || note.result.next != 0 {
		return false
	}

	return note.result.pages <= 1
}

// rateLimit stops when Retry-After is longer than the remaining catalog budget.
func (c *Client) rateLimit(ctx context.Context, resp *http.Response, path string) error {
	wait := c.retryAfter(resp.Header.Get("Retry-After"))
	deadline, ok := ctx.Deadline()

	if ok && time.Until(deadline) < wait {
		return errGitLabRateLimited
	}

	failure := &statusError{
		status: resp.StatusCode,
		path:   path,
		wait:   wait,
	}

	return failure
}

// newProjectIDs rejects a non-empty page that repeats only IDs already seen in this group.
func (c *Client) newProjectIDs(seen map[int]struct{}, projects []*Project) error {
	fresh := 0

	for _, project := range projects {
		if project == nil {
			continue
		}

		if _, ok := seen[project.ID]; ok {
			continue
		}

		seen[project.ID] = struct{}{}
		fresh++
	}

	if len(projects) > 0 && fresh == 0 {
		return errGitLabPage
	}

	return nil
}

// retryAfter parses a delay. An unusable value falls back to one second.
func (c *Client) retryAfter(raw string) time.Duration {
	if raw == "" {
		return time.Second
	}

	seconds, err := strconv.Atoi(raw)
	if err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}

	when, err := http.ParseTime(raw)
	if err != nil {
		return time.Second
	}

	wait := time.Until(when)

	if wait < 0 {
		return 0
	}

	return wait
}

// httpClient applies the attempt timeout and refuses authenticated redirects.
func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		client := *c.HTTP
		client.CheckRedirect = refuseRedirect

		if client.Timeout == 0 {
			client.Timeout = c.AttemptTimeout
		}

		return &client
	}

	return &http.Client{
		Timeout:       c.AttemptTimeout,
		CheckRedirect: refuseRedirect,
	}
}

// refuseRedirect keeps PRIVATE-TOKEN on the configured origin.
func refuseRedirect(*http.Request, []*http.Request) error {
	return errGitLabRedirect
}
