package app

import (
	"os"
	"path/filepath"
)

// fetchOutcome is one repository fetch and its typed error.
type fetchOutcome struct {
	// repo is the repository that was fetched.
	repo *repository
	// err is the fetch error. Nil means the fetch finished.
	err error
}

// workspaceItem is one selected project and the report row the collector owns.
type workspaceItem struct {
	// spec is the workspace entry.
	spec *ProjectSpec
	// row is the report line owned by the collector.
	row *WorkspaceRow
	// repo is the local Git repository, when one was opened.
	repo *repository
	// revision is the pin or the workspace default for this project.
	revision *RevisionSpec
	// resolved is the object id that revision resolved to.
	resolved *ResolvedRevision
	// dir is the absolute worktree path.
	dir string
}

// newWorkspaceItems builds one work item per selected project.
func newWorkspaceItems(spec *WorkspaceSpec, selected []*ProjectSpec) []*workspaceItem {
	items := make([]*workspaceItem, 0, len(selected))

	for _, project := range selected {
		record := &WorkspaceRow{
			Path:       project.Path,
			Outcome:    outcomeNotStarted,
			ReasonCode: reasonNotStarted,
			Message:    "not started",
		}
		item := &workspaceItem{
			spec:     project,
			revision: spec.RevisionFor(project),
			row:      record,
		}
		items = append(items, item)
	}

	return items
}

// workspacePaths returns the project paths in selection order.
func workspacePaths(items []*workspaceItem) []string {
	paths := make([]string, len(items))

	for i, item := range items {
		paths[i] = item.spec.Path
	}

	return paths
}

// workspaceScope reports whether the run used the full inventory or a filter.
func workspaceScope(cfg *Config) string {
	if len(cfg.Repositories) == 0 && len(cfg.Groups) == 0 {
		return scopeWorkspace
	}

	return scopeSelection
}

// blockRow stores an outcome, a reason, and a message on one row.
func blockRow(row *WorkspaceRow, outcome, code, message string) {
	row.Outcome = outcome
	row.ReasonCode = code
	row.Message = message
	row.Ready = false
}

// rowPending reports a row that has not reached an outcome yet.
func rowPending(row *WorkspaceRow) bool {
	return row != nil && row.ReasonCode == reasonNotStarted
}

// markPending applies one outcome to every row that is still pending.
func markPending(items []*workspaceItem, outcome, code, message string) {
	for _, item := range items {
		if rowPending(item.row) || (item.row.Outcome == outcomePlanned && item.row.ReasonCode == "") {
			blockRow(item.row, outcome, code, message)
		}
	}
}

// itemsBlocked reports whether any selected row already has a reason.
func (*runner) itemsBlocked(items []*workspaceItem) bool {
	for _, item := range items {
		if item.row.ReasonCode != "" {
			return true
		}
	}

	return false
}

// blockReadyPlans stops clean plans because another repository is blocked.
func (*runner) blockReadyPlans(items []*workspaceItem) {
	for _, item := range items {
		if item.row.Outcome == outcomePlanned && item.row.ReasonCode == "" {
			blockRow(
				item.row,
				outcomeNotStarted,
				reasonPlanBlocked,
				"another selected repository blocks the workspace plan",
			)
		}
	}
}

// hasFetchable reports a pending repository that can be fetched.
func (*runner) hasFetchable(items []*workspaceItem) bool {
	for _, item := range items {
		if item.repo != nil && rowPending(item.row) {
			return true
		}
	}

	return false
}

// itemByRepo finds the work item for a repository pointer.
func (*runner) itemByRepo(items []*workspaceItem, repo *repository) *workspaceItem {
	for _, item := range items {
		if item.repo == repo {
			return item
		}
	}

	return nil
}

// pathsSame reports whether two paths name the same directory.
func (*runner) pathsSame(left, right string) (bool, error) {
	leftResolved, err := filepath.EvalSymlinks(left)
	if err != nil {
		return false, err
	}

	rightResolved, err := filepath.EvalSymlinks(right)
	if err != nil {
		return false, err
	}

	leftInfo, err := os.Stat(leftResolved)
	if err != nil {
		return false, err
	}

	rightInfo, err := os.Stat(rightResolved)
	if err != nil {
		return false, err
	}

	return os.SameFile(leftInfo, rightInfo), nil
}

// alreadyPinned reports a clean worktree already at the requested object.
func (*runner) alreadyPinned(state *ObservedState, resolved *ResolvedRevision) bool {
	if state == nil || resolved == nil || !state.Verified || state.Dirty || state.Operation != "" {
		return false
	}

	if state.Head != resolved.OID {
		return false
	}

	switch resolved.Kind {
	case revisionBranch:
		return state.Branch == resolved.Value
	case revisionTag, revisionCommit:
		return true
	default:
		return false
	}
}
