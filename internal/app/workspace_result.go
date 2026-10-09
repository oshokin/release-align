package app

import (
	"context"
	"errors"
)

// workspaceFinish is the planned run that finishWorkspace turns into a report.
type workspaceFinish struct {
	// report is the mutable workspace report.
	report *WorkspaceReport
	// items are the planned repositories.
	items []*workspaceItem
	// freshness is the remote inventory note, when one was collected.
	freshness string
	// runErr is the first failure from planning or applying.
	runErr error
}

// finishWorkspace fills the report and chooses the error returned to the caller.
func finishWorkspace(ctx context.Context, done *workspaceFinish) (*WorkspaceReport, error) {
	if done.freshness != "" {
		done.report.Freshness = done.freshness
	}

	if ctx.Err() != nil && (done.runErr == nil || errors.Is(done.runErr, context.Canceled)) {
		markPending(done.items, outcomeCanceled, reasonCanceled, messageRunStopped)
		done.runErr = context.Cause(ctx)
	}

	if done.runErr != nil && !workspaceUsage(done.runErr) && !errors.Is(done.runErr, errWorkspaceNotReady) {
		done.report.Errors = append(done.report.Errors, redactGitText(done.runErr.Error()))
	}

	finalizeErr := done.report.Finalize(workspacePaths(done.items))
	if finalizeErr != nil {
		done.report.Errors = append(done.report.Errors, finalizeErr.Error())
		done.runErr = preferErr(done.runErr, finalizeErr)
	}

	if done.runErr != nil {
		return done.report, done.runErr
	}

	return done.report, workspaceResultError(done.report)
}

// preferErr keeps the first error and ignores a later one.
func preferErr(current, next error) error {
	if current != nil {
		return current
	}

	return next
}

// workspaceUsage reports errors that are usage failures rather than runtime failures.
func workspaceUsage(err error) bool {
	return errors.Is(err, errWorkspaceUnknownGroup) ||
		errors.Is(err, errWorkspaceUnknownProject) ||
		errors.Is(err, errWorkspacePathKind) ||
		errors.Is(err, errWorkspacePath) ||
		errors.Is(err, errWorkspaceDuplicate) ||
		errors.Is(err, errGitLabSource) ||
		errors.Is(err, errGitLabURL) ||
		errors.Is(err, errGitLabGroups) ||
		errors.Is(err, errGitLabProtocol) ||
		errors.Is(err, errRemoteDryRun) ||
		errors.Is(err, errRemoteToken) ||
		errors.Is(err, errCloneSelection) ||
		errors.Is(err, errCloneUnknown) ||
		errors.Is(err, errArchivedOptIn) ||
		errors.Is(err, errCloneBranch) ||
		errors.Is(err, errCloneAlign) ||
		errors.Is(err, errTimeoutRange) ||
		errors.Is(err, errWorkspaceTimeout) ||
		errors.Is(err, errWorkspaceOutput) ||
		errors.Is(err, errArchiveFile) ||
		errors.Is(err, errArchiveEmpty) ||
		errors.Is(err, errArchiveReserved) ||
		errors.Is(err, errWorkspaceInactive) ||
		errors.Is(err, errWorkspaceRevisionShort) ||
		errors.Is(err, errCloneDepth)
}

// workspaceResultError returns an error when the report is not a success.
func workspaceResultError(report *WorkspaceReport) error {
	if reportHasRuntime(report) {
		return errWorkspaceRuntime
	}

	if report.DryRun && reportBlocked(report) {
		return errWorkspaceNotReady
	}

	if report.DryRun {
		return nil
	}

	if !report.Ready {
		if report.ActionableCount == 0 && report.SkippedArchivedCount > 0 &&
			report.SkippedArchivedCount == report.ExpectedCount {
			return nil
		}

		return errWorkspaceNotReady
	}

	return nil
}

// reportHasRuntime reports a row that failed in Git, auth, or the network.
func reportHasRuntime(report *WorkspaceReport) bool {
	for _, row := range report.Rows {
		switch row.ReasonCode {
		case reasonGitFailed, reasonAuth, reasonNetwork:
			return true
		}
	}

	return false
}

// reportBlocked reports a row that is not ready.
func reportBlocked(report *WorkspaceReport) bool {
	for _, row := range report.Rows {
		if row.Outcome == outcomeSkipped || row.ReasonCode == "" {
			continue
		}

		return true
	}

	return false
}
