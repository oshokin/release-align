package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/oshokin/release-align/internal/gitlab"
	"github.com/oshokin/release-align/internal/gitter"
)

// WorkspaceCloneOptions selects projects to download and register.
type WorkspaceCloneOptions struct {
	// BaseDir is the root that will contain path_with_namespace directories.
	BaseDir string
	// WorkspaceFile is the inventory to update after a successful selection.
	WorkspaceFile string
	// Repos are exact path_with_namespace values. Empty when All is set.
	Repos []string
	// All selects every project in the saved GitLab scope.
	All bool
	// Attempts is the total number of transport probes, not extra retries.
	Attempts int
	// ProbeTimeout limits one git ls-remote attempt.
	ProbeTimeout time.Duration
	// RetryDelay is the pause between probe attempts.
	RetryDelay time.Duration
	// FetchTimeout is the budget for the whole GitLab catalog.
	FetchTimeout time.Duration
	// LocalTimeout limits Git commands that do not talk to a remote.
	LocalTimeout time.Duration
	// CloneTimeout limits one git clone.
	CloneTimeout time.Duration
	// Output is text or json.
	Output string
	// hooks replaces the token and HTTP client in tests.
	hooks *remoteHooks
	// progress receives lines that must stay off JSON stdout.
	progress io.Writer
	// timeoutLocks records durations already chosen by a flag or the environment.
	timeoutLocks *TimeoutLocks
}

// CloneReport is the single JSON document for workspace clone.
type CloneReport struct {
	// SchemaVersion is the clone document version.
	SchemaVersion int `json:"schema_version"`
	// Cloned is the number of new directories created by this run.
	Cloned int `json:"cloned"`
	// Reused is the number of existing matching checkouts that were kept.
	Reused int `json:"reused"`
	// Added is the number of workspace rows appended.
	Added int `json:"added"`
	// SkippedEmpty is the number of projects skipped because they have no default branch.
	SkippedEmpty int `json:"skipped_empty"`
	// Failed is the number of clone attempts that stopped the batch.
	Failed int `json:"failed"`
	// Paths are the selected projects that finished, in path order.
	Paths []string `json:"paths,omitempty"`
	// Recovery is the command to run when the workspace file could not be published.
	Recovery string `json:"recovery,omitempty"`
	// Next is the suggested alignment command. It is not executed.
	Next string `json:"next,omitempty"`
	// Error is the failure text when publication or a clone did not finish.
	Error string `json:"error,omitempty"`
}

// cloneItem is one selected project during a clone run.
type cloneItem struct {
	// project is the GitLab record used to choose the clone URL.
	project *gitlab.Project
	// path is the workspace path and the directory under the base.
	path string
	// reuse reports that a matching checkout already exists.
	reuse bool
	// listed reports that the path is already in the workspace file.
	listed bool
}

// cloneLocations are the canonical base directory and workspace file.
type cloneLocations struct {
	// base is the cleaned base directory.
	base string
	// file is the cleaned workspace path.
	file string
}

// cloneHold is the pair of locks and the reread workspace document.
type cloneHold struct {
	// unlockBase releases the base-directory lock.
	unlockBase func()
	// unlockFile releases the workspace-file lock.
	unlockFile func()
	// document is the workspace file read after both locks were taken.
	document *workspaceDocument
}

// cloneJob is one clone invocation after the locks are held.
type cloneJob struct {
	// opts are the flags for this invocation.
	opts *WorkspaceCloneOptions
	// cfg carries timeouts into the shared GitLab helper.
	cfg *Config
	// base is the canonical base directory.
	base string
	// document is the locked workspace file.
	document *workspaceDocument
	// spec is the validated inventory from document.
	spec *WorkspaceSpec
	// report is the document written to stdout.
	report *CloneReport
	// client runs git clone and the transport probe.
	client *gitter.Client
}

// clonePick is the selection made before the first clone starts.
type clonePick struct {
	// items are the projects that will be cloned or reused.
	items []*cloneItem
	// paths are the selected path_with_namespace values.
	paths []string
	// skipped counts projects left out because they have no default branch.
	skipped int
}

// SetTimeoutLocks records durations already chosen by a flag or the environment.
func (o *WorkspaceCloneOptions) SetTimeoutLocks(locks *TimeoutLocks) {
	if o == nil {
		return
	}

	o.timeoutLocks = locks
}

// SetProgress records where catalog progress is written.
func (o *WorkspaceCloneOptions) SetProgress(w io.Writer) {
	if o == nil {
		return
	}

	o.progress = w
}

// CloneWorkspace downloads missing clones and registers selected projects.
// A canceled run does not publish the workspace file. Completed directories are kept.
func CloneWorkspace(ctx context.Context, opts *WorkspaceCloneOptions) (*CloneReport, error) {
	report := &CloneReport{
		SchemaVersion: 1,
		Paths:         []string{},
	}
	if err := validateCloneOptions(opts); err != nil {
		return report, err
	}

	locations, err := clonePaths(opts)
	if err != nil {
		return report, err
	}

	hold, err := lockClone(locations.base, locations.file)
	if err != nil {
		return report, err
	}

	defer hold.unlockBase()
	defer hold.unlockFile()

	job := &cloneJob{
		opts:     opts,
		base:     locations.base,
		document: hold.document,
		report:   report,
	}

	return job.locked(ctx)
}

