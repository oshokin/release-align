package app

import (
	"context"
	"errors"
)

// applyWorkspace checks out each planned repository.
// Without --ignore-errors the first failure leaves the rest unstarted.
func (r *runner) applyWorkspace(ctx context.Context, items []*workspaceItem) error {
	r.beginPhase(ctx, "apply", r.plannedCount(items))

	for index, item := range items {
		if ctx.Err() != nil {
			markPending(items, outcomeCanceled, reasonCanceled, messageRunStopped)

			return context.Cause(ctx)
		}

		if item.row.Outcome != outcomePlanned {
			continue
		}

		err := r.applyItem(ctx, item)
		r.stepLevel(ctx, item.spec.Path, r.rowLogMessage(item.row), item.row.Outcome == outcomeBlocked)

		if err == nil {
			continue
		}

		if ctx.Err() != nil {
			markPending(items, outcomeCanceled, reasonCanceled, messageRunStopped)

			return context.Cause(ctx)
		}

		if r.ignoreErrors() {
			continue
		}

		markPending(items[index+1:], outcomeNotStarted, reasonPlanBlocked, "a selected repository failed during apply")

		return nil
	}

	return nil
}

// applyItem switches one repository onto its pinned revision and reads HEAD back.
func (r *runner) applyItem(ctx context.Context, item *workspaceItem) error {
	state, err := ObserveWorkspaceState(ctx, r.git, item.dir)
	if err != nil {
		return r.failApply(ctx, item, err)
	}

	item.row.Actual = state

	code, message := r.switchBlocker(ctx, item, state, item.resolved)
	if code != "" {
		blockRow(item.row, outcomeBlocked, code, message)

		return r.gitTextError(message)
	}

	again, err := ResolveRevision(ctx, r.git, item.dir, item.revision)
	if err != nil {
		return r.failApply(ctx, item, err)
	}

	if again.OID != item.resolved.OID {
		item.row.Expected = again
		blockRow(item.row, outcomeBlocked, reasonTargetChanged, "target ref changed after planning")

		return errWorkspaceTargetChanged
	}

	if r.alreadyPinned(state, item.resolved) {
		return r.confirmItem(ctx, item)
	}

	if err = r.checkoutPlanned(ctx, item); err != nil {
		r.observeFailedApply(ctx, item)

		return r.failApply(ctx, item, err)
	}

	if r.afterWorkspaceCheckout != nil {
		r.afterWorkspaceCheckout(item.repo)
	}

	return r.confirmItem(ctx, item)
}

// failApply records why a checkout could not finish.
func (r *runner) failApply(ctx context.Context, item *workspaceItem, err error) error {
	if ctx.Err() != nil {
		blockRow(item.row, outcomeCanceled, reasonCanceled, messageRunStopped)

		return context.Cause(ctx)
	}

	if errors.Is(err, ErrWorkspaceTargetMissing) {
		blockRow(item.row, outcomeBlocked, reasonMissingTarget, "requested revision does not exist")

		return err
	}

	if errors.Is(err, errWorkspaceTargetChanged) {
		blockRow(item.row, outcomeBlocked, reasonTargetChanged, "target ref changed after planning")

		return err
	}

	code, message := r.workspaceGitReason(err)
	blockRow(item.row, outcomeBlocked, code, message)

	return err
}

// confirmItem accepts the row only when the observed HEAD matches the pin.
func (r *runner) confirmItem(ctx context.Context, item *workspaceItem) error {
	item.row.Actual = nil

	state, err := ObserveWorkspaceState(ctx, r.git, item.dir)
	if err != nil {
		return r.failApply(ctx, item, err)
	}

	item.row.Actual = state
	item.row.Expected = item.resolved
	item.row.ReasonCode = ""
	item.row.Message = ""
	item.row.StashOID = ""

	if item.row.MatchesContract() {
		item.row.Outcome = outcomeUpdated
		item.row.Message = "observed HEAD matches the requested revision"

		return nil
	}

	code, message := r.statusMismatch(state, item.resolved)
	blockRow(item.row, outcomeBlocked, code, message)
	item.row.Actual = state

	return errWorkspaceTargetChanged
}

// observeFailedApply discards pre-checkout state and attempts a fresh local observation.
// An interrupted run keeps actual absent instead of starting more subprocesses.
func (r *runner) observeFailedApply(ctx context.Context, item *workspaceItem) {
	item.row.Actual = nil

	if ctx.Err() != nil {
		return
	}

	state, err := ObserveWorkspaceState(ctx, r.git, item.dir)
	if err == nil {
		item.row.Actual = state
	}
}
