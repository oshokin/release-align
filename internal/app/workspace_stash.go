package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/oshokin/release-align/internal/logger"
)

// WorkspaceStashOptions selects the clones named by the workspace file.
type WorkspaceStashOptions struct {
	// BaseDir is the root that contains the clones.
	BaseDir string
}

// StashRecord is one stash this command created or found.
type StashRecord struct {
	// Path is the workspace path of the repository.
	Path string
	// OID is the stash commit. The command does not pop it.
	OID string
}

// stashOutcome is one repository visit.
type stashOutcome struct {
	// record is the path and oid. Nil when the worktree is clean.
	record *StashRecord
	// kind is created, kept, clean, or missing.
	kind string
}

// foundStash is the newest stash whose subject is the workspace message.
type foundStash struct {
	// oid is that stash commit.
	oid string
	// found reports that the message is already in the stash list.
	found bool
}

// stashLog is one progress line for a repository visit.
type stashLog struct {
	// phase counts the visit.
	phase *logger.Progress
	// path is the workspace path.
	path string
	// record is the path and oid when this visit has one.
	record *StashRecord
	// kind selects the line.
	kind string
}

// WorkspaceStashResult lists repositories that were stashed, already stashed, clean, or missing.
type WorkspaceStashResult struct {
	// Stashed are repositories that received a new stash in this run.
	Stashed []*StashRecord
	// Kept are repositories whose existing stash used the same message.
	Kept []*StashRecord
	// Clean is the number of worktrees that had nothing to save.
	Clean int
	// Missing are listed paths whose directories are absent.
	Missing []string
}

const (
	// stashMessage is the subject stored by workspace stash.
	// A later run treats a stash with this subject as already saved.
	stashMessage = "release-align"
	// stashCreated means this run pushed a new stash.
	stashCreated = "created"
	// stashKept means a stash with the same message was already present.
	stashKept = "kept"
	// stashClean means the worktree had nothing to save.
	stashClean = "clean"
	// stashMissing means the listed directory is not on disk.
	stashMissing = "missing"
	// stashPhase is the progress phase printed on each stash line.
	stashPhase = "stash"
)

// errWorkspaceStashFlags means the file, base directory, or Git client was omitted.
var errWorkspaceStashFlags = errors.New("workspace stash requires base-dir, file, and git")

// StashWorkspace runs git stash push -u in dirty listed repositories.
// A second run does not push again when a stash with the same message already exists.
// The command does not pop, switch branches, or change a clean tree.
func StashWorkspace(
	ctx context.Context,
	git LocalGit,
	filename string,
	options *WorkspaceStashOptions,
) (*WorkspaceStashResult, error) {
	if filename == "" || options == nil || options.BaseDir == "" || git == nil {
		return nil, errWorkspaceStashFlags
	}

	spec, err := LoadWorkspace(filename)
	if err != nil {
		return nil, err
	}

	return stashProjects(ctx, git, options.BaseDir, spec.Projects)
}

// stashProjects visits every listed repository and keeps going after one failure.
func stashProjects(
	ctx context.Context,
	git LocalGit,
	base string,
	projects []*ProjectSpec,
) (*WorkspaceStashResult, error) {
	result := &WorkspaceStashResult{}
	phase := logger.NewProgress(stashPhase, listedCount(projects))

	var failed error

	for _, project := range projects {
		if project == nil || project.Path == "" {
			continue
		}

		outcome, err := stashProject(ctx, git, base, project)
		if err != nil {
			failed = errors.Join(failed, fmt.Errorf("%s: %w", project.Path, err))
			phase.Warn(ctx, project.Path, "stash failed")

			continue
		}

		recordStash(result, outcome.record, outcome.kind)
		entry := &stashLog{
			phase:  phase,
			path:   project.Path,
			record: outcome.record,
			kind:   outcome.kind,
		}
		logStash(ctx, entry)
	}

	logStashSummary(ctx, phase, result)

	return result, failed
}

// listedCount is the number of projects the progress total includes.
func listedCount(projects []*ProjectSpec) int {
	count := 0

	for _, project := range projects {
		if project != nil && project.Path != "" {
			count++
		}
	}

	return count
}

// logStash writes one repository in the same progress form as status and sync.
// A clean worktree is info. A stash, an existing stash, and a missing directory are warnings.
func logStash(ctx context.Context, entry *stashLog) {
	if entry == nil || entry.phase == nil || entry.path == "" {
		return
	}

	switch entry.kind {
	case stashCreated:
		entry.phase.Warn(ctx, entry.path, stashNote("stashed", entry.record))
	case stashKept:
		entry.phase.Warn(ctx, entry.path, stashNote("already stashed", entry.record))
	case stashClean:
		entry.phase.Advance(ctx, entry.path, "clean")
	case stashMissing:
		entry.phase.Warn(ctx, entry.path, "missing directory")
	}
}

// stashNote is the progress message, including the oid when this run has one.
func stashNote(action string, record *StashRecord) string {
	if record == nil || record.OID == "" {
		return action
	}

	return action + " " + record.OID
}