// validateCloneOptions checks flags before any lock or network call.
func validateCloneOptions(opts *WorkspaceCloneOptions) error {
	if opts == nil || opts.BaseDir == "" || opts.WorkspaceFile == "" {
		return errCloneSelection
	}

	if opts.All == (len(opts.Repos) > 0) {
		return errCloneSelection
	}

	if opts.Attempts < 1 || opts.ProbeTimeout <= 0 || opts.FetchTimeout <= 0 || opts.CloneTimeout <= 0 {
		return errTimeoutRange
	}

	if opts.RetryDelay < 0 || opts.LocalTimeout <= 0 {
		return errTimeoutRange
	}

	if opts.Output != "" && opts.Output != outputText && opts.Output != outputJSON {
		return errWorkspaceOutput
	}

	return nil
}

// clonePaths resolves the base directory and the workspace file.
func clonePaths(opts *WorkspaceCloneOptions) (*cloneLocations, error) {
	base, err := canonicalWorkspaceBase(opts.BaseDir)
	if err != nil {
		return nil, err
	}

	path, err := canonicalWorkspaceFilename(opts.WorkspaceFile)
	if err != nil {
		return nil, err
	}

	locations := &cloneLocations{
		base: base,
		file: path,
	}

	return locations, nil
}

// lockClone takes the base lock and then the workspace lock, and rereads the file.
func lockClone(base, path string) (*cloneHold, error) {
	unlockBase, err := lockBase(base)
	if err != nil {
		return nil, err
	}

	unlockFile, err := lockWorkspaceFile(path)
	if err != nil {
		unlockBase()

		return nil, err
	}

	document, err := readWorkspaceDocument(path)
	if err != nil {
		unlockFile()
		unlockBase()

		return nil, err
	}

	hold := &cloneHold{
		unlockBase: unlockBase,
		unlockFile: unlockFile,
		document:   document,
	}

	return hold, nil
}

// locked lists GitLab, checks every selected path, then clones what is missing.
func (j *cloneJob) locked(ctx context.Context) (*CloneReport, error) {
	j.spec = j.document.spec
	resolver := &gitter.Client{
		LocalTimeout: j.opts.LocalTimeout,
		NoLazyFetch:  true,
	}

	if err := ResolveWorkspaceRevisions(ctx, resolver, j.base, j.spec); err != nil {
		return j.report, err
	}

	if err := applyCloneTimeouts(j.opts, j.spec); err != nil {
		return j.report, err
	}

	if err := prepareCloneSource(j.opts, j.spec); err != nil {
		return j.report, err
	}

	j.cfg = cloneConfig(j.opts, j.base)
	if j.opts.progress != nil {
		_, _ = fmt.Fprintln(j.opts.progress, "Checking GitLab inventory...")
	}

	projects, err := listRemoteProjects(ctx, j.cfg, j.spec.GitLab)
	if err != nil {
		return j.report, err
	}

	pick, err := j.choose(ctx, projects)
	if err != nil {
		return j.report, err
	}

	j.report.SkippedEmpty = pick.skipped
	err = j.cloneAll(ctx, pick.items)

	if ctx.Err() != nil {
		return j.report, context.Cause(ctx)
	}

	published, pubErr := j.publish(ctx, pick.items)
	if err == nil {
		return published, pubErr
	}

	if !errors.Is(err, errClonePartial) {
		return j.report, err
	}

	if pubErr != nil {
		return published, errors.Join(err, pubErr)
	}

	return published, err
}

// prepareCloneSource checks the saved GitLab block and the API token.
func prepareCloneSource(opts *WorkspaceCloneOptions, spec *WorkspaceSpec) error {
	if spec == nil || spec.GitLab == nil {
		return errGitLabSource
	}

	if err := spec.GitLab.Validate(); err != nil {
		return err
	}

	for _, project := range spec.Projects {
		if project != nil && project.CloneDepth != nil {
			return errCloneDepth
		}
	}

	cfg := &Config{
		remoteHooks: opts.hooks,
	}
	if gitlabToken(cfg) == "" {
		return errRemoteToken
	}

	return nil
}

// cloneConfig carries timeouts into the shared GitLab client helper.
func cloneConfig(opts *WorkspaceCloneOptions, base string) *Config {
	return &Config{
		BaseDir:       base,
		ProbeTimeout:  opts.ProbeTimeout,
		FetchTimeout:  opts.FetchTimeout,
		Attempts:      opts.Attempts,
		RetryDelay:    opts.RetryDelay,
		LocalTimeout:  opts.LocalTimeout,
		Remote:        true,
		remoteHooks:   opts.hooks,
		progress:      opts.progress,
		WorkspaceFile: opts.WorkspaceFile,
	}
}
