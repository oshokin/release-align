package app

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/oshokin/release-align/internal/gitter"
	"github.com/oshokin/release-align/internal/logger"
	"github.com/oshokin/release-align/internal/retry"
)

// Run handles independent repositories concurrently. cfg and manifest must remain
// unchanged until it returns; all workers share their read-only configuration.
func Run(parent context.Context, cfg *Config, manifest Manifest) (*Summary, error) {
	summary := new(Summary)
	if cfg == nil {
		return summary, errConfigNil
	}

	if err := cfg.Validate(); err != nil {
		return summary, err
	}

	if _, err := exec.LookPath("git"); err != nil {
		return summary, err
	}

	gitClient := &gitter.Client{LocalTimeout: cfg.LocalTimeout}
	r := &runner{cfg: cfg, git: gitClient, manifest: manifest}

	if err := r.useProbeRetries(); err != nil {
		return summary, err
	}

	ctx, cancel := context.WithCancelCause(parent)

	defer cancel(nil)

	if _, err := r.git.Local(ctx, cfg.BaseDir, "check-ref-format", "--branch", cfg.Branch); err != nil {
		return summary, fmt.Errorf("invalid branch or base directory: %w", err)
	}

	paths, err := discover(ctx, cfg.BaseDir, cfg.Depth)
	if err != nil {
		return summary, err
	}

	logger.Infof(ctx, "Found %d repositories; workers=%d; branch=%s", len(paths), cfg.Jobs, cfg.Branch)

	if len(paths) == 0 {
		return summary, nil
	}

	if !cfg.DryRun {
		unlock, lockErr := lockBase(cfg.BaseDir)
		if lockErr != nil {
			return summary, lockErr
		}
		defer unlock()
	} else {
		logger.Warn(ctx, "Offline dry-run: using cached refs; remote changes and permissions are not checked")
	}

	repos, err := r.prepare(ctx, paths, summary)
	if err != nil {
		return summary, err
	}

	if !cfg.DryRun {
		repos, err = r.preflight(ctx, repos, summary)
		if err != nil {
			return summary, err
		}
	}

	r.processAll(ctx, cancel, repos, summary)

	if ctx.Err() != nil {
		return summary, context.Cause(ctx)
	}

	if summary.Failed > 0 {
		return summary, fmt.Errorf("%d %w", summary.Failed, errRepositoriesFailed)
	}

	return summary, nil
}

// LogNotUpdated writes the repositories this run did not update, sorted by path.
func LogNotUpdated(ctx context.Context, summary *Summary) {
	if summary == nil || len(summary.NotUpdated) == 0 {
		return
	}

	items := slices.Clone(summary.NotUpdated)
	slices.SortStableFunc(items, func(a, b *RepositoryReport) int {
		return strings.Compare(a.Path, b.Path)
	})

	logger.Warn(ctx, "Repositories not updated:")

	for _, item := range items {
		logger.Warnf(ctx, "%s [%s] %s", item.Path, item.Status, item.Reason)
	}
}

// useProbeRetries attaches the origin-check retry engine when more than one attempt is allowed.
func (r *runner) useProbeRetries() error {
	if r.cfg.Attempts <= 1 {
		return nil
	}

	engineCfg := &retry.EngineConfig{
		MaxRetries:  uint64(r.cfg.Attempts - 1),
		DelayPolicy: retry.NewRandomRangePolicy(r.cfg.RetryDelay, r.cfg.RetryDelay),
		IsRetryable: r.retryableProbe,
	}

	engine, err := retry.NewEngine(engineCfg)
	if err != nil {
		return err
	}

	r.probeRetry = engine

	return nil
}

// record stores one repository outcome and writes its log line.
func (r *runner) record(ctx context.Context, summary *Summary, res *result) {
	ctx = logger.WithKV(ctx, "repo", res.repo.relative)
	switch res.status {
	case statusUpdated:
		summary.Updated++
	case statusPlanned:
		summary.Planned++
	case statusSkipped:
		summary.Skipped++
		r.keepNotUpdated(summary, res)

		logger.Warnf(ctx, "skipped: %s", res.message)

		return
	case statusFailed:
		summary.Failed++
		r.keepNotUpdated(summary, res)

		logger.Errorf(ctx, "failed: %s", res.message)

		return
	case statusCanceled:
		summary.Canceled++

		if res.message == messageIdleCancel {
			return
		}

		r.keepNotUpdated(summary, res)
		logger.Warnf(ctx, "canceled: %s", res.message)

		return
	}

	logger.Infof(ctx, "%s: %s", res.status, res.message)
}

// keepNotUpdated remembers a repository for the end-of-run list.
func (r *runner) keepNotUpdated(summary *Summary, res *result) {
	report := &RepositoryReport{
		Path:   res.repo.relative,
		Status: res.status,
		Reason: res.message,
	}

	summary.NotUpdated = append(summary.NotUpdated, report)
}

// endpoint reduces an origin URL to the host used for one preflight check.
func (r *runner) endpoint(remote string) string {
	if u, err := url.Parse(remote); err == nil && u.Host != "" {
		return u.Scheme + "://" + u.Host
	}

	if !filepath.IsAbs(remote) && strings.Contains(remote, ":") {
		return "ssh:" + strings.SplitN(remote, ":", 2)[0]
	}

	return "local:" + remote
}
