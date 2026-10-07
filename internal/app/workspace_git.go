package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oshokin/release-align/internal/gitter"
)

// LocalGit is the small existing gitter.Client surface needed here.
type LocalGit interface {
	Local(ctx context.Context, dir string, args ...string) (string, error)
}

// ErrWorkspaceTargetMissing means the requested revision is not in the local repository.
var ErrWorkspaceTargetMissing = errors.New("workspace target is missing")

// ResolveRevision reads cached refs after the caller's fetch, never substitutes another target.
func ResolveRevision(ctx context.Context, g LocalGit, dir string, spec *RevisionSpec) (*ResolvedRevision, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	result := &ResolvedRevision{
		Kind:  revisionCommit,
		Value: spec.Commit,
	}

	ref, err := revisionRef(ctx, g, dir, spec, result)
	if err != nil {
		return nil, err
	}

	// --quiet has a documented nonzero result for an absent/unresolvable object.
	oid, err := g.Local(ctx, dir, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	if gitter.ExitCode(err) == 1 {
		return nil, fmt.Errorf("%w: %s", ErrWorkspaceTargetMissing, result.Value)
	}

	if err != nil {
		return nil, err
	}

	if !workspaceOID.MatchString(oid) {
		return nil, errWorkspaceGitOID
	}
	result.OID = oid

	return result, nil
}

// ResolveProjectDirectory rejects escapes and aliases through directory symlinks.
// It deliberately requires an existing directory: missing clones are reported, not created.
func ResolveProjectDirectory(base, relative string) (string, error) {
	if !canonicalProjectPath(relative) {
		return "", errWorkspacePath
	}

	absolute, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}

	root, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	current := root
	for part := range strings.SplitSeq(relative, "/") {
		current = filepath.Join(current, part)

		info, statErr := os.Lstat(current)
		if statErr != nil {
			return "", statErr
		}

		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", errWorkspacePathKind
		}
	}

	return current, nil
}

// ObserveWorkspaceState does not decide whether switching is safe.
// safeCurrent remains mandatory before mutations.
func ObserveWorkspaceState(ctx context.Context, g LocalGit, dir string) (*ObservedState, error) {
	if err := sameWorktreeRoot(ctx, g, dir); err != nil {
		return nil, err
	}

	head, err := g.Local(ctx, dir, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return nil, err
	}
	branch, err := g.Local(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil && gitter.ExitCode(err) != 1 {
		return nil, err
	}

	status, err := g.Local(
		ctx,
		dir,
		"status",
		"--porcelain=v1",
		"-z",
		"--untracked-files=normal",
		"--ignore-submodules=none",
	)
	if err != nil {
		return nil, err
	}
	state := &ObservedState{
		Head:   head,
		Branch: branch,
		Dirty:  status != "",
	}

	for _, name := range gitOperationNames() {
		p, pathErr := g.Local(ctx, dir, "rev-parse", "--git-path", name)
		if pathErr != nil {
			return nil, pathErr
		}

		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}

		_, statErr := os.Stat(p)
		if statErr == nil {
			state.Operation = name

			break
		}

		if !errors.Is(statErr, os.ErrNotExist) {
			return nil, statErr
		}
	}
	state.Verified = workspaceOID.MatchString(head)

	return state, nil
}

// sameWorktreeRoot reports that dir is the root returned by git rev-parse --show-toplevel.
// A linked worktree is accepted. A subdirectory of another repository is not.
func sameWorktreeRoot(ctx context.Context, g LocalGit, dir string) error {
	top, err := g.Local(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}

	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}

	got, err := filepath.EvalSymlinks(top)
	if err != nil {
		return err
	}

	wi, err := os.Stat(want)
	if err != nil {
		return err
	}

	gi, err := os.Stat(got)
	if err != nil {
		return err
	}

	if !os.SameFile(wi, gi) {
		return errWorkspaceRoot
	}

	return nil
}

// revisionRef selects the cached ref that must resolve to the requested revision.
func revisionRef(
	ctx context.Context,
	g LocalGit,
	dir string,
	spec *RevisionSpec,
	result *ResolvedRevision,
) (string, error) {
	if spec.Branch != "" {
		result.Kind, result.Value = revisionBranch, spec.Branch
		_, err := g.Local(ctx, dir, "check-ref-format", "refs/heads/"+spec.Branch)

		return "refs/remotes/origin/" + spec.Branch, err
	}

	if spec.Tag != "" {
		result.Kind, result.Value = revisionTag, spec.Tag
		_, err := g.Local(ctx, dir, "check-ref-format", "refs/tags/"+spec.Tag)

		return "refs/tags/" + spec.Tag, err
	}

	return spec.Commit, nil
}

// gitOperationNames is shared by safety checks and final observations.
func gitOperationNames() []string {
	return []string{
		"MERGE_HEAD",
		"CHERRY_PICK_HEAD",
		"REVERT_HEAD",
		"BISECT_LOG",
		"rebase-merge",
		"rebase-apply",
		"sequencer",
	}
}
