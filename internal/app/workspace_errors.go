package app

import (
	"errors"
	"fmt"
)

const (
	// revisionBranch is a branch pin.
	revisionBranch = "branch"
	// revisionTag is a tag pin.
	revisionTag = "tag"
	// revisionCommit is a commit pin.
	revisionCommit = "commit"

	// reasonMissingRepository means the listed directory is absent.
	reasonMissingRepository = "missing_repository"
	// reasonNotRepositoryRoot means the directory is not the repository root.
	reasonNotRepositoryRoot = "not_repository_root"
	// reasonMissingOrigin means the repository has no usable origin.
	reasonMissingOrigin = "missing_origin"
	// reasonMissingTarget means the requested revision does not exist locally.
	reasonMissingTarget = "missing_target"
	// reasonDirty means uncommitted changes block a switch.
	reasonDirty = "dirty_worktree"
	// reasonUnfinished means a merge, rebase, or cherry-pick is in progress.
	reasonUnfinished = "unfinished_operation"
	// reasonDetached means HEAD is detached when a branch was required.
	reasonDetached = "detached_head"
	// reasonUnpushed means the branch has commits that are not on the remote.
	reasonUnpushed = "unpushed_commits"
	// reasonDiverged means the local and remote histories diverged.
	reasonDiverged = "target_diverged"
	// reasonUpstream means the upstream does not match the requested target.
	reasonUpstream = "upstream_mismatch"
	// reasonTargetChanged means the target moved after it was planned.
	reasonTargetChanged = "target_changed"
	// reasonGitFailed means Git exited with an error that is not classified further.
	reasonGitFailed = "git_failed"
	// reasonAuth means the remote rejected credentials.
	reasonAuth = "auth_failed"
	// reasonNetwork means the remote could not be reached.
	reasonNetwork = "network_unavailable"
	// reasonCanceled means the run was canceled.
	reasonCanceled = "canceled"
	// reasonNotStarted means the repository was not processed.
	reasonNotStarted = "not_started"
	// reasonPlanBlocked means planning refused the switch.
	reasonPlanBlocked = "plan_blocked"
	// reasonNotObserved means the worktree was not read back.
	reasonNotObserved = "not_observed"

	// outcomeBlocked means the repository was left unchanged.
	outcomeBlocked = "blocked"
	// outcomeUpdated means the repository was switched.
	outcomeUpdated = "updated"
	// outcomeObserved means status read the worktree.
	outcomeObserved = "observed"
	// outcomeNotStarted means work has not begun.
	outcomeNotStarted = "not_started"
	// outcomePlanned means a dry-run accepted the switch.
	outcomePlanned = "planned"
	// outcomeCanceled means the repository was stopped by cancellation.
	outcomeCanceled = "canceled"

	// ModeSync prepares selected repositories onto their exact revisions.
	ModeSync = "sync"
	// ModeStatus reads cached local state and does not fetch or switch.
	ModeStatus = "status"
	// ModePlan is a dry-run of sync. It does not mean the worktrees are ready.
	ModePlan = "plan"
	// freshnessCached means refs were read locally.
	freshnessCached = "cached"
	// freshnessFetched means refs were updated from the remote.
	freshnessFetched = "fetched"
	// scopeWorkspace means every project in the file was selected.
	scopeWorkspace = "workspace"
	// scopeSelection means a subset of projects was selected.
	scopeSelection = "selection"
	// outputText is the human-readable report.
	outputText = "text"
	// outputJSON is the machine-readable report.
	outputJSON = "json"
)

