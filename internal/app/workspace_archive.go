package app

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/oshokin/release-align/internal/gitter"
)

// WorkspaceArchiveOptions selects repositories and the destination ZIP.
type WorkspaceArchiveOptions struct {
	// WorkspaceFile is the inventory JSON.
	WorkspaceFile string
	// BaseDir is the directory that contains the clones.
	BaseDir string
	// File is the destination ZIP. An existing file is left unchanged.
	File string
	// Repositories are exact project paths. Empty means no path filter.
	Repositories []string
	// Groups are workspace groups. Empty means no group filter.
	Groups []string
	// Output is text or json.
	Output string
	// ArchiveTimeout limits git archive and the ZIP copy for one repository.
	ArchiveTimeout time.Duration
	// LocalTimeout limits rev-parse and ls-tree.
	LocalTimeout time.Duration
	// Progress receives short status lines. Nil discards them.
	Progress io.Writer
	// timeoutLocks records durations already chosen by a flag or the environment.
	timeoutLocks *TimeoutLocks
}

// ArchiveReport is the command result. It is not the ZIP manifest.
type ArchiveReport struct {
	// SchemaVersion is the report version.
	SchemaVersion int `json:"schema_version"`
	// File is the published ZIP path. It is empty when nothing was published.
	File string `json:"file,omitempty"`
	// Release is the workspace label.
	Release string `json:"release,omitempty"`
	// Repositories is the number of packed repositories.
	Repositories int `json:"repositories"`
	// Gitlinks is the number of recorded submodule commits.
	Gitlinks int `json:"gitlinks"`
	// Source describes which revisions were packed.
	Source string `json:"source,omitempty"`
	// Error explains a failure. An empty value means success.
	Error string `json:"error,omitempty"`
}

// archiveManifest is the document stored at _release-align/manifest.json.
type archiveManifest struct {
	// SchemaVersion is the manifest version.
	SchemaVersion int `json:"schema_version"`
	// Release is the workspace label.
	Release string `json:"release,omitempty"`
	// Repositories lists each packed path and the commit that was exported.
	Repositories []*archiveManifestRepo `json:"repositories"`
}

// archiveManifestRepo is one exported repository.
type archiveManifestRepo struct {
	// Path is the workspace project path.
	Path string `json:"path"`
	// Requested is the branch, tag, or commit named by the workspace.
	Requested string `json:"requested"`
	// Commit is the full object id that was exported.
	Commit string `json:"commit"`
	// Gitlinks are submodule commits found in that tree. Their trees are not in the ZIP.
	Gitlinks []*archiveGitlink `json:"gitlinks,omitempty"`
}

// archiveGitlink is a gitlink (mode 160000) recorded from the exported tree.
type archiveGitlink struct {
	// Path is the gitlink path inside the repository.
	Path string `json:"path"`
	// Commit is the recorded submodule commit.
	Commit string `json:"commit"`
}

// plannedRepo is one repository after its revision has been resolved.
type plannedRepo struct {
	// Path is the workspace project path.
	Path string
	// Requested is the branch, tag, or commit named by the workspace.
	Requested string
	// Commit is the full object id that will be exported.
	Commit string
	// Dir is the local clone. It is not written to the manifest.
	Dir string
	// Gitlinks are submodule commits in that tree.
	Gitlinks []*archiveGitlink
}

// archiveJob packs one workspace selection.
type archiveJob struct {
	// ctx is the command context.
	ctx context.Context
	// opts are the caller's options.
	opts *WorkspaceArchiveOptions
	// spec is the loaded workspace.
	spec *WorkspaceSpec
	// git runs local Git with lazy fetch suppressed on a best-effort basis.
	git *gitter.Client
	// base is the absolute base directory.
	base string
	// destination is the absolute ZIP path. Git receives this path, not the caller's relative name.
	destination string
}

const (
	// archiveSchemaVersion is the version of the archive report and manifest.
	archiveSchemaVersion = 1
	// archiveManifestDir is the directory inside the ZIP that holds manifest.json.
	archiveManifestDir = "_release-align"
	// archiveSource is printed with a successful text report.
	archiveSource = "Source: workspace revisions, locally cached refs. Uncommitted changes are not included."
	// archiveCopyChunk is the most raw ZIP data copied before the next cancel check.
	archiveCopyChunk = 32 << 10
)

// SetTimeoutLocks records durations already chosen by a flag or the environment.
func (o *WorkspaceArchiveOptions) SetTimeoutLocks(locks *TimeoutLocks) {
	if o == nil {
		return
	}

	o.timeoutLocks = locks
}

