package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/oshokin/release-align/internal/app"
	"github.com/oshokin/release-align/internal/logger"
)

// reportCompleted is the text state of a finished operation.
const reportCompleted = "completed"

// errNilReport means a command was asked to print a nil report.
var errNilReport = errors.New("nil report")

// withCommandLog attaches a text logger to the command context.
// Diagnostics stay on stderr. The command result is written separately to stdout.
func withCommandLog(cmd *cobra.Command, level string) context.Context {
	parsed, _ := logger.ParseLogLevel(level)
	log := logger.NewWithWriter(parsed, cmd.ErrOrStderr())

	ctx := logger.ToContext(cmd.Context(), log)
	logger.InfoKV(ctx, "Starting workspace operation", "operation", cmd.Name())

	return ctx
}

// newSyncCommand moves the selected clones onto the workspace release.
func newSyncCommand(cfg *app.Config) *cobra.Command {
	command := &cobra.Command{
		Use:   "sync",
		Short: "Move selected clones onto the workspace release",
		Long: "Each selected project is moved to its exact branch, tag, or commit. " +
			"The run fails when a selected project is not ready. " +
			"--ignore-errors still checks out every repository that can move and leaves the blocked ones unchanged. " +
			"--workspace defaults to release-align.yml. " +
			"An omitted --base-dir uses release-align.base-dir from that file. " +
			"--remote asks GitLab for the configured groups after that and does not clone.",
		Args:          noPositionalArgs,
		RunE:          func(cmd *cobra.Command, _ []string) error { return runCommand(cmd, cfg) },
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	command.SetFlagErrorFunc(usageFlagError)
	bindFlags(command, cfg)
	command.Flags().BoolVar(
		&cfg.IgnoreErrors,
		"ignore-errors",
		false,
		"check out every repository that can move; leave blocked repositories unchanged",
	)

	return command
}

// newStatusCommand builds the offline workspace status subcommand.
func newStatusCommand(cfg *app.Config) *cobra.Command {
	command := &cobra.Command{
		Use:   "status",
		Short: "Check a workspace locally without fetching or switching",
		Long: "status reads cached refs and worktrees. Without --remote it does not call GitLab. " +
			"--remote adds the group inventory after the local check and does not clone. " +
			"--dry-run is rejected, including together with --remote.",
		Args: noPositionalArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := applyCommandEnv(cmd, cfg); err != nil {
				return usageCommand(cmd, cfg, app.ModeStatus, err)
			}

			return runWorkspaceCommand(cmd, cfg, app.ModeStatus)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	command.SetFlagErrorFunc(usageFlagError)
	bindFlags(command, cfg)

	return command
}

// runWorkspaceCommand loads the inventory and runs sync or status.
func runWorkspaceCommand(cmd *cobra.Command, cfg *app.Config, mode string) error {
	if err := cfg.ValidateSettings(); err != nil {
		return usageCommand(cmd, cfg, mode, err)
	}

	if err := cfg.ValidateWorkspace(mode); err != nil {
		return usageCommand(cmd, cfg, mode, err)
	}

	spec, err := app.LoadWorkspace(cfg.WorkspaceFile)
	if err != nil {
		return usageCommand(cmd, cfg, mode, err)
	}

	choice := &baseDirChoice{
		command:     cmd,
		current:     cfg.BaseDir,
		recorded:    spec.BaseDir,
		recordedSet: true,
	}

	base, err := savedBaseDir(choice)
	if err != nil {
		return usageCommand(cmd, cfg, mode, err)
	}

	cfg.BaseDir = base

	if err = cfg.TakeLogLevel(durationLocked(cmd, "log-level", "LOG_LEVEL"), spec.LogLevel); err != nil {
		return usageCommand(cmd, cfg, mode, err)
	}

	if err = cfg.Validate(); err != nil {
		return usageCommand(cmd, cfg, mode, err)
	}

	cfg.SetTimeoutLocks(timeoutLocks(cmd))

	if err = app.ApplySpecTimeouts(cfg, spec); err != nil {
		return usageCommand(cmd, cfg, mode, err)
	}

	if err = app.PrepareRemote(cfg, spec); err != nil {
		return usageCommand(cmd, cfg, mode, err)
	}

	if !cmd.Flags().Changed("branch") {
		return executeWorkspace(cmd, cfg, spec, mode)
	}

	spec, err = spec.WithDefaultBranch(cfg.Branch)
	if err != nil {
		return usageCommand(cmd, cfg, mode, err)
	}

	return executeWorkspace(cmd, cfg, spec, mode)
}

// usageCommand returns a usage error and writes a JSON envelope when requested.
func usageCommand(cmd *cobra.Command, cfg *app.Config, mode string, err error) error {
	if cfg == nil || !cfg.JSON() {
		return &commandError{
			code:  exitUsage,
			cause: err,
		}
	}

	report := &app.WorkspaceReport{
		SchemaVersion: 1,
		Mode:          mode,
		Freshness:     "cached",
		Errors:        []string{err.Error()},
		Rows:          []*app.WorkspaceRow{},
	}

	if cfg.DryRun && mode == app.ModeSync {
		report.DryRun = true
		report.Mode = app.ModePlan
	}

	writeErr := app.WriteWorkspaceReport(cmd.OutOrStdout(), report)
	if writeErr != nil {
		return &commandError{
			code:  exitFailed,
			cause: errors.Join(err, writeErr),
		}
	}

	return &commandError{
		code:  exitUsage,
		cause: err,
	}
}

// executeWorkspace runs the workspace and writes the report.
func executeWorkspace(cmd *cobra.Command, cfg *app.Config, spec *app.WorkspaceSpec, mode string) error {
	level, _ := logger.ParseLogLevel(cfg.LogLevel)
	log := logger.NewWithWriter(level, cmd.ErrOrStderr())

	defer func() {
		syncErr := log.Sync()
		if syncErr != nil {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), syncErr)
		}
	}()

	cfg.SetProgress(cmd.ErrOrStderr())
	ctx := logger.ToContext(cmd.Context(), log)
	logger.InfoKV(ctx, "Starting workspace operation", "operation", mode, "dry_run", cfg.DryRun)
	report, runErr := app.RunWorkspace(ctx, cfg, spec, mode)

	writeErr := error(nil)
	if cfg.JSON() {
		writeErr = app.WriteWorkspaceReport(cmd.OutOrStdout(), report)
	}

	if writeErr != nil {
		return &commandError{
			code:  exitFailed,
			cause: errors.Join(runErr, writeErr),
		}
	}

	if !cfg.JSON() {
		if writeErr = app.WriteRemoteInventory(cmd.OutOrStdout(), cfg, report); writeErr != nil {
			return &commandError{
				code:  exitFailed,
				cause: errors.Join(runErr, writeErr),
			}
		}
	}

	app.LogWorkspaceReport(ctx, report, !cfg.JSON())

	return mapWorkspaceError(runErr)
}

// mapWorkspaceError maps a workspace failure onto a command exit error.
func mapWorkspaceError(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	code := app.ExitCodeForWorkspace(err)
	if code == exitNotReady {
		return &commandError{
			code:  exitNotReady,
			cause: err,
		}
	}

	if code == exitUsage {
		return &commandError{
			code:  exitUsage,
			cause: err,
		}
	}

	return &commandError{
		code:  exitFailed,
		cause: err,
	}
}
