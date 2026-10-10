package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/oshokin/release-align/internal/gitlab"
	"github.com/oshokin/release-align/internal/gitter"
)

// remoteHooks replaces GitLab access in tests. Production leaves it nil.
type remoteHooks struct {
	// token is sent as PRIVATE-TOKEN. It is never written to the workspace file.
	token string
	// httpClient is the test server client. Production uses the default client.
	httpClient *http.Client
	// allowLocalClone lets tests clone filesystem paths. Production leaves it false.
	allowLocalClone bool
}

// localRoot is one Git worktree found under the base directory.
type localRoot struct {
	// path is the workspace-relative directory.
	path string
	// origin is the raw origin URL from git remote get-url.
	origin string
}

// remoteCheck is the local result that a GitLab comparison is attached to.
type remoteCheck struct {
	// cfg carries the --remote flag, timeouts, and test hooks.
	cfg *Config
	// spec is the workspace inventory.
	spec *WorkspaceSpec
	// report is the local alignment result.
	report *WorkspaceReport
	// runErr is the error from the local operation, if one happened.
	runErr error
	// projects is the catalog read before checkout. Nil means this call may list.
	projects []*gitlab.Project
	// listed reports that projects was already read for this run.
	listed bool
	// archiveBefore is the saved Archived flag for each path, taken before the in-memory skip mark.
	archiveBefore map[string]bool
}

// projectCompare is one complete catalog matched against disk and the workspace file.
type projectCompare struct {
	// git runs local Git commands for origin lookups.
	git LocalGit
	// base is the directory that contains the clones.
	base string
	// spec is the workspace inventory.
	spec *WorkspaceSpec
	// projects is the complete non-archived catalog.
	projects []*gitlab.Project
	// allowLocal matches filesystem clone URLs. Tests set it.
	allowLocal bool
}

const (
	// remoteStatusChecked means every configured group was listed.
	remoteStatusChecked = "checked"
	// remoteStatusFailed means the catalog is incomplete.
	remoteStatusFailed = "failed"
	// remoteStatusSkipped means GitLab was not queried.
	remoteStatusSkipped = "not_checked"
	// remoteReasonCancel means the run was canceled before the listing.
	remoteReasonCancel = "canceled"
	// remoteReasonNetwork means an earlier network failure blocked another listing.
	remoteReasonNetwork = "prior_network_failure"
	// remoteNotCheckedMsg tells the user the run used saved archive marks.
	remoteNotCheckedMsg = "GitLab lifecycle: not checked; using workspace flags"
)

// PrepareRemote rejects a requested catalog before checkout when the configuration is unusable.
func PrepareRemote(cfg *Config, spec *WorkspaceSpec) error {
	if cfg == nil || !cfg.Remote {
		return nil
	}

	if cfg.DryRun {
		return errRemoteDryRun
	}

	if spec == nil || spec.GitLab == nil {
		return errGitLabSource
	}

	if err := spec.GitLab.ValidateForAPI(); err != nil {
		return err
	}

	if gitlabToken(cfg) == "" {
		return errRemoteToken
	}

	return nil
}

// attachRemote adds the catalog after the local operation, or records why it was skipped.
func attachRemote(ctx context.Context, check *remoteCheck) (*WorkspaceReport, error) {
	if check == nil {
		return nil, errRemoteInventory
	}

	if check.report == nil {
		return nil, check.runErr
	}

	cfg := check.cfg
	spec := check.spec
	report := check.report
	runErr := check.runErr

	if cfg == nil || !cfg.Remote {
		report.RemoteInventory = remoteNote(spec, remoteStatusSkipped, "", remoteNotCheckedMsg)

		return report, runErr
	}

	if ctx.Err() != nil {
		report.RemoteInventory = remoteNote(spec, remoteStatusSkipped, remoteReasonCancel, ctx.Err().Error())

		return report, preferErr(runErr, context.Cause(ctx))
	}

	if remoteNetworkFailed(report, runErr) && !check.listed {
		note := "GitLab inventory: not checked because the host was already unreachable"
		report.RemoteInventory = remoteNote(spec, remoteStatusSkipped, remoteReasonNetwork, note)

		return report, runErr
	}

	projects := check.projects
	if !check.listed {
		if cfg.progress != nil {
			_, _ = fmt.Fprintln(cfg.progress, "Checking GitLab inventory...")
		}

		var listErr error

		projects, listErr = listRemoteProjects(ctx, cfg, spec.GitLab)
		if listErr != nil {
			return failRemote(spec, report, runErr, listErr)
		}
	}

	query := &projectCompare{
		git:        offlineGit(cfg),
		base:       cfg.BaseDir,
		spec:       spec,
		projects:   projects,
		allowLocal: allowLocal(cfg),
	}

	inventory, err := compareProjects(ctx, query)
	if err != nil {
		return failRemote(spec, report, runErr, err)
	}

	overlayArchiveDiff(inventory.Catalog, check.archiveBefore, projects)
	report.RemoteInventory = inventory

	return report, runErr
}