// logStashSummary writes the phase totals once the visits are finished.
func logStashSummary(ctx context.Context, phase *logger.Progress, result *WorkspaceStashResult) {
	if phase == nil || result == nil || phase.Total() == 0 {
		return
	}

	elapsed := time.Since(phase.Started())
	percent, left := logger.PaceText(phase.Done(), phase.Total(), elapsed)

	logger.InfoKV(
		ctx,
		"finished",
		"phase", stashPhase,
		"elapsed", logger.DurationText(elapsed),
		"done", fmt.Sprintf("%d/%d", phase.Done(), phase.Total()),
		"percent", percent,
		"left", left,
		"stashed", len(result.Stashed),
		"kept", len(result.Kept),
		"clean", result.Clean,
		"missing", len(result.Missing),
	)
}

// recordStash stores one repository outcome.
func recordStash(result *WorkspaceStashResult, record *StashRecord, kind string) {
	switch kind {
	case stashCreated:
		result.Stashed = append(result.Stashed, record)
	case stashKept:
		result.Kept = append(result.Kept, record)
	case stashClean:
		result.Clean++
	case stashMissing:
		result.Missing = append(result.Missing, record.Path)
	}
}

// stashIdentity refuses a named URL that is not this checkout's origin.
func stashIdentity(ctx context.Context, git LocalGit, dir string, project *ProjectSpec) error {
	if project == nil || strings.TrimSpace(project.URL) == "" {
		return nil
	}

	origin, err := git.Local(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		return err
	}

	if remoteIdentity(project.URL, origin) != "" {
		return fmt.Errorf("%s: %w", project.Path, errArchiveIdentity)
	}

	return nil
}

// stashProject pushes one dirty worktree or reports why it did not.
func stashProject(
	ctx context.Context,
	git LocalGit,
	base string,
	project *ProjectSpec,
) (*stashOutcome, error) {
	if project == nil {
		return nil, errWorkspaceStashFlags
	}

	path := project.Path
	dir, err := ResolveProjectDirectory(base, path)

	if errors.Is(err, os.ErrNotExist) {
		missing := &StashRecord{Path: path}
		outcome := &stashOutcome{
			record: missing,
			kind:   stashMissing,
		}

		return outcome, nil
	}

	if err != nil {
		return nil, err
	}

	if err = sameWorktreeRoot(ctx, git, dir); err != nil {
		return nil, err
	}

	if err = stashIdentity(ctx, git, dir, project); err != nil {
		return nil, err
	}

	dirty, err := worktreeDirty(ctx, git, dir)
	if err != nil {
		return nil, err
	}

	if !dirty {
		return &stashOutcome{kind: stashClean}, nil
	}

	found, err := findStash(ctx, git, dir, stashMessage)
	if err != nil {
		return nil, err
	}

	if found.found {
		kept := &StashRecord{Path: path, OID: found.oid}
		outcome := &stashOutcome{
			record: kept,
			kind:   stashKept,
		}

		return outcome, nil
	}

	if err = pushStash(ctx, git, dir); err != nil {
		return nil, err
	}

	oid, err := currentStash(ctx, git, dir)
	if err != nil {
		return nil, err
	}

	created := &StashRecord{Path: path, OID: oid}
	outcome := &stashOutcome{
		record: created,
		kind:   stashCreated,
	}

	return outcome, nil
}

// worktreeDirty reports staged, unstaged, and untracked changes.
func worktreeDirty(ctx context.Context, git LocalGit, dir string) (bool, error) {
	out, err := git.Local(ctx, dir, "status", "--porcelain=v1", "-z", "--untracked-files=normal")
	if err != nil {
		return false, err
	}

	return strings.TrimRight(out, "\x00") != "", nil
}

// findStash returns the newest stash whose subject is the workspace message.
func findStash(ctx context.Context, git LocalGit, dir, message string) (*foundStash, error) {
	out, err := git.Local(ctx, dir, "stash", "list", "--format=%H%x1e%s")
	if err != nil {
		return nil, err
	}

	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}

		hash, subject, ok := strings.Cut(line, "\x1e")
		if ok && sameStashMessage(subject, message) {
			found := &foundStash{
				oid:   hash,
				found: true,
			}

			return found, nil
		}
	}

	return &foundStash{}, nil
}

// sameStashMessage matches the message Git stores as "On <branch>: <message>".
func sameStashMessage(subject, message string) bool {
	return subject == message || strings.HasSuffix(subject, ": "+message)
}

// pushStash saves tracked and untracked changes and leaves them out of the worktree.
func pushStash(ctx context.Context, git LocalGit, dir string) error {
	_, err := git.Local(ctx, dir, "stash", "push", "-u", "-m", stashMessage)

	return err
}

// currentStash reads the commit created by the push that just ran.
func currentStash(ctx context.Context, git LocalGit, dir string) (string, error) {
	oid, err := git.Local(ctx, dir, "rev-parse", "stash")
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(oid), nil
}
