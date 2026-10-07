package app

import (
	"errors"
	"fmt"
)

const (
	revisionBranch = "branch"
	revisionTag    = "tag"
	revisionCommit = "commit"

	reasonMissingRepository = "missing_repository"
	reasonNotRepositoryRoot = "not_repository_root"
	reasonMissingOrigin     = "missing_origin"
	reasonMissingTarget     = "missing_target"
	reasonDirty             = "dirty_worktree"
	reasonUnfinished        = "unfinished_operation"
	reasonDetached          = "detached_head"
	reasonUnpushed          = "unpushed_commits"
	reasonDiverged          = "target_diverged"
	reasonUpstream          = "upstream_mismatch"
	reasonTargetChanged     = "target_changed"
	reasonGitFailed         = "git_failed"
	reasonAuth              = "auth_failed"
	reasonNetwork           = "network_unavailable"
	reasonCanceled          = "canceled"
	reasonNotStarted        = "not_started"
	reasonPlanBlocked       = "plan_blocked"
	reasonNotObserved       = "not_observed"

	outcomeBlocked    = "blocked"
	outcomeUpdated    = "updated"
	outcomeObserved   = "observed"
	outcomeNotStarted = "not_started"
	outcomePlanned    = "planned"
	outcomeCanceled   = "canceled"

	// ModeSync prepares selected repositories onto their exact revisions.
	ModeSync = "sync"
	// ModeStatus reads cached local state and does not fetch or switch.
	ModeStatus = "status"
	// ModePlan is a dry-run of sync. It does not mean the worktrees are ready.
	ModePlan         = "plan"
	freshnessCached  = "cached"
	freshnessFetched = "fetched"
	scopeWorkspace   = "workspace"
	scopeSelection   = "selection"
	outputText       = "text"
	outputJSON       = "json"
)

var (
	errWorkspaceGitOID           = errors.New("git returned an invalid full object ID")
	errWorkspacePath             = errors.New("invalid project path")
	errWorkspacePathKind         = errors.New("project path contains a symlink or non-directory")
	errWorkspaceRoot             = errors.New("project path is not the repository root")
	errWorkspaceNilReport        = errors.New("nil report")
	errWorkspaceRows             = errors.New("report contains nil, duplicate or unexpected repository")
	errWorkspaceFreezeIncomplete = errors.New("cannot freeze an incomplete or failed observation")
	errWorkspaceFreezeMismatch   = errors.New("cannot freeze repositories that do not match the requested clean state")
	errWorkspaceSize             = errors.New("workspace exceeds 1 MiB")
	errWorkspaceJSON             = errors.New("workspace must contain one JSON object")
	errWorkspaceNesting          = errors.New("JSON nesting exceeds 32 levels")
	errWorkspaceDelimiter        = errors.New("unexpected JSON delimiter")
	errWorkspaceSchema           = errors.New("workspace requires schema_version=1 and nonempty projects")
	errWorkspaceDefaultBranch    = errors.New("invalid default_branch")
	errWorkspaceProjectPaths     = errors.New("project paths must be nonempty, canonical, relative and unique")
	errWorkspaceRevisionMissing  = errors.New("missing revision")
	errWorkspaceRevisionCount    = errors.New("revision requires exactly one of branch, tag, commit")
	errWorkspaceCommit           = errors.New("commit requires a full lowercase 40- or 64-digit object ID")
	errWorkspaceRevisionName     = errors.New("invalid revision name")
	errWorkspaceExpectedPath     = errors.New("invalid expected path")
	errWorkspaceJSONKey          = errors.New("invalid or duplicate JSON key")
	errWorkspaceNoDefault        = errors.New("no revision or default_branch")
	errWorkspaceGroup            = errors.New("invalid or duplicate group")
	errWorkspaceUnknownProject   = errors.New("unknown project")
	errWorkspaceUnknownGroup     = errors.New("unknown group")
	errWorkspaceNotReady         = errors.New("selected workspace is not ready")
	errWorkspaceDuplicate        = errors.New("workspace paths resolve to the same directory")
	errWorkspaceTargetChanged    = errors.New("target ref changed after planning")
	errWorkspaceRuntime          = errors.New("workspace git operation failed")
	errWorkspaceRequired         = errors.New("workspace file is required")
	errWorkspaceFilter           = errors.New("--repo and --group require --workspace")
	errWorkspaceVersions         = errors.New("--workspace cannot be combined with a versions file")
	errWorkspaceDepth            = errors.New("--depth applies only to legacy discovery")
	errWorkspaceLocal            = errors.New("workspace mode accepts only --local skip")
	errWorkspaceOutput           = errors.New("output must be text or json")
	errWorkspaceJSONNeedsFile    = errors.New("--output json requires --workspace")
	errWorkspaceStatusDryRun     = errors.New("status does not accept --dry-run")
	errWorkspaceBranchOverride   = errors.New("invalid --branch for workspace")
	errGitText                   = errors.New("git command failed")
)

// gitTextError wraps a Git message as a workspace runtime error.
func (*runner) gitTextError(message string) error {
	return fmt.Errorf("%w: %s", errGitText, message)
}
