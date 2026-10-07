package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/oshokin/release-align/internal/app"
	"github.com/oshokin/release-align/internal/logger"
)

// newStatusCommand builds the offline workspace status subcommand.
func newStatusCommand(cfg *app.Config) *cobra.Command {
	command := &cobra.Command{
		Use:   "status",
		Short: "Check a workspace locally without fetching or switching",
		Long: "status reads cached refs and worktrees. Without --remote it does not call GitLab. " +
			"--remote adds the group inventory after the local check and does not clone. " +
			"--dry-run is rejected, including together with --remote.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return &commandError{
					code:  exitUsage,
					cause: err,
				}
			}

			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := applyCommandEnv(cmd, cfg); err != nil {
				return usageCommand(cmd, cfg, app.ModeStatus, err)
			}

			return runWorkspaceCommand(cmd, cfg, app.ModeStatus)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	command.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &commandError{
			code:  exitUsage,
			cause: err,
		}
	})
	bindFlags(command, cfg)

	return command
}

// runWorkspaceCommand loads the inventory and runs sync or status.
func runWorkspaceCommand(cmd *cobra.Command, cfg *app.Config, mode string) error {
	if err := cfg.Validate(); err != nil {
		return usageCommand(cmd, cfg, mode, err)
	}

	if err := cfg.ValidateWorkspace(mode); err != nil {
		return usageCommand(cmd, cfg, mode, err)
	}

	spec, err := app.LoadWorkspace(cfg.WorkspaceFile)
	if err != nil {
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
			cause: writeErr,
		}
	}

	return &commandError{
		code:  exitUsage,
		cause: err,
	}
}

// executeWorkspace runs the workspace and writes the report.
func executeWorkspace(cmd *cobra.Command, cfg *app.Config, spec *app.WorkspaceSpec, mode string) error {
	logOut := cmd.OutOrStdout()
	if cfg.JSON() {
		logOut = cmd.ErrOrStderr()
	}

	level, _ := logger.ParseLogLevel(cfg.LogLevel)
	log := logger.NewWithWriter(level, logOut)

	defer func() {
		syncErr := log.Sync()
		if syncErr != nil {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), syncErr)
		}
	}()

	cfg.SetProgress(cmd.ErrOrStderr())
	ctx := logger.ToContext(cmd.Context(), log)
	report, runErr := app.RunWorkspace(ctx, cfg, spec, mode)

	writeErr := error(nil)
	if cfg.JSON() {
		writeErr = app.WriteWorkspaceReport(cmd.OutOrStdout(), report)
	}

	if writeErr != nil {
		return &commandError{
			code:  exitFailed,
			cause: writeErr,
		}
	}

	if !cfg.JSON() {
		if writeErr = app.WriteRemoteInventory(cmd.OutOrStdout(), cfg, report); writeErr != nil {
			return &commandError{
				code:  exitFailed,
				cause: writeErr,
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
