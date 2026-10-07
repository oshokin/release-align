package app

import (
	"context"
	"errors"
	"os/exec"

	"github.com/oshokin/release-align/internal/gitter"
	"github.com/oshokin/release-align/internal/logger"
)

// RunWorkspace prepares or inspects the selected projects.
// mode is sync or status. The returned report is non-nil when spec is non-nil.
func RunWorkspace(ctx context.Context, cfg *Config, spec *WorkspaceSpec, mode string) (*WorkspaceReport, error) {
	report := newWorkspaceReport(cfg, spec, mode)
	if cfg == nil || spec == nil {
		report.Errors = []string{errConfigNil.Error()}

		return report, errConfigNil
	}

	selected, err := spec.SelectProjects(cfg.Repositories, cfg.Groups)
	if err != nil {
		report.Errors = []string{err.Error()}

		return report, err
	}

	items := newWorkspaceItems(spec, selected)
	report.Rows = workspaceRows(items)
	freshness, runErr := runWorkspaceItems(ctx, cfg, items, mode)

	return finishWorkspace(ctx, report, items, freshness, runErr)
}

// ExitCodeForWorkspace maps a workspace run error to a process status.
// Cancellation is left to the caller so a signal can still win.
func ExitCodeForWorkspace(err error) int {
	switch {
	case err == nil:
		return 0
	case workspaceUsage(err):
		return 2
	case errors.Is(err, errWorkspaceNotReady):
		return 3
	default:
		return 1
	}
}

// LogWorkspaceReport writes a human summary. Text mode also lists every repository.
func LogWorkspaceReport(ctx context.Context, report *WorkspaceReport, text bool) {
	if report == nil {
		return
	}

	if text {
		for _, row := range report.Rows {
			logger.Infof(
				ctx,
				"%s outcome=%s ready=%t reason=%s %s",
				row.Path,
				row.Outcome,
				row.Ready,
				row.ReasonCode,
				row.Message,
			)
		}
	}

	logger.Infof(
		ctx,
		"workspace ready=%t coverage=%t expected=%d inventory=%d scope=%s freshness=%s mode=%s",
		report.Ready,
		report.Coverage,
		report.ExpectedCount,
		report.InventoryCount,
		report.Scope,
		report.Freshness,
		report.Mode,
	)
}

// newWorkspaceReport builds the report shell before repositories are visited.
func newWorkspaceReport(cfg *Config, spec *WorkspaceSpec, mode string) *WorkspaceReport {
	report := &WorkspaceReport{
		SchemaVersion: 1,
		Mode:          mode,
		Freshness:     freshnessCached,
		Errors:        []string{},
		Rows:          []*WorkspaceRow{},
	}

	if spec != nil {
		report.Release = spec.Release
		report.InventoryCount = len(spec.Projects)
	}

	if cfg == nil {
		return report
	}

	report.Scope = workspaceScope(cfg)
	report.DryRun = cfg.DryRun && mode == ModeSync

	if report.DryRun {
		report.Mode = ModePlan
	}

	return report
}

// workspaceRows returns the row pointers in selection order.
func workspaceRows(items []*workspaceItem) []*WorkspaceRow {
	rows := make([]*WorkspaceRow, len(items))

	for i, item := range items {
		rows[i] = item.row
	}

	return rows
}

// runWorkspaceItems binds directories and either syncs or reads cached state.
func runWorkspaceItems(
	ctx context.Context,
	cfg *Config,
	items []*workspaceItem,
	mode string,
) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return freshnessCached, err
	}

	runner := &runner{
		cfg: cfg,
		git: &gitter.Client{
			LocalTimeout: cfg.LocalTimeout,
			NoLazyFetch:  mode == ModeStatus || cfg.DryRun,
		},
		beforeWorkspaceCheckout: cfg.beforeCheckout,
	}

	if cfg.afterCheckout != nil {
		runner.afterWorkspaceCheckout = func(repo *repository) {
			cfg.afterCheckout(repo.path)
		}
	}

	if err := runner.useProbeRetries(); err != nil {
		return freshnessCached, err
	}

	if err := runner.bindWorkspaceDirs(ctx, items); err != nil {
		return freshnessCached, err
	}

	if mode != ModeStatus && !cfg.DryRun {
		return runner.syncWorkspace(ctx, items)
	}

	if err := runner.checkWorkspaceRefs(ctx, items); err != nil {
		return freshnessCached, err
	}

	return freshnessCached, runner.readCached(ctx, items, cfg.DryRun && mode == ModeSync)
}