// archiveFlags copies the saved archive mark for each path.
func archiveFlags(spec *WorkspaceSpec) map[string]bool {
	if spec == nil {
		return nil
	}

	flags := make(map[string]bool, len(spec.Projects))
	for _, project := range spec.Projects {
		if project != nil {
			flags[project.Path] = project.Archived
		}
	}

	return flags
}

// overlayArchiveDiff restores transitions that were computed against the file, not the in-memory skip mark.
func overlayArchiveDiff(catalog *RemoteCatalog, before map[string]bool, projects []*gitlab.Project) {
	if catalog == nil || before == nil {
		return
	}

	server := make(map[string]bool, len(projects))
	for _, project := range projects {
		if project != nil && project.PathWithNamespace != "" {
			server[project.PathWithNamespace] = project.Archived
		}
	}

	archived := make([]string, 0)
	active := make([]string, 0)

	for path, was := range before {
		now, found := server[path]
		if !found || now == was {
			continue
		}

		if now {
			archived = append(archived, path)

			continue
		}

		active = append(active, path)
	}

	slices.Sort(archived)
	slices.Sort(active)
	catalog.BecameArchived = archived
	catalog.BecameActive = active
}

// noteServerArchived marks listed projects that GitLab currently reports as archived.
// A saved archive mark is left in place when the server says the project is active.
func noteServerArchived(spec *WorkspaceSpec, projects []*gitlab.Project) {
	if spec == nil {
		return
	}

	byPath := make(map[string]*gitlab.Project, len(projects))
	for _, project := range projects {
		if project != nil {
			byPath[project.PathWithNamespace] = project
		}
	}

	for _, project := range spec.Projects {
		if project == nil || project.Archived {
			continue
		}

		server := byPath[project.Path]
		if server != nil && server.Archived {
			project.Archived = true
		}
	}
}

// listRemoteProjects reads every configured group once.
func listRemoteProjects(ctx context.Context, cfg *Config, source *GitLabSource) ([]*gitlab.Project, error) {
	client := gitlabClient(cfg, source)

	return client.ListProjects(ctx, source.Groups)
}

// compareProjects matches one complete catalog to the disk and the workspace file.
func compareProjects(ctx context.Context, query *projectCompare) (*RemoteInventory, error) {
	if query == nil {
		return nil, errRemoteInventory
	}

	roots, err := scanLocalClones(ctx, query.git, query.base, query.spec)
	if err != nil {
		return nil, err
	}

	diffQuery := &remoteDiffQuery{
		spec:       query.spec,
		roots:      roots,
		base:       query.base,
		allowLocal: query.allowLocal,
		count:      len(query.projects),
	}
	diff := newRemoteDiff(diffQuery)
	diff.catalog.VisibleCount = len(query.projects)
	diff.classifyAll(query.projects)
	diff.leftovers()
	diff.finish()

	return checkedInventory(query.spec.GitLab, diff.catalog), nil
}

// failRemote records an incomplete catalog and chooses the process error.
func failRemote(spec *WorkspaceSpec, report *WorkspaceReport, runErr, cause error) (*WorkspaceReport, error) {
	wrapped := fmt.Errorf("%w: %w", errRemoteInventory, cause)
	if report.Ready && report.Mode == ModeSync {
		wrapped = fmt.Errorf("%w: %w", errRemoteAfterSync, cause)
	}

	report.Errors = append(report.Errors, wrapped.Error())
	report.RemoteInventory = remoteNote(spec, remoteStatusFailed, "", wrapped.Error())

	if runErr == nil || errors.Is(runErr, errWorkspaceNotReady) {
		return report, wrapped
	}

	return report, runErr
}

// remoteNote builds a result that has no catalog counts.
func remoteNote(spec *WorkspaceSpec, status, reason, message string) *RemoteInventory {
	note := &RemoteInventory{
		Status: status,
		Reason: reason,
		Error:  message,
	}
	if spec != nil && spec.GitLab != nil {
		note.URL = spec.GitLab.URL
		note.Groups = slices.Clone(spec.GitLab.Groups)
	}

	return note
}

// checkedInventory marks a complete listing.
func checkedInventory(source *GitLabSource, catalog *RemoteCatalog) *RemoteInventory {
	inventory := &RemoteInventory{
		Status:           remoteStatusChecked,
		CheckedAt:        time.Now().Format(time.RFC3339),
		IncludeSubgroups: boolPtr(true),
		IncludeArchived:  boolPtr(true),
		IncludeShared:    boolPtr(false),
		Catalog:          catalog,
	}
	if source != nil {
		inventory.URL = source.URL
		inventory.Groups = slices.Clone(source.Groups)
	}

	return inventory
}

