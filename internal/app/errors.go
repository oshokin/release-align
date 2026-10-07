package app

import "errors"

var (
	// errConfigNil rejects a run started without configuration.
	errConfigNil = errors.New("config is nil")
	// errBaseDirEmpty rejects an empty repository root.
	errBaseDirEmpty = errors.New("base directory must not be empty")
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
	// errBaseDirNotDirectory rejects a base path that is not a directory.
	errBaseDirNotDirectory = errors.New("base-dir is not a directory")
)
