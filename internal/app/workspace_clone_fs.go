package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// cloneAdditionQuery is the set of finished clones that may be appended.
type cloneAdditionQuery struct {
	// items are the projects this invocation considered.
	items []*cloneItem
	// report lists paths that finished cloning.
	report *CloneReport
	// protocol is ssh or https.
	protocol string
	// hooks supply tests with a clone URL.
	hooks *remoteHooks
	// ctx bounds the local rev-parse used to pin a new archived clone.
	ctx context.Context
}

// archivedPinChoice is the path, catalog branch, and reuse bit for one archived pin.
type archivedPinChoice struct {
	// path is the workspace path.
	path string
	// branch is the catalog default branch.
	branch string
	// reuse reports that the checkout is already on disk.
	reuse bool
}

// publishClone appends completed new paths. It does not delete clones when the write fails.
func (j *cloneJob) publish(ctx context.Context, items []*cloneItem) (*CloneReport, error) {
	query := &cloneAdditionQuery{
		items:    items,
		report:   j.report,
		protocol: j.spec.GitLab.CloneProtocol,
		hooks:    j.opts.hooks,
		ctx:      ctx,
	}

	selected, err := j.cloneAdditions(query)
	if err != nil {
		return j.publishCloneError(j.opts, j.report, err)
	}

	marks := j.archivedMarks(items)
	j.report.Next = j.nextSyncCommand(j.opts)

	if ctx.Err() != nil {
		return j.report, context.Cause(ctx)
	}

	if len(selected) == 0 && len(marks) == 0 {
		return j.report, nil
	}

	appended, err := AppendDiscoveredProjects(j.spec, selected)
	if err != nil {
		return j.publishCloneError(j.opts, j.report, err)
	}

	next := appended.spec
	added := appended.added

	if len(added) == 0 && len(marks) == 0 {
		return j.report, nil
	}

	if len(marks) > 0 {
		if _, err = writeArchiveMarks(j.document.root, marks); err != nil {
			return j.publishCloneError(j.opts, j.report, err)
		}
	}

	if err = publishWorkspace(ctx, j.document, next, nil); err != nil {
		return j.publishCloneError(j.opts, j.report, err)
	}

	j.report.Added = len(added)

	return j.report, nil
}

// cloneAdditions builds workspace rows for completed projects that are not listed yet.
func (j *cloneJob) cloneAdditions(query *cloneAdditionQuery) ([]*ProjectSpec, error) {
	if query == nil || query.report == nil {
		return nil, errClonePublish
	}

	items := query.items
	report := query.report
	protocol := query.protocol
	hooks := query.hooks

	done := make(map[string]struct{}, len(report.Paths))
	for _, path := range report.Paths {
		done[path] = struct{}{}
	}

	selected := make([]*ProjectSpec, 0)

	for _, item := range items {
		if item == nil || item.listed {
			continue
		}

		if _, finished := done[item.path]; !finished {
			continue
		}

		remote, err := j.chooseCloneURL(item.project, protocol, hooks)
		if err != nil {
			return nil, err
		}

		project := &ProjectSpec{
			Path:     item.path,
			URL:      remote,
			Groups:   directoryGroups(item.path),
			Archived: item.project != nil && item.project.Archived,
		}

		if project.Archived {
			dir := filepath.Join(j.base, filepath.FromSlash(item.path))

			oid, oidErr := j.archivedPin(query.ctx, item, dir)
			if oidErr != nil {
				return nil, oidErr
			}

			project.Revision = &RevisionSpec{
				Commit: strings.TrimSpace(oid),
			}
		}

		selected = append(selected, project)
	}

	return selected, nil
}

// archivedMarks records listed projects whose successful clone must store archived=true.
func (j *cloneJob) archivedMarks(items []*cloneItem) map[string]bool {
	done := map[string]struct{}{}

	if j.report != nil {
		for _, path := range j.report.Paths {
			done[path] = struct{}{}
		}
	}

	marks := map[string]bool{}

	for _, item := range items {
		if item == nil || !item.listed || item.project == nil || !item.project.Archived {
			continue
		}

		if _, finished := done[item.path]; !finished {
			continue
		}

		marks[item.path] = true
	}

	return marks
}

// archivedPin is the commit stored for a new archived row.
// Both a fresh clone and a reused checkout must match origin's catalog default branch.
// A reused checkout is not switched and is not fetched.
func (j *cloneJob) archivedPin(ctx context.Context, item *cloneItem, dir string) (string, error) {
	if j.client != nil {
		j.client.NoLazyFetch = true
	}

	choice := j.pinChoice(item)
	if choice.branch == "" {
		return "", fmt.Errorf("%w: %s", errCloneBranch, choice.path)
	}

	if _, err := j.client.Local(ctx, dir, "check-ref-format", "refs/heads/"+choice.branch); err != nil {
		return "", fmt.Errorf("%w: %s", errCloneBranch, choice.path)
	}

	origin, err := j.client.Local(
		ctx,
		dir,
		"rev-parse",
		"--verify",
		"refs/remotes/origin/"+choice.branch+"^{commit}",
	)
	if err != nil {
		return "", err
	}

	origin = strings.TrimSpace(origin)
	if !workspaceOID.MatchString(origin) {
		return "", fmt.Errorf("%w: %s", errArchivedHead, choice.path)
	}

	if choice.reuse {
		return origin, nil
	}

	head, err := j.client.Local(ctx, dir, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", err
	}

	if strings.TrimSpace(head) != origin {
		return "", fmt.Errorf("%w: %s", errArchivedHead, choice.path)
	}

	return origin, nil
}

