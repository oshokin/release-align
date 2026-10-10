package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/oshokin/release-align/internal/gitlab"
	"github.com/oshokin/release-align/internal/gitter"
	"github.com/oshokin/release-align/internal/logger"
	"github.com/oshokin/release-align/internal/retry"
)

// clonePathQuery is the catalog slice this invocation may clone.
type clonePathQuery struct {
	// spec is the workspace inventory.
	spec *WorkspaceSpec
	// projects are the non-archived catalog entries.
	projects []*gitlab.Project
	// opts carries --repo or --all.
	opts *WorkspaceCloneOptions
	// byPath indexes the catalog by path_with_namespace.
	byPath map[string]*gitlab.Project
}

// cloneChoiceQuery is one catalog path and the local inventory around it.
type cloneChoiceQuery struct {
	// spec is the workspace inventory.
	spec *WorkspaceSpec
	// project is the catalog entry. Nil when the path is not in the catalog index.
	project *gitlab.Project
	// path is the workspace path.
	path string
	// inventory is the disk comparison for this catalog.
	inventory *RemoteInventory
}

// choose rejects unknown, conflicting, and empty selections before the first clone.
func (j *cloneJob) choose(ctx context.Context, projects []*gitlab.Project) (*clonePick, error) {
	query := &projectCompare{
		git:        offlineGit(j.cfg),
		base:       j.base,
		spec:       j.spec,
		projects:   projects,
		allowLocal: allowLocal(j.cfg),
	}

	inventory, err := compareProjects(ctx, query)
	if err != nil {
		return nil, err
	}

	byPath := make(map[string]*gitlab.Project, len(projects))
	for _, project := range projects {
		if project != nil {
			byPath[project.PathWithNamespace] = project
		}
	}

	pathQuery := &clonePathQuery{
		spec:     j.spec,
		projects: projects,
		opts:     j.opts,
		byPath:   byPath,
	}

	pick, err := j.wantedClonePaths(pathQuery)
	if err != nil {
		return nil, err
	}

	if err = j.rejectCloneHazards(j.base, pick.paths, inventory); err != nil {
		return nil, err
	}

	pick.items = make([]*cloneItem, 0, len(pick.paths))
	for _, path := range pick.paths {
		choice := &cloneChoiceQuery{
			spec:      j.spec,
			project:   byPath[path],
			path:      path,
			inventory: inventory,
		}
		pick.items = append(pick.items, j.cloneChoice(choice))
	}

	return pick, nil
}

// wantedClonePaths returns the exact projects this invocation will touch.
func (j *cloneJob) wantedClonePaths(query *clonePathQuery) (*clonePick, error) {
	if query == nil || query.opts == nil {
		return nil, errCloneConflict
	}

	spec := query.spec
	projects := query.projects
	opts := query.opts
	byPath := query.byPath

	if !opts.All {
		return j.explicitClonePaths(spec, opts.Repos, byPath)
	}

	paths := make([]string, 0, len(projects))
	skipped := 0

	for _, project := range projects {
		if project == nil || !canonicalProjectPath(project.PathWithNamespace) {
			return nil, errCloneConflict
		}

		if project.DefaultBranch == "" {
			skipped++

			continue
		}

		accepted, acceptErr := j.acceptArchivedClone(project, opts.IncludeArchived, false)
		if acceptErr != nil {
			return nil, acceptErr
		}

		if !accepted {
			skipped++

			continue
		}

		if !j.cloneListed(spec, project.PathWithNamespace) && !spec.hasDefault() {
			return nil, errCloneAlign
		}

		paths = append(paths, project.PathWithNamespace)
	}

	slices.Sort(paths)
	pick := &clonePick{
		paths:   paths,
		skipped: skipped,
	}

	return pick, nil
}

