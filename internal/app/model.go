package app

import (
	"context"
	"time"

	"github.com/oshokin/release-align/internal/logger"
	"github.com/oshokin/release-align/internal/retry"
)

type (
	// repository is a Git working tree and the paths used to lock it.
	repository struct {
		// path is the absolute working tree path.
		path string
		// name is the repository directory name.
		name string
		// relative is the slash-separated path from the base directory.
		relative string
		// common is the Git common directory used to serialize worktrees.
		common string
		// endpoint is the normalized origin URL.
		endpoint string
	}

	// gitClient is the Git surface the runner calls.
	gitClient interface {
		// Local runs a Git command that does not talk to a remote.
		Local(ctx context.Context, dir string, args ...string) (string, error)
		// Probe checks that origin answers within timeout.
		Probe(ctx context.Context, dir string, timeout time.Duration) error
		// Fetch updates origin branches and tags.
		Fetch(ctx context.Context, dir string, timeout time.Duration) error
	}

	// runner shares read-only configuration and clients across repository workers.
	runner struct {
		// cfg is the read-only run configuration.
		cfg *Config
		// git runs Git commands.
		git gitClient
		// probeRetry retries origin reachability checks.
		probeRetry *retry.Engine
		// beforeWorkspaceCheckout runs once after a clean plan and before the first switch.
		beforeWorkspaceCheckout func()
		// afterWorkspaceCheckout runs after one switch and before that repository is read back.
		afterWorkspaceCheckout func(*repository)
		// progress counts the phase that is running now.
		progress *logger.Progress
		// report keeps the latest phase counts for the final summary.
		report *WorkspaceReport
	}
)

const (
	// messageRunStopped explains a repository left alone because the run ended.
	messageRunStopped = "run stopped"
	// messageDirtyTree is the stable explanation for staged, unstaged, or untracked files.
	messageDirtyTree = "working tree has staged, unstaged or untracked changes"
	// messageDetachedHead tells the user to leave an unpinned detached HEAD alone.
	messageDetachedHead = "detached HEAD; select a tracked branch explicitly"
	// messagePlanAdmissible means cached refs allow a later fast-forward or detach.
	messagePlanAdmissible = "fast-forward or detach is admissible"
)
