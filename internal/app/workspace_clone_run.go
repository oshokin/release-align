package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/oshokin/release-align/internal/gitlab"
	"github.com/oshokin/release-align/internal/gitter"
	"github.com/oshokin/release-align/internal/retry"
)

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

	pick, err := wantedClonePaths(j.spec, projects, j.opts, byPath)
	if err != nil {
		return nil, err
	}

	if err = rejectCloneHazards(j.base, pick.paths, inventory); err != nil {
		return nil, err
	}

	pick.items = make([]*cloneItem, 0, len(pick.paths))
	for _, path := range pick.paths {
		pick.items = append(pick.items, cloneChoice(j.spec, byPath[path], path, inventory))
	}

	return pick, nil
}

// wantedClonePaths returns the exact projects this invocation will touch.
func wantedClonePaths(
	spec *WorkspaceSpec,
	projects []*gitlab.Project,
	opts *WorkspaceCloneOptions,
	byPath map[string]*gitlab.Project,
) (*clonePick, error) {
	if !opts.All {
		return explicitClonePaths(spec, opts.Repos, byPath)
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

		if !cloneListed(spec, project.PathWithNamespace) && !spec.hasDefault() {
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
func explicitClonePaths(spec *WorkspaceSpec, repos []string, byPath map[string]*gitlab.Project) (*clonePick, error) {
	seen := make(map[string]bool, len(repos))
	paths := make([]string, 0, len(repos))

	for _, path := range repos {
		if seen[path] {
			continue
		}

		seen[path] = true
		project := byPath[path]

		if project == nil {
			return nil, fmt.Errorf("%w: %s", errCloneUnknown, path)
		}

		if project.DefaultBranch == "" {
			return nil, fmt.Errorf("%w: %s", errCloneBranch, path)
		}

		if !cloneListed(spec, path) && !spec.hasDefault() {
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
func rejectCloneHazards(base string, paths []string, inventory *RemoteInventory) error {
	if inventory == nil || inventory.Catalog == nil {
		return errRemoteInventory
	}

	blocked := append([]string{}, inventory.Catalog.Conflicts...)
	blocked = append(blocked, inventory.Catalog.DifferentPath...)
	block := make(map[string]bool, len(blocked))

	for _, path := range blocked {
		block[path] = true
	}

	for _, path := range paths {
		if block[path] || !canonicalProjectPath(path) {
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

	if folding && foldedPair(paths) {
		return errCloneConflict
	}

	return nil
}

// cloneChoice records whether an existing matching checkout can be kept.
func cloneChoice(spec *WorkspaceSpec, project *gitlab.Project, path string, inventory *RemoteInventory) *cloneItem {
	reuse := !slices.Contains(inventory.Catalog.NotCloned, path)
	item := &cloneItem{
		project: project,
		path:    path,
		reuse:   reuse,
		listed:  cloneListed(spec, path),
	}

	return item
}

// cloneListed reports a project already stored in the workspace.
func cloneListed(spec *WorkspaceSpec, path string) bool {
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
func foldedPair(paths []string) bool {
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

	for _, item := range items {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}

		if item.reuse {
			j.report.Reused++
			j.report.Paths = append(j.report.Paths, item.path)

			continue
		}

		if err := j.one(ctx, item); err != nil {
			j.report.Failed++

			if ctx.Err() != nil {
				return context.Cause(ctx)
			}

			return fmt.Errorf("%w: %s: %w", errClonePartial, item.path, err)
		}

		j.report.Cloned++
		j.report.Paths = append(j.report.Paths, item.path)
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

		raw, err := chooseCloneURL(item.project, j.spec.GitLab.CloneProtocol, j.opts.hooks)
		if err != nil {
			return err
		}

		remote = raw

		break
	}

	if remote == "" {
		return nil
	}

	return probeCloneURL(ctx, j.client, j.opts, remote)
}

// probeCloneURL retries a transport check without starting a clone.
func probeCloneURL(ctx context.Context, client *gitter.Client, opts *WorkspaceCloneOptions, remote string) error {
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
	err := retry.Do(ctx, engine, op)

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

// chooseCloneURL returns the protocol URL, or a test filesystem path.
func chooseCloneURL(project *gitlab.Project, protocol string, hooks *remoteHooks) (string, error) {
	raw := project.SSHURLToRepo
	if protocol == cloneProtocolHTTPS {
		raw = project.HTTPURLToRepo
	}

	allow := hooks != nil && hooks.allowLocalClone
	if allow && localCloneURL(raw) {
		return raw, nil
	}

	if !productionCloneURL(raw, protocol) {
		return "", fmt.Errorf("%w: %s", errCloneURL, project.PathWithNamespace)
	}

	return raw, nil
}
