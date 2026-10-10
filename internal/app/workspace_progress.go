package app

import (
	"context"
	"time"

	"github.com/oshokin/release-align/internal/logger"
)

// stopGroup is one repeated cancellation.
type stopGroup struct {
	// row is the text shared by the group.
	row *WorkspaceRow
	// count is how many repositories were left in this state.
	count int
}

const (
	// logReady is the ready field on a repository log line.
	logReady = "ready"
)

// beginPhase starts a progress phase whose size is known.
func (r *runner) beginPhase(ctx context.Context, name string, total int) {
	if total <= 0 {
		r.progress = nil

		return
	}

	r.progress = logger.NewProgress(name, total)
	r.progress.Start(ctx)

	if r.report == nil {
		return
	}

	r.report.ProgressStarted = r.progress.Started()
	r.report.ProgressDone = 0
	r.report.ProgressTotal = total
}

// step records one finished repository in the current phase.
func (r *runner) step(ctx context.Context, repo, message string) {
	r.stepLevel(ctx, repo, message, false)
}

// stepLevel records one finished repository. A blocked repository is a warning.
func (r *runner) stepLevel(ctx context.Context, repo, message string, warn bool) {
	if r.progress == nil {
		return
	}

	if warn {
		r.progress.Warn(ctx, repo, message)
	} else {
		r.progress.Advance(ctx, repo, message)
	}

	if r.report == nil {
		return
	}

	r.report.ProgressDone = r.progress.Done()
	r.report.ProgressTotal = r.progress.Total()
}

// logTextRows prints each distinct repository result.
// Identical leftovers are one line: a canceled run, a dead origin, or a plan blocked by another repository.
func logTextRows(ctx context.Context, rows []*WorkspaceRow) {
	order := make([]string, 0)
	groups := make(map[string]*stopGroup)

	for _, row := range rows {
		if row == nil {
			continue
		}

		if !bulkStop(row) {
			logOneRow(ctx, row)

			continue
		}

		key := row.Outcome + "\x00" + row.ReasonCode + "\x00" + row.Message
		if groups[key] == nil {
			group := &stopGroup{
				row: row,
			}
			groups[key] = group

			order = append(order, key)
		}

		groups[key].count++
	}

	for _, key := range order {
		logStopGroup(ctx, groups[key])
	}
}

// logStopGroup prints one leftover repository, or a count when several share it.
func logStopGroup(ctx context.Context, group *stopGroup) {
	if group == nil || group.row == nil {
		return
	}

	if group.count == 1 {
		logOneRow(ctx, group.row)

		return
	}

	logger.DebugKV(
		ctx,
		group.row.Message,
		"outcome", group.row.Outcome,
		"reason", group.row.ReasonCode,
		"count", group.count,
	)
}

// logOneRow prints one repository. The name is a colored field on a terminal.
func logOneRow(ctx context.Context, row *WorkspaceRow) {
	fields := []any{
		"repo", row.Path,
		"outcome", row.Outcome,
		logReady, row.Ready,
	}

	if row.ReasonCode != "" {
		fields = append(fields, "reason", row.ReasonCode)
	}

	logger.DebugKV(ctx, row.Message, fields...)
}

// bulkStop reports a repository that was not started.
// The run stopped, the origin was dead, or another selected repository already blocked the plan.
func bulkStop(row *WorkspaceRow) bool {
	if row.ReasonCode == reasonPlanBlocked {
		return true
	}

	if row.Outcome != outcomeCanceled {
		return false
	}

	return row.Message == messageRunStopped || row.ReasonCode == reasonNetwork
}

// logWorkspaceSummary prints the run totals, including elapsed time and the latest phase.
func logWorkspaceSummary(ctx context.Context, report *WorkspaceReport) {
	logger.DebugKV(ctx, "workspace", summaryFields(report)...)
}

// summaryFields is the comma-separated tail of the workspace line.
func summaryFields(report *WorkspaceReport) []any {
	fields := []any{
		logReady, report.Ready,
		"coverage", report.Coverage,
		"expected", report.ExpectedCount,
		"inventory", report.InventoryCount,
	}

	if report.Scope != "" {
		fields = append(fields, "scope", report.Scope)
	}

	fields = append(fields, "freshness", report.Freshness, "mode", report.Mode)

	if report.Started.IsZero() {
		return fields
	}

	fields = append(fields, "elapsed", logger.DurationText(time.Since(report.Started)))
	percent, left := logger.PaceText(report.ProgressDone, report.ProgressTotal, phaseElapsed(report))

	if percent == "" {
		return fields
	}

	return append(fields, "percent", percent, "left", left)
}

// phaseElapsed is how long the latest phase has been running.
func phaseElapsed(report *WorkspaceReport) time.Duration {
	if report.ProgressStarted.IsZero() {
		return time.Since(report.Started)
	}

	return time.Since(report.ProgressStarted)
}

// rowLogMessage is the text progress line for one finished repository.
func (r *runner) rowLogMessage(row *WorkspaceRow) string {
	if row != nil && row.Message != "" {
		return row.Message
	}

	if row == nil {
		return ""
	}

	return row.Outcome
}

// pendingCount is the number of repositories that still have no outcome.
func (r *runner) pendingCount(items []*workspaceItem) int {
	count := 0

	for _, item := range items {
		if item != nil && rowPending(item.row) {
			count++
		}
	}

	return count
}

// plannedCount is the number of repositories waiting to be checked out.
func (r *runner) plannedCount(items []*workspaceItem) int {
	count := 0

	for _, item := range items {
		if item != nil && item.row != nil && item.row.Outcome == outcomePlanned {
			count++
		}
	}

	return count
}

// probeCount is the number of distinct origins still pending.
func (r *runner) probeCount(items []*workspaceItem) int {
	seen := make(map[string]struct{})

	for _, item := range items {
		if item == nil || item.repo == nil || !rowPending(item.row) {
			continue
		}

		seen[item.repo.endpoint] = struct{}{}
	}

	return len(seen)
}