// explicitClonePaths checks each --repo before any directory is created.
func (j *cloneJob) explicitClonePaths(
	spec *WorkspaceSpec,
	repos []string,
	byPath map[string]*gitlab.Project,
) (*clonePick, error) {
	seen := make(map[string]struct{}, len(repos))
	paths := make([]string, 0, len(repos))

	for _, path := range repos {
		if _, found := seen[path]; found {
			continue
		}

		seen[path] = struct{}{}
		project := byPath[path]

		if project == nil {
			return nil, fmt.Errorf("%w: %s", errCloneUnknown, path)
		}

		include := j.opts != nil && j.opts.IncludeArchived
		if _, acceptErr := j.acceptArchivedClone(project, include, true); acceptErr != nil {
			return nil, acceptErr
		}

		if project.DefaultBranch == "" {
			return nil, fmt.Errorf("%w: %s", errCloneBranch, path)
		}

		if !j.cloneListed(spec, path) && !spec.hasDefault() {
			return nil, fmt.Errorf("%w: %s", errCloneAlign, path)
		}

		paths = append(paths, path)
	}

	pick := &clonePick{
		paths: paths,
	}

	return pick, nil
}

// rejectCloneHazards stops before clone when a target is unsafe or ambiguous.
func (j *cloneJob) rejectCloneHazards(base string, paths []string, inventory *RemoteInventory) error {
	if inventory == nil || inventory.Catalog == nil {
		return errRemoteInventory
	}

	blocked := append([]string{}, inventory.Catalog.Conflicts...)
	blocked = append(blocked, inventory.Catalog.DifferentPath...)
	block := make(map[string]struct{}, len(blocked))

	for _, path := range blocked {
		block[path] = struct{}{}
	}

	for _, path := range paths {
		_, blockedPath := block[path]
		if blockedPath || !canonicalProjectPath(path) {
			return fmt.Errorf("%w: %s", errCloneConflict, path)
		}
	}

	query := &remoteDiffQuery{
		base: base,
	}
	diff := newRemoteDiff(query)

	folding, err := diff.caseFolding()
	if err != nil {
		return fmt.Errorf("%w: case check failed: %w", errCloneConflict, err)
	}

	if folding && j.foldedPair(paths) {
		return errCloneConflict
	}

	return nil
}

// cloneChoice records whether an existing matching checkout can be kept.
func (j *cloneJob) cloneChoice(query *cloneChoiceQuery) *cloneItem {
	spec := query.spec
	project := query.project
	path := query.path
	inventory := query.inventory
	reuse := j.checkoutPresent(inventory, path)
	item := &cloneItem{
		project: project,
		path:    path,
		reuse:   reuse,
		listed:  j.cloneListed(spec, path),
	}

	return item
}

// checkoutPresent reports a catalog path that is already on disk.
// Active and archived gaps are separate lists; absence from only one of them is not a checkout.
func (*cloneJob) checkoutPresent(inventory *RemoteInventory, path string) bool {
	if inventory == nil || inventory.Catalog == nil {
		return false
	}

	catalog := inventory.Catalog

	return !slices.Contains(catalog.NotCloned, path) && !slices.Contains(catalog.NotClonedArchived, path)
}

// cloneListed reports a project already stored in the workspace.
func (j *cloneJob) cloneListed(spec *WorkspaceSpec, path string) bool {
	if spec == nil {
		return false
	}

	for _, project := range spec.Projects {
		if project != nil && project.Path == path {
			return true
		}
	}

	return false
}

// foldedPair reports two different paths that compare equal ignoring case.
func (j *cloneJob) foldedPair(paths []string) bool {
	for left := range paths {
		for right := left + 1; right < len(paths); right++ {
			if paths[left] != paths[right] && strings.EqualFold(paths[left], paths[right]) {
				return true
			}
		}
	}

	return false
}

