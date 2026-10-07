package app

import (
	"context"
	"errors"
)

// finishWorkspace fills the report and chooses the error returned to the caller.
func finishWorkspace(
	ctx context.Context,
	report *WorkspaceReport,
	items []*workspaceItem,
	freshness string,
	runErr error,
) (*WorkspaceReport, error) {
	if freshness != "" {
		report.Freshness = freshness
	}

	if ctx.Err() != nil && (runErr == nil || errors.Is(runErr, context.Canceled)) {
		markPending(items, outcomeCanceled, reasonCanceled, messageRunStopped)
		runErr = context.Cause(ctx)
	}

	if runErr != nil && !workspaceUsage(runErr) && !errors.Is(runErr, errWorkspaceNotReady) {
		report.Errors = append(report.Errors, redactGitText(runErr.Error()))
	}

	finalizeErr := report.Finalize(workspacePaths(items))
	if finalizeErr != nil {
		report.Errors = append(report.Errors, finalizeErr.Error())
		runErr = preferErr(runErr, finalizeErr)
	}

	if runErr != nil {
		return report, runErr
	}

	return report, workspaceResultError(report)
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
		if row.ReasonCode != "" {
			return true
		}
	}

	return false
}