// pinChoice reads the path, catalog branch, and reuse bit for one pin.
func (*cloneJob) pinChoice(item *cloneItem) *archivedPinChoice {
	choice := &archivedPinChoice{}
	if item == nil {
		return choice
	}

	choice.path = item.path
	choice.reuse = item.reuse

	if item.project != nil {
		choice.branch = item.project.DefaultBranch
	}

	return choice
}

// publishCloneError keeps the downloaded directories and names the recovery command.
func (j *cloneJob) publishCloneError(
	opts *WorkspaceCloneOptions,
	report *CloneReport,
	err error,
) (*CloneReport, error) {
	report.Recovery = j.recoveryCommand(opts, report.Paths)
	report.Error = err.Error()

	return report, fmt.Errorf("%w: %s: %s", errClonePublish, err.Error(), report.Recovery)
}

// cloneOne clones into a sibling staging directory and renames it onto the expected path.
func (j *cloneJob) one(ctx context.Context, item *cloneItem) error {
	target, err := j.containedTarget(j.base, item.path)
	if err != nil {
		return err
	}

	if _, statErr := os.Lstat(target); !errors.Is(statErr, os.ErrNotExist) {
		return errCloneConflict
	}

	remote, err := j.chooseCloneURL(item.project, j.spec.GitLab.CloneProtocol, j.opts.hooks)
	if err != nil {
		return err
	}

	parent := filepath.Dir(target)
	if err = os.MkdirAll(parent, 0o750); err != nil {
		return err
	}

	staging, err := os.MkdirTemp(parent, ".release-align-clone-")
	if err != nil {
		return err
	}

	cloneErr := j.client.Clone(ctx, remote, staging, j.opts.CloneTimeout)
	if cloneErr != nil || ctx.Err() != nil {
		_ = os.RemoveAll(staging)

		if ctx.Err() != nil {
			return context.Cause(ctx)
		}

		return cloneErr
	}

	if _, statErr := os.Lstat(target); !errors.Is(statErr, os.ErrNotExist) {
		_ = os.RemoveAll(staging)

		return errCloneConflict
	}

	if err = os.Rename(staging, target); err != nil {
		_ = os.RemoveAll(staging)

		return err
	}

	return nil
}

// containedTarget joins a namespace under base and rejects symlink escapes.
func (j *cloneJob) containedTarget(base, relative string) (string, error) {
	if !canonicalProjectPath(relative) {
		return "", errWorkspacePath
	}

	root, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}

	current := root
	parts := strings.Split(relative, "/")

	for index, part := range parts {
		next := filepath.Join(current, part)
		info, statErr := os.Lstat(next)

		if errors.Is(statErr, os.ErrNotExist) {
			rest := filepath.Join(parts[index:]...)

			return filepath.Join(current, rest), nil
		}

		if statErr != nil {
			return "", statErr
		}

		if info.Mode()&os.ModeSymlink != 0 {
			return "", errWorkspacePathKind
		}

		current = next
	}

	rel, err := filepath.Rel(root, current)
	if err != nil || rel == pathDotDot || strings.HasPrefix(rel, pathDotDot+string(filepath.Separator)) {
		return "", errWorkspacePath
	}

	return current, nil
}

// nextSyncCommand is the alignment command after a clone.
func (j *cloneJob) nextSyncCommand(opts *WorkspaceCloneOptions) string {
	return "release-align workspace sync --workspace " + shellArg(opts.WorkspaceFile) +
		" --base-dir " + shellArg(opts.BaseDir) + " --remote"
}

// recoveryCommand repeats clone so a later run reuses finished checkouts.
func (j *cloneJob) recoveryCommand(opts *WorkspaceCloneOptions, paths []string) string {
	var command strings.Builder

	_, _ = command.WriteString("release-align workspace clone --workspace " + shellArg(opts.WorkspaceFile))
	_, _ = command.WriteString(" --base-dir " + shellArg(opts.BaseDir))

	for _, path := range paths {
		_, _ = command.WriteString(" --repo " + shellArg(path))
	}

	if opts.IncludeArchived {
		_, _ = command.WriteString(" --include-archived")

		return command.String()
	}

	_, _ = command.WriteString("\nOr: release-align workspace refresh --base-dir " + shellArg(opts.BaseDir))
	_, _ = command.WriteString(" --file " + shellArg(opts.WorkspaceFile))

	for _, path := range paths {
		_, _ = command.WriteString(" --add " + shellArg(path))
	}

	return command.String()
}
