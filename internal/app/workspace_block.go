package app

import (
	"context"
	"fmt"
	"strings"
)

// statusMismatch explains why the observed HEAD does not match the pin.
func (*runner) statusMismatch(state *ObservedState, resolved *ResolvedRevision) (string, string) {
	if state.Dirty {
		return reasonDirty, messageDirtyTree
	}

	if state.Operation != "" {
		return reasonUnfinished, "unfinished Git operation: " + state.Operation
	}

	if resolved.Kind == revisionBranch && state.Branch == "" {
		return reasonDetached, messageDetachedHead
	}

	branch := state.Branch
	if branch == "" {
		branch = "detached"
	}

	return reasonDiverged, fmt.Sprintf("HEAD %s on %s is not %s %s", state.Head, branch, resolved.Kind, resolved.Value)
}

// switchBlocker reports a condition that forbids checkout.
func (r *runner) switchBlocker(
	ctx context.Context,
	item *workspaceItem,
	state *ObservedState,
	resolved *ResolvedRevision,
) (string, string) {
	if state.Dirty {
		return reasonDirty, messageDirtyTree
	}

	if state.Operation != "" {
		return reasonUnfinished, "unfinished Git operation: " + state.Operation
	}

	if r.alreadyPinned(state, resolved) {
		return "", ""
	}

	if state.Branch == "" {
		return reasonDetached, messageDetachedHead
	}

	reason, err := r.safeCurrent(ctx, item.repo)
	if err != nil {
		return r.workspaceGitReason(err)
	}

	if reason != "" {
		return r.mapSafeReason(reason), reason
	}

	if resolved.Kind != revisionBranch {
		return "", ""
	}

	return r.branchBlocker(ctx, item, resolved)
}

// mapSafeReason maps a legacy safety message onto a workspace reason code.
func (*runner) mapSafeReason(reason string) string {
	switch {
	case strings.Contains(reason, "unpushed"), strings.Contains(reason, "diverges"):
		return reasonUnpushed
	case strings.Contains(reason, "upstream"), strings.Contains(reason, "tracks a remote"):
		return reasonUpstream
	case strings.Contains(reason, "detached"):
		return reasonDetached
	case strings.Contains(reason, "unfinished"):
		return reasonUnfinished
	case strings.Contains(reason, "staged"), strings.Contains(reason, "untracked"):
		return reasonDirty
	default:
		return reasonGitFailed
	}
}

// branchBlocker reports a local branch that cannot fast-forward to the pin.
func (r *runner) branchBlocker(ctx context.Context, item *workspaceItem, resolved *ResolvedRevision) (string, string) {
	busy, err := r.branchBusy(ctx, item.repo, resolved.Value)
	if err != nil {
		return r.workspaceGitReason(err)
	}

	if busy {
		return reasonGitFailed, "target branch is checked out in another worktree"
	}

	local := "refs/heads/" + resolved.Value

	exists, err := r.refExists(ctx, item.repo, local)
	if err != nil {
		return r.workspaceGitReason(err)
	}

	if !exists {
		return "", ""
	}

	ok, err := r.ancestor(ctx, item.repo, local, resolved.OID)
	if err != nil {
		return r.workspaceGitReason(err)
	}

	if !ok {
		return reasonDiverged, "target branch has local commits or diverges from the pinned revision"
	}

	upstream, err := r.git.Local(ctx, item.dir, "for-each-ref", "--format=%(upstream)", local)
	if err != nil {
		return r.workspaceGitReason(err)
	}

	if upstream != "refs/remotes/origin/"+resolved.Value {
		return reasonUpstream, "target branch has no matching origin upstream"
	}

	return "", ""
}
