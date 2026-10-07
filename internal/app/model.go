package app

import (
	"context"
	"time"

	"github.com/oshokin/release-align/internal/retry"
)

type (
	// Summary counts repositories by the outcome of one run.
	Summary struct {
		// Updated is how many repositories were fast-forwarded or detached.
		Updated int
		// Planned is how many dry-run repositories would have changed.
		Planned int
		// Skipped is how many repositories were left untouched.
		Skipped int
		// Failed is how many repositories stopped on an error.
		Failed int
		// Canceled is how many repositories were stopped because the run ended.
		Canceled int
		// NotUpdated lists repositories that were skipped, failed, or canceled after work started.
		NotUpdated []*RepositoryReport
	}

	// RepositoryReport is one repository the run did not update.
	RepositoryReport struct {
		// Path is the slash-separated path from the base directory.
		Path string
		// Status is skipped, failed, or canceled.
		Status string
		// Reason explains why the repository was not updated.
		Reason string
	}

	// repository is a discovered Git working tree and the paths used to lock it.
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

	// result is the status and message recorded for one repository.
	result struct {
		// repo is the repository this outcome belongs to.
		repo *repository
		// status is updated, planned, skipped, failed, or canceled.
		status string
		// message explains the outcome.
		message string
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
		// manifest is the service version table.
		manifest Manifest
		// probeRetry retries origin reachability checks.
		probeRetry *retry.Engine
		// beforeWorkspaceCheckout runs once after a clean plan and before the first switch.
		beforeWorkspaceCheckout func()
		// afterWorkspaceCheckout runs after one switch and before that repository is read back.
		afterWorkspaceCheckout func(*repository)
	}
)

const (
	// statusUpdated marks a repository that was fast-forwarded or detached.
	statusUpdated = "updated"
	// statusPlanned marks a dry-run repository that would have changed.
	statusPlanned = "planned"
	// statusSkipped marks a repository left untouched.
	statusSkipped = "skipped"
	// statusFailed marks a repository that stopped on an error.
	statusFailed = "failed"
	// statusCanceled marks a repository stopped because the run ended.
	statusCanceled = "canceled"
	// messageRunStopped explains a repository left alone because the run ended.
	messageRunStopped = "run stopped"
	// messageDirtyTree is the stable explanation for staged, unstaged, or untracked files.
	messageDirtyTree = "working tree has staged, unstaged or untracked changes"
	// messageDetachedHead tells the user to leave an unpinned detached HEAD alone.
	messageDetachedHead = "detached HEAD; select a tracked branch explicitly"
	// messagePlanAdmissible means cached refs allow a later fast-forward or detach.
	messagePlanAdmissible = "fast-forward or detach is admissible"
	// messageIdleCancel marks a repository that was still queued when the run stopped.
	messageIdleCancel = "not started"
)
