package app

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/oshokin/release-align/internal/gitlab"
	"github.com/oshokin/release-align/internal/gitter"
)

// workspaceItemsRun is one sync or status pass over the selected projects.
type workspaceItemsRun struct {
	// cfg is the invocation settings.
	cfg *Config
	// items are the selected projects.
	items []*workspaceItem
	// mode is sync or status.
	mode string
	// report receives the phase clock.
	report *WorkspaceReport
}

// RunWorkspace prepares or inspects the selected projects.
// mode is sync or status. The returned report is non-nil when spec is non-nil.
func RunWorkspace(ctx context.Context, cfg *Config, spec *WorkspaceSpec, mode string) (*WorkspaceReport, error) {
	report := newWorkspaceReport(cfg, spec, mode)
	if cfg == nil || spec == nil {
		report.Errors = []string{errConfigNil.Error()}

		return report, errConfigNil
	}

	if err := PrepareRemote(cfg, spec); err != nil {
		report.Errors = []string{err.Error()}
		report.RemoteInventory = remoteNote(spec, remoteStatusSkipped, "", err.Error())

		return report, err
	}

	selected, err := spec.SelectProjects(cfg.Repositories, cfg.Groups)
	if err != nil {
		report.Errors = []string{err.Error()}
		report.RemoteInventory = remoteNote(spec, remoteStatusSkipped, "", err.Error())

		return report, err
	}

	client := &gitter.Client{
		LocalTimeout: cfg.LocalTimeout,
		NoLazyFetch:  true,
	}

	listedProjects, err := listedCatalog(ctx, cfg, spec)
	if err != nil {
		report.Errors = []string{err.Error()}
		report.RemoteInventory = remoteNote(spec, remoteStatusFailed, "", err.Error())

		return report, err
	}

	lookup := &revisionLookup{
		git:      client,
		base:     cfg.BaseDir,
		spec:     spec,
		projects: selected,
		soft:     true,
	}

	if err = ResolveWorkspaceRevisions(ctx, lookup); err != nil {
		report.Errors = []string{err.Error()}

		return report, err
	}

	items := newWorkspaceItems(spec, selected)
	report.Rows = workspaceRows(items)
	report.Started = time.Now()
	run := &workspaceItemsRun{
		cfg:    cfg,
		items:  items,
		mode:   mode,
		report: report,
	}
	freshness, runErr := runWorkspaceItems(ctx, run)
	done := &workspaceFinish{
		report:    report,
		items:     items,
		freshness: freshness,
		runErr:    runErr,
	}
	report, runErr = finishWorkspace(ctx, done)

	check := &remoteCheck{
		cfg:      cfg,
		spec:     spec,
		report:   report,
		runErr:   runErr,
		projects: listedProjects,
		listed:   cfg.Remote,
	}

	return attachRemote(ctx, check)
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

// LogWorkspaceReport writes a human summary. Text mode also lists repository results.
func LogWorkspaceReport(ctx context.Context, report *WorkspaceReport, text bool) {
	if report == nil {
		return
	}

	if text {
		logTextRows(ctx, report.Rows)
	}

	logWorkspaceSummary(ctx, report)
}

// listedCatalog reads GitLab before checkout when --remote is set.
func listedCatalog(ctx context.Context, cfg *Config, spec *WorkspaceSpec) ([]*gitlab.Project, error) {
	if cfg == nil || !cfg.Remote {
		return nil, nil
	}

	projects, err := listRemoteProjects(ctx, cfg, spec.GitLab)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errRemoteInventory, err)
	}

	noteServerArchived(spec, projects)

	return projects, nil
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
func runWorkspaceItems(ctx context.Context, run *workspaceItemsRun) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return freshnessCached, err
	}

	client := &gitter.Client{
		LocalTimeout: run.cfg.LocalTimeout,
		NoLazyFetch:  run.mode == ModeStatus || run.cfg.DryRun,
	}
	runner := &runner{
		cfg:                     run.cfg,
		git:                     client,
		beforeWorkspaceCheckout: run.cfg.beforeCheckout,
		report:                  run.report,
	}

	if run.cfg.afterCheckout != nil {
		runner.afterWorkspaceCheckout = func(repo *repository) {
			run.cfg.afterCheckout(repo.path)
		}
	}

	if err := runner.useProbeRetries(); err != nil {
		return freshnessCached, err
	}

	if err := runner.bindWorkspaceDirs(ctx, run.items); err != nil {
		return freshnessCached, err
	}

	if run.mode != ModeStatus && !run.cfg.DryRun {
		return runner.syncWorkspace(ctx, run.items)
	}

	if err := runner.checkWorkspaceRefs(ctx, run.items); err != nil {
		return freshnessCached, err
	}

	return freshnessCached, runner.readCached(ctx, run.items, run.cfg.DryRun && run.mode == ModeSync)
}