// cloneAll probes transport once, then clones missing checkouts in path order.
func (j *cloneJob) cloneAll(ctx context.Context, items []*cloneItem) error {
	j.client = offlineGit(j.cfg)
	j.client.NoLazyFetch = false

	if err := j.probe(ctx, items); err != nil {
		return err
	}

	phase := logger.NewProgress("clone", len(items))
	phase.Start(ctx)

	for _, item := range items {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}

		if item.reuse {
			j.report.Reused++
			j.report.Paths = append(j.report.Paths, item.path)
			phase.Advance(ctx, item.path, "reused")

			continue
		}

		logger.InfoKV(ctx, "Cloning repository", "repo", item.path)

		err := j.one(ctx, item)
		if err != nil {
			j.report.Failed++

			if ctx.Err() != nil {
				phase.Advance(ctx, item.path, "clone interrupted")

				return context.Cause(ctx)
			}

			phase.Error(ctx, item.path, "clone failed")

			return fmt.Errorf("%w: %s: %w", errClonePartial, item.path, err)
		}

		j.report.Cloned++
		j.report.Paths = append(j.report.Paths, item.path)
		phase.Advance(ctx, item.path, "cloned")
	}

	return nil
}

// probe checks one clone URL at most Attempts times for the whole batch.
func (j *cloneJob) probe(ctx context.Context, items []*cloneItem) error {
	remote := ""

	for _, item := range items {
		if item.reuse {
			continue
		}

		raw, err := j.chooseCloneURL(item.project, j.spec.GitLab.CloneProtocol, j.opts.hooks)
		if err != nil {
			return err
		}

		remote = raw

		break
	}

	if remote == "" {
		return nil
	}

	logger.Info(ctx, "Checking clone transport")

	return j.probeCloneURL(ctx, j.client, j.opts, remote)
}

// probeCloneURL retries a transport check without starting a clone.
func (j *cloneJob) probeCloneURL(
	ctx context.Context,
	client *gitter.Client,
	opts *WorkspaceCloneOptions,
	remote string,
) error {
	if opts.Attempts <= 1 {
		return client.ProbeURL(ctx, remote, opts.ProbeTimeout)
	}

	op := func(ctx context.Context) error {
		err := client.ProbeURL(ctx, remote, opts.ProbeTimeout)
		if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
			timeout := &probeTimeoutError{
				cause: err,
			}

			return timeout
		}

		return err
	}
	engine := &retry.EngineConfig{
		MaxRetries:  uint64(opts.Attempts - 1),
		DelayPolicy: retry.NewRandomRangePolicy(opts.RetryDelay, opts.RetryDelay),
		IsRetryable: retryableCloneProbe,
	}
	onRetry := retry.WithOnRetry(func(ctx context.Context, info *retry.AttemptInfo) {
		logger.Warnf(ctx, "Clone transport check failed; next attempt in %s: %v", info.Delay, info.Err)
	})
	err := retry.Do(ctx, engine, op, onRetry)

	if timeout, ok := errors.AsType[*probeTimeoutError](err); ok {
		return timeout.cause
	}

	return err
}

// retryableCloneProbe repeats a timeout or a dropped connection.
func retryableCloneProbe(err error) bool {
	var timeout *probeTimeoutError

	return errors.As(err, &timeout) || gitter.NetworkError(err)
}

// acceptArchivedClone allows an archived project only when the flag asks for it.
// --all skips the rest. An explicit --repo returns a usage error.
func (*cloneJob) acceptArchivedClone(project *gitlab.Project, include, explicit bool) (bool, error) {
	if project == nil {
		return false, errCloneUnknown
	}

	if !project.Archived || include {
		return true, nil
	}

	if explicit {
		return false, fmt.Errorf("%w: %s", errArchivedOptIn, project.PathWithNamespace)
	}

	return false, nil
}

// chooseCloneURL returns the protocol URL, or a test filesystem path.
func (j *cloneJob) chooseCloneURL(project *gitlab.Project, protocol string, hooks *remoteHooks) (string, error) {
	raw := project.SSHURLToRepo
	if protocol == cloneProtocolHTTPS {
		raw = project.HTTPURLToRepo
	}

	allow := hooks != nil && hooks.allowLocalClone
	if allow && j.localCloneURL(raw) {
		return raw, nil
	}

	if !j.productionCloneURL(raw, protocol) {
		return "", fmt.Errorf("%w: %s", errCloneURL, project.PathWithNamespace)
	}

	return raw, nil
}
