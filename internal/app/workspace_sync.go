package app

import (
	"context"

	"github.com/oshokin/release-align/internal/gitter"
)

// syncWorkspace fetches, plans, and checks out the selected repositories.
func (r *runner) syncWorkspace(ctx context.Context, items []*workspaceItem) (string, error) {
	if err := r.checkWorkspaceRefs(ctx, items); err != nil {
		return freshnessCached, err
	}

	if !r.hasFetchable(items) {
		return freshnessCached, r.planWorkspace(ctx, items)
	}

	unlock, err := lockBase(r.cfg.BaseDir)
	if err != nil {
		return freshnessCached, err
	}

	defer unlock()

	ctx, cancel := context.WithCancelCause(ctx)

	defer cancel(nil)

	if err = r.workspacePreflight(ctx, items); err != nil {
		return freshnessCached, err
	}

	if err = r.fetchWorkspace(ctx, cancel, items); err != nil {
		return freshnessFetched, err
	}

	if err = r.planWorkspace(ctx, items); err != nil {
		return freshnessFetched, err
	}

	if !r.ignoreErrors() && r.itemsBlocked(items) {
		return freshnessFetched, nil
	}

	if r.beforeWorkspaceCheckout != nil {
		r.beforeWorkspaceCheckout()
	}

	return freshnessFetched, r.applyWorkspace(ctx, items)
}

// workspacePreflight probes each distinct origin before any fetch.
func (r *runner) workspacePreflight(ctx context.Context, items []*workspaceItem) error {
	checked := map[string]struct{}{}

	r.beginPhase("probe", r.probeCount(items))

	for _, item := range items {
		if item.repo == nil || !rowPending(item.row) {
			continue
		}

		if _, seen := checked[item.repo.endpoint]; seen {
			continue
		}

		err := r.probe(ctx, item.repo)
		r.step(ctx, item.repo.relative, "origin checked")

		if err == nil {
			checked[item.repo.endpoint] = struct{}{}

			continue
		}

		if ctx.Err() != nil {
			r.noteOutage(items, err)

			return context.Cause(ctx)
		}

		if gitter.NetworkError(err) {
			r.noteOutage(items, err)

			return err
		}

		r.noteProbe(ctx, items, item, err)
	}

	return nil
}

// noteProbe records a preflight failure for one repository.
func (r *runner) noteProbe(ctx context.Context, items []*workspaceItem, item *workspaceItem, err error) {
	if err == nil {
		return
	}

	if gitter.NetworkError(err) || ctx.Err() != nil {
		r.noteOutage(items, err)

		return
	}

	code, message := r.workspaceGitReason(err)
	blockRow(item.row, outcomeBlocked, code, message)
}

// noteOutage cancels pending rows when the remote cannot be reached.
func (r *runner) noteOutage(items []*workspaceItem, err error) {
	code, message := r.workspaceGitReason(err)
	if code == reasonGitFailed {
		code = reasonNetwork
	}

	markPending(items, outcomeCanceled, code, message)
}
