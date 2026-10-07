package app

import (
	"context"
	"errors"
)

// checkWorkspaceRefs rejects revision names Git would not accept.
func (r *runner) checkWorkspaceRefs(ctx context.Context, items []*workspaceItem) error {
	for _, item := range items {
		if item.repo == nil || !rowPending(item.row) {
			continue
		}

		ref := r.workspaceRefName(item.revision)
		if ref == "" {
			continue
		}

		_, err := r.git.Local(ctx, item.dir, "check-ref-format", ref)
		if err == nil {
			continue
		}

		if ctx.Err() != nil {
			markPending(items, outcomeCanceled, reasonCanceled, messageRunStopped)

			return context.Cause(ctx)
		}

		blockRow(item.row, outcomeBlocked, reasonMissingTarget, "revision name is not a valid Git ref")
	}

	return nil
}

// workspaceRefName returns the ref Git should validate for a revision.
func (*runner) workspaceRefName(revision *RevisionSpec) string {
	switch {
	case revision.Branch != "":
		return "refs/heads/" + revision.Branch
	case revision.Tag != "":
		return "refs/tags/" + revision.Tag
	default:
		return ""
	}
}

// planWorkspace decides the outcome of every pending repository.
func (r *runner) planWorkspace(ctx context.Context, items []*workspaceItem) error {
	for _, item := range items {
		if !rowPending(item.row) {
			continue
		}

		if err := r.planItem(ctx, item); err != nil {
			markPending(items, outcomeCanceled, reasonCanceled, messageRunStopped)

			return err
		}
	}

	if r.itemsBlocked(items) {
		r.blockReadyPlans(items)
	}

	return nil
}

// planItem resolves one repository and records whether it can be switched.
func (r *runner) planItem(ctx context.Context, item *workspaceItem) error {
	resolved, state, err := r.readRevision(ctx, item)
	if err != nil || resolved == nil {
		return err
	}

	code, message := r.switchBlocker(ctx, item, state, resolved)
	if code != "" {
		blockRow(item.row, outcomeBlocked, code, message)

		return nil
	}

	item.row.ReasonCode = ""
	item.row.Message = messagePlanAdmissible
	item.row.Outcome = outcomePlanned

	return nil
}

// readCached resolves revisions from the local clone without fetching.
func (r *runner) readCached(ctx context.Context, items []*workspaceItem, plan bool) error {
	for _, item := range items {
		if !rowPending(item.row) {
			continue
		}

		if err := r.readOne(ctx, item, plan); err != nil {
			markPending(items, outcomeCanceled, reasonCanceled, messageRunStopped)

			return err
		}
	}

	if plan && r.itemsBlocked(items) {
		r.blockReadyPlans(items)
	}

	return nil
}

// readOne reads one repository and either plans it or records its status.
func (r *runner) readOne(ctx context.Context, item *workspaceItem, plan bool) error {
	resolved, state, err := r.readRevision(ctx, item)
	if err != nil || resolved == nil {
		return err
	}

	if plan {
		return r.finishPlan(ctx, item, state, resolved)
	}

	if r.alreadyPinned(state, resolved) {
		item.row.ReasonCode = ""
		item.row.Outcome = outcomeObserved
		item.row.Message = "local HEAD matches the requested revision"

		return nil
	}

	code, message := r.statusMismatch(state, resolved)
	blockRow(item.row, outcomeBlocked, code, message)

	return nil
}

// finishPlan marks a repository planned or blocked from the observed state.
func (r *runner) finishPlan(
	ctx context.Context,
	item *workspaceItem,
	state *ObservedState,
	resolved *ResolvedRevision,
) error {
	code, message := r.switchBlocker(ctx, item, state, resolved)
	if code != "" {
		blockRow(item.row, outcomeBlocked, code, message)

		return nil
	}

	item.row.ReasonCode = ""
	item.row.Message = messagePlanAdmissible
	item.row.Outcome = outcomePlanned

	return nil
}

// readRevision resolves the requested object and reads the worktree.
func (r *runner) readRevision(
	ctx context.Context,
	item *workspaceItem,
) (*ResolvedRevision, *ObservedState, error) {
	resolved, err := ResolveRevision(ctx, r.git, item.dir, item.revision)
	if err != nil {
		return nil, nil, r.noteResolve(ctx, item, err)
	}

	state, err := ObserveWorkspaceState(ctx, r.git, item.dir)
	if err != nil && ctx.Err() != nil {
		return nil, nil, context.Cause(ctx)
	}

	if err != nil {
		code, message := r.workspaceGitReason(err)
		blockRow(item.row, outcomeBlocked, code, message)

		return nil, nil, nil
	}

	item.resolved = resolved
	item.row.Expected = resolved
	item.row.Actual = state

	return resolved, state, nil
}

// noteResolve records a failure to resolve the requested revision.
func (r *runner) noteResolve(ctx context.Context, item *workspaceItem, err error) error {
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}

	if errors.Is(err, ErrWorkspaceTargetMissing) {
		blockRow(item.row, outcomeBlocked, reasonMissingTarget, "requested revision does not exist")

		return nil
	}

	code, message := r.workspaceGitReason(err)
	blockRow(item.row, outcomeBlocked, code, message)

	return nil
}