var (
	// errWorkspaceGitOID means Git did not print a full object id.
	errWorkspaceGitOID = errors.New("git returned an invalid full object ID")
	// errWorkspacePath means a project path is not canonical.
	errWorkspacePath = errors.New("invalid project path")
	// errWorkspacePathKind means a path component is a symlink or not a directory.
	errWorkspacePathKind = errors.New("project path contains a symlink or non-directory")
	// errWorkspaceRoot means the directory is not the repository root.
	errWorkspaceRoot = errors.New("project path is not the repository root")
	// errWorkspaceNilReport means a report pointer was nil.
	errWorkspaceNilReport = errors.New("nil report")
	// errWorkspaceRows means the report rows do not match the selection.
	errWorkspaceRows = errors.New("report contains nil, duplicate or unexpected repository")
	// errWorkspaceSize means the document is larger than 1 MiB.
	errWorkspaceSize = errors.New("workspace exceeds 1 MiB")
	// errWorkspaceJSON means the file is not one JSON object.
	errWorkspaceJSON = errors.New("workspace must contain one JSON object")
	// errWorkspaceNesting means JSON nesting exceeded the decoder limit.
	errWorkspaceNesting = errors.New("JSON nesting exceeds 32 levels")
	// errWorkspaceDelimiter means a JSON delimiter was unexpected.
	errWorkspaceDelimiter = errors.New("unexpected JSON delimiter")
	// errWorkspaceSchema means schema_version or projects is missing.
	errWorkspaceSchema = errors.New("workspace requires schema_version=1 and nonempty projects")
	// errWorkspaceDefaultBranch means default_branch is not a usable name.
	errWorkspaceDefaultBranch = errors.New("invalid default_branch")
	// errWorkspaceProjectPaths means a project path is empty, absolute, or duplicated.
	errWorkspaceProjectPaths = errors.New("project paths must be nonempty, canonical, relative and unique")
	// errWorkspaceRevisionMissing means a required revision was not set.
	errWorkspaceRevisionMissing = errors.New("missing revision")
	// errWorkspaceRevisionCount means a revision set more or less than one field.
	errWorkspaceRevisionCount = errors.New("revision requires exactly one of branch, tag, commit")
	// errWorkspaceCommit means a commit is not a full lowercase object id.
	errWorkspaceCommit = errors.New("commit requires a full lowercase 40- or 64-digit object ID")
	// errWorkspaceRevisionName means a branch or tag name is not usable.
	errWorkspaceRevisionName = errors.New("invalid revision name")
	// errWorkspaceExpectedPath means a selected path is not canonical.
	errWorkspaceExpectedPath = errors.New("invalid expected path")
	// errWorkspaceJSONKey means a JSON object repeated a key.
	errWorkspaceJSONKey = errors.New("invalid or duplicate JSON key")
	// errWorkspaceNoDefault means a project has neither a pin nor a default branch.
	errWorkspaceNoDefault = errors.New("no revision or default_branch")
	// errWorkspaceGroup means a group name is invalid or duplicated.
	errWorkspaceGroup = errors.New("invalid or duplicate group")
	// errWorkspaceUnknownProject means a requested path is not in the file.
	errWorkspaceUnknownProject = errors.New("unknown project")
	// errWorkspaceUnknownGroup means a requested group is not in the file.
	errWorkspaceUnknownGroup = errors.New("unknown group")
	// errWorkspaceNotReady means at least one selected project does not match.
	errWorkspaceNotReady = errors.New("selected workspace is not ready")
	// errWorkspaceDuplicate means two paths resolve to one directory.
	errWorkspaceDuplicate = errors.New("workspace paths resolve to the same directory")
	// errWorkspaceTargetChanged means the target moved after planning.
	errWorkspaceTargetChanged = errors.New("target ref changed after planning")
	// errWorkspaceRuntime means a Git operation failed.
	errWorkspaceRuntime = errors.New("workspace git operation failed")
	// errWorkspaceRequired means the workspace file flag was omitted.
	errWorkspaceRequired = errors.New("workspace file is required")
	// errWorkspaceOutput means --output is neither text nor json.
	errWorkspaceOutput = errors.New("output must be text or json")
	// errWorkspaceStatusDryRun means status was combined with --dry-run.
	errWorkspaceStatusDryRun = errors.New("status does not accept --dry-run")
	// errWorkspaceBranchOverride means --branch is not a usable name.
	errWorkspaceBranchOverride = errors.New("invalid --branch for workspace")
	// errGitText means a Git command failed with text that is wrapped for the report.
	errGitText = errors.New("git command failed")
	// errGitLabSource means --remote or clone was asked for without a gitlab block.
	errGitLabSource = errors.New(
		`workspace has no gitlab source; add "gitlab": {"url":"https://gitlab.example","groups":["group"],"clone_protocol":"ssh"}`,
	)
	// errGitLabURL means the GitLab URL is not a bare HTTPS origin.
	errGitLabURL = errors.New("gitlab url must be an https origin without a path, user, query, or fragment")
	// errGitLabGroups means a group path is empty or not canonical.
	errGitLabGroups = errors.New("gitlab groups must be nonempty canonical namespace paths")
	// errGitLabProtocol means clone_protocol is neither ssh nor https.
	errGitLabProtocol = errors.New("gitlab clone_protocol must be ssh or https")
	// errRemoteDryRun means --dry-run was combined with --remote.
	errRemoteDryRun = errors.New("--dry-run cannot be combined with --remote; use status --remote")
	// errRemoteToken means GITLAB_TOKEN is empty.
	errRemoteToken = errors.New("GITLAB_TOKEN is required for --remote and workspace clone")
	// errRemoteInventory means the GitLab listing did not finish.
	errRemoteInventory = errors.New("GitLab inventory check failed; completeness is unknown")
	// errRemoteAfterSync means alignment finished and the following inventory check failed.
	//
	//nolint:staticcheck,revive // The user-facing sentence is specified with its punctuation.
	errRemoteAfterSync = errors.New(
		"Release alignment completed; GitLab inventory check failed. No clone was attempted.",
	)
	// errCloneSelection means --repo and --all were both set or both omitted.
	errCloneSelection = errors.New("workspace clone requires --repo or --all, and not both")
	// errCloneUnknown means a requested path is not in the GitLab listing.
	errCloneUnknown = errors.New("repository is not in the GitLab inventory")
	// errCloneConflict means the target path is occupied or ambiguous.
	errCloneConflict = errors.New("target path conflicts with another repository")
	// errCloneBranch means an explicitly requested project has no default branch.
	errCloneBranch = errors.New("repository has no default branch")
	// errCloneURL means the clone URL is not an allowed ssh or https URL.
	errCloneURL = errors.New("clone URL is not an ssh or https GitLab URL")
	// errClonePartial means a later clone failed after earlier ones succeeded.
	errClonePartial = errors.New("clone stopped after a failure; completed clones were kept")
	// errClonePublish means the workspace file was not written after clones finished.
	errClonePublish = errors.New("workspace file was not updated; downloaded clones were kept")
	// errCloneAlign means a new row would have no branch to align later.
	errCloneAlign = errors.New("new project needs default_branch or an explicit revision")
)

// gitTextError wraps a Git message as a workspace runtime error.
func (*runner) gitTextError(message string) error {
	return fmt.Errorf("%w: %s", errGitText, message)
}