// boolPtr returns a distinct bool for JSON false values.
func boolPtr(value bool) *bool {
	copied := value

	return &copied
}

// gitlabToken reads the test hook or GITLAB_TOKEN. The value is not logged.
func gitlabToken(cfg *Config) string {
	if cfg != nil && cfg.remoteHooks != nil && cfg.remoteHooks.token != "" {
		return cfg.remoteHooks.token
	}

	return os.Getenv("GITLAB_TOKEN")
}

// catalogBudget is Config.CatalogBudget. A nil config uses the built-in default.
func catalogBudget(cfg *Config) time.Duration {
	if cfg == nil || cfg.CatalogBudget <= 0 {
		return DefaultConfig().CatalogBudget
	}

	return cfg.CatalogBudget
}

// catalogAttemptTimeout is one page attempt. It stays inside the catalog budget
// so the remaining attempts and their pauses still fit.
func catalogAttemptTimeout(cfg *Config) time.Duration {
	page := DefaultConfig().CatalogTimeout
	if cfg != nil && cfg.CatalogTimeout > 0 {
		page = cfg.CatalogTimeout
	}

	budget := catalogBudget(cfg)
	if page > budget {
		return budget
	}

	attempts := 1
	pause := time.Duration(0)

	if cfg != nil && cfg.Attempts > 1 {
		attempts = cfg.Attempts
		if cfg.RetryDelay > 0 {
			pause = cfg.RetryDelay
		}
	}

	pauses := pause * time.Duration(attempts-1)
	if pauses >= budget {
		return page
	}

	share := (budget - pauses) / time.Duration(attempts)
	if share < page {
		return share
	}

	return page
}

// gitlabClient builds the thin API client for this invocation.
// A projects page uses CatalogTimeout. The listing uses CatalogBudget.
func gitlabClient(cfg *Config, source *GitLabSource) *gitlab.Client {
	client := &gitlab.Client{
		BaseURL:        source.URL,
		Token:          gitlabToken(cfg),
		AttemptTimeout: catalogAttemptTimeout(cfg),
		Budget:         catalogBudget(cfg),
		Attempts:       cfg.Attempts,
		RetryDelay:     cfg.RetryDelay,
	}
	if cfg.remoteHooks != nil {
		client.HTTP = cfg.remoteHooks.httpClient
	}

	return client
}

// allowLocal reports the test-only filesystem clone match.
func allowLocal(cfg *Config) bool {
	return cfg != nil && cfg.remoteHooks != nil && cfg.remoteHooks.allowLocalClone
}

// offlineGit reads local origins without asking for a promisor object.
func offlineGit(cfg *Config) *gitter.Client {
	timeout := time.Duration(0)
	if cfg != nil {
		timeout = cfg.LocalTimeout
	}

	return &gitter.Client{
		LocalTimeout: timeout,
		NoLazyFetch:  true,
	}
}

// remoteNetworkFailed reports a confirmed transport failure of the main operation.
func remoteNetworkFailed(report *WorkspaceReport, err error) bool {
	if gitter.NetworkError(err) {
		return true
	}

	if report == nil {
		return false
	}

	for _, row := range report.Rows {
		if row != nil && row.ReasonCode == reasonNetwork {
			return true
		}
	}

	return false
}

// scanLocalClones reads discovered roots and listed roots the walk does not enter.
func scanLocalClones(
	ctx context.Context,
	g LocalGit,
	base string,
	spec *WorkspaceSpec,
) (map[string]*localRoot, error) {
	discovered, err := DiscoverWorkspaceProjects(ctx, g, base)
	if err != nil {
		return nil, err
	}

	roots := make(map[string]*localRoot, len(discovered))
	for _, project := range discovered {
		root, rootErr := readLocalRoot(ctx, g, base, project.Path)
		if rootErr != nil {
			return nil, rootErr
		}

		roots[project.Path] = root
	}

	if spec == nil {
		return roots, nil
	}

	for _, project := range spec.Projects {
		if roots[project.Path] != nil {
			continue
		}

		missing, missErr := listedProjectMissing(ctx, g, base, project.Path)
		if missErr != nil {
			return nil, missErr
		}

		if missing {
			continue
		}

		root, rootErr := readLocalRoot(ctx, g, base, project.Path)
		if rootErr != nil {
			return nil, rootErr
		}

		roots[project.Path] = root
	}

	return roots, nil
}

// readLocalRoot reads one origin. The caller has already accepted the directory.
func readLocalRoot(ctx context.Context, g LocalGit, base, relative string) (*localRoot, error) {
	dir, err := ResolveProjectDirectory(base, relative)
	if err != nil {
		return nil, err
	}

	origin, err := g.Local(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		return nil, err
	}

	return &localRoot{
		path:   relative,
		origin: strings.TrimSpace(origin),
	}, nil
}