// SetProgress records where archive progress is written.
func (o *WorkspaceArchiveOptions) SetProgress(w io.Writer) {
	if o == nil {
		return
	}

	o.Progress = w
}

// ArchiveWorkspace packs workspace revisions into one ZIP.
// It does not fetch, clone, or check out. A failure publishes nothing.
func ArchiveWorkspace(ctx context.Context, opts *WorkspaceArchiveOptions) (*ArchiveReport, error) {
	report := &ArchiveReport{
		SchemaVersion: archiveSchemaVersion,
	}

	job, err := newArchiveJob(ctx, opts)
	if err != nil {
		return archiveFailed(report, err)
	}

	unlock, err := lockBase(job.base)
	if err != nil {
		return archiveFailed(report, err)
	}

	defer unlock()

	planned, err := job.plan()
	if err != nil {
		return archiveFailed(report, err)
	}

	if err = job.write(planned); err != nil {
		return archiveFailed(report, err)
	}

	report.File = opts.File
	report.Release = job.spec.Release
	report.Repositories = len(planned)
	report.Source = archiveSource
	report.Gitlinks = countGitlinks(planned)

	return report, nil
}

// newArchiveJob loads the workspace and resolves the selection before any packing.
func newArchiveJob(ctx context.Context, opts *WorkspaceArchiveOptions) (*archiveJob, error) {
	if err := validateArchiveOptions(opts); err != nil {
		return nil, err
	}

	spec, err := LoadWorkspace(opts.WorkspaceFile)
	if err != nil {
		return nil, err
	}

	if err = applyArchiveTimeouts(opts, spec); err != nil {
		return nil, err
	}

	base, err := filepath.Abs(opts.BaseDir)
	if err != nil {
		return nil, err
	}

	destination, err := filepath.Abs(opts.File)
	if err != nil {
		return nil, err
	}

	gitClient := &gitter.Client{
		LocalTimeout: opts.LocalTimeout,
		NoLazyFetch:  true,
	}

	selected, err := spec.SelectProjects(opts.Repositories, opts.Groups)
	if err != nil {
		return nil, err
	}

	if len(selected) == 0 {
		return nil, errArchiveEmpty
	}

	lookup := &revisionLookup{
		git:      gitClient,
		base:     base,
		spec:     spec,
		projects: selected,
	}
	if err = ResolveWorkspaceRevisions(ctx, lookup); err != nil {
		return nil, err
	}

	if err = rejectReservedPaths(selected); err != nil {
		return nil, err
	}

	job := &archiveJob{
		ctx:         ctx,
		opts:        opts,
		spec:        spec,
		base:        base,
		destination: destination,
		git:         gitClient,
	}

	return job, nil
}

// validateArchiveOptions checks the destination and the time limits.
func validateArchiveOptions(opts *WorkspaceArchiveOptions) error {
	if opts == nil || opts.WorkspaceFile == "" {
		return errWorkspaceRequired
	}

	if opts.BaseDir == "" {
		return errBaseDirEmpty
	}

	if opts.File == "" {
		return errArchiveFile
	}

	if opts.Output != "" && opts.Output != outputText && opts.Output != outputJSON {
		return errWorkspaceOutput
	}

	if opts.ArchiveTimeout <= 0 || opts.LocalTimeout <= 0 {
		return errTimeoutRange
	}

	return nil
}

// rejectReservedPaths refuses a project path that would cover the manifest.
func rejectReservedPaths(projects []*ProjectSpec) error {
	for _, project := range projects {
		if project == nil {
			continue
		}

		if project.Path == archiveManifestDir || strings.HasPrefix(project.Path, archiveManifestDir+"/") {
			return fmt.Errorf("%w: %s", errArchiveReserved, project.Path)
		}
	}

	return nil
}

// archiveFailed records a redacted error and returns it with the report.
func archiveFailed(report *ArchiveReport, err error) (*ArchiveReport, error) {
	if report != nil && err != nil {
		report.Error = redactGitText(err.Error())
	}

	return report, err
}

// countGitlinks sums submodule commits across the planned repositories.
func countGitlinks(planned []*plannedRepo) int {
	total := 0

	for _, repo := range planned {
		total += len(repo.Gitlinks)
	}

	return total
}

// archivePhase wraps a packing failure with the repository and the step that stopped.
func archivePhase(path, phase string, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%w: %s: %s: %w", errArchiveFailed, path, phase, err)
}
