package app

import "errors"

var (
	// errConfigNil rejects a run started without configuration.
	errConfigNil = errors.New("config is nil")
	// errBaseDirEmpty rejects an empty repository root.
	errBaseDirEmpty = errors.New("base directory must not be empty")
	// errDepthRange rejects a search depth outside 1..32.
	errDepthRange = errors.New("depth must be between 1 and 32")
	// errJobsRange rejects a worker count outside 1..64.
	errJobsRange = errors.New("jobs must be between 1 and 64")
	// errAttemptsRange rejects an attempt count outside 1..10.
	errAttemptsRange = errors.New("attempts must be between 1 and 10 (total attempts, not extra retries)")
	// errTimeoutRange rejects a non-positive timeout or a negative retry delay.
	errTimeoutRange = errors.New("timeouts must be positive; retry delay must not be negative")
	// errInvalidBranch rejects an empty branch name or one that looks like a flag.
	errInvalidBranch = errors.New("invalid release branch")
	// errInvalidLogLevel rejects a name zap cannot parse.
	errInvalidLogLevel = errors.New("unrecognized log level")
	// errInvalidLocal rejects a --local value other than skip, keep, or reset.
	errInvalidLocal = errors.New("local must be skip, keep, or reset")
	// errNothingStashed means a dirty tree was not stored in the stash.
	errNothingStashed = errors.New("local changes were not stashed")
	// errStillDirty means stash left paths that would block checkout.
	errStillDirty = errors.New("working tree is still dirty after stash")
	// errStashKept means the stash entry must stay because apply or drop failed.
	errStashKept = errors.New("local changes remain in the stash")
	// errBaseDirNotDirectory rejects a base path that is not a directory.
	errBaseDirNotDirectory = errors.New("base-dir is not a directory")
	// errRepositoriesFailed reports how many repositories failed after the run finished.
	errRepositoriesFailed = errors.New("repositories failed")
	// errInvalidRepositoryKey rejects a blank or padded versions-file key.
	errInvalidRepositoryKey = errors.New("invalid repository key")
	// errExpectedTagCommit rejects a versions-file value that is not TAG:HEX_COMMIT.
	errExpectedTagCommit = errors.New("expected TAG:HEX_COMMIT")
	// errVersionsFileTooLarge rejects a versions file bigger than 16 MiB.
	errVersionsFileTooLarge = errors.New("versions file exceeds 16 MiB")
	// errVersionsFileExtension rejects a versions file that is not JSON.
	errVersionsFileExtension = errors.New("versions file must use a .json extension")
	// errVersionsFileEmpty rejects a versions file with no JSON object.
	errVersionsFileEmpty = errors.New("versions file must contain a JSON object")
	// errUnexpectedJSON rejects tokens that follow the versions JSON object.
	errUnexpectedJSON = errors.New("unexpected data after JSON map")
	// errUpstreamNotOrigin rejects a current branch that does not track origin.
	errUpstreamNotOrigin = errors.New("fallback upstream is not origin")
	// errTargetNotFound means the selector should try the next candidate.
	errTargetNotFound = errors.New("target not found")
)
