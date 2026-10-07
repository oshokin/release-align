// Package cmd implements the release-align command line.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/oshokin/release-align/internal/app"
	"github.com/oshokin/release-align/internal/logger"
)

// commandError is a CLI failure that carries the process exit code.
type commandError struct {
	// code is the process exit code.
	code int
	// cause is the underlying CLI error.
	cause error
}

const (
	// exitOK is the POSIX status of a successful run.
	exitOK = 0
	// exitFailed is the status of a finished run in which a repository failed.
	exitFailed = 1
	// exitUsage is the shell status for invalid arguments, flags, or configuration.
	exitUsage = 2
	// exitNotReady means the selected workspace does not match the requested revisions.
	exitNotReady = 3
	// exitInterrupted is the shell status for SIGINT: 128 plus the signal number.
	exitInterrupted = 128 + int(syscall.SIGINT)
)

// NewRootCommand constructs an independent Cobra command for each invocation.
func NewRootCommand(out, errOut io.Writer) *cobra.Command {
	cfg := app.DefaultConfig()
	// Delay environment errors until RunE so help, version and completion stay usable.
	envErr := cfg.ApplyEnv(os.Getenv)
	root := &cobra.Command{
		Use:           "release-align",
		Short:         "Safely switch and fast-forward a directory of Git repositories",
		Long:          "Without --workspace, priority is the release branch, a branch containing the version commit, the version tag, then the current branch. With --workspace, workspace mode uses exact targets and fails when selected projects are not ready.",
		Version:       fullVersion(),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return &commandError{code: exitUsage, cause: err}
			}

			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCommand(cmd, cfg, envErr)
		},
	}
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetVersionTemplate("{{.Version}}\n")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &commandError{code: exitUsage, cause: err}
	})

	bindFlags(root, cfg)
	root.AddCommand(newVersionCommand())
	root.AddCommand(newStatusCommand(cfg, envErr))

	return root
}

// Execute runs the root command and maps its error to a process exit code.
func Execute(args []string, out, errOut io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := NewRootCommand(out, errOut)
	root.SetArgs(args)

	err := root.ExecuteContext(ctx)
	if err == nil {
		return exitOK
	}

	if ctx.Err() != nil {
		return interruptedExit(err, errOut)
	}

	_, _ = fmt.Fprintln(errOut, err)

	if commandErr, ok := errors.AsType[*commandError](err); ok {
		return commandErr.code
	}

	return exitUsage
}

// interruptedExit prints a non-cancel error and returns the interrupt exit code.
func interruptedExit(err error, errOut io.Writer) int {
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		_, _ = fmt.Fprintln(errOut, err)
	}

	return exitInterrupted
}

// runCommand loads the version table, runs the update, and maps failures to exit codes.
func runCommand(cmd *cobra.Command, cfg *app.Config, envErr error) error {
	if envErr != nil {
		return &commandError{code: exitUsage, cause: envErr}
	}

	if cfg.WorkspaceFile != "" || len(cfg.Repositories) > 0 || len(cfg.Groups) > 0 || cfg.JSON() {
		return runWorkspaceCommand(cmd, cfg, app.ModeSync)
	}

	if err := cfg.Validate(); err != nil {
		return &commandError{code: exitUsage, cause: err}
	}

	manifest, err := app.LoadManifest(cfg.VersionsFile)
	if err != nil {
		return &commandError{code: exitUsage, cause: err}
	}

	level, _ := logger.ParseLogLevel(cfg.LogLevel)
	log := logger.NewWithWriter(level, cmd.OutOrStdout())
	defer func() {
		// The console sink does not fsync stdout. A real Sync error is still reported.
		syncErr := log.Sync()
		if syncErr != nil {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), syncErr)
		}
	}()

	ctx := logger.ToContext(cmd.Context(), log)
	summary, err := app.Run(ctx, cfg, manifest)
	app.LogNotUpdated(ctx, summary)
	logger.Infof(
		ctx,
		"Summary: total=%d updated=%d planned=%d skipped=%d failed=%d canceled=%d",
		summary.Updated+summary.Planned+summary.Skipped+summary.Failed+summary.Canceled,
		summary.Updated,
		summary.Planned,
		summary.Skipped,
		summary.Failed,
		summary.Canceled,
	)

	if err != nil {
		return &commandError{code: exitFailed, cause: err}
	}

	return nil
}

// bindFlags registers the root command flags on cfg.
func bindFlags(root *cobra.Command, cfg *app.Config) {
	flags := root.Flags()
	flags.StringVar(&cfg.BaseDir, "base-dir", cfg.BaseDir, "repository root directory")
	flags.StringVar(&cfg.Branch, "branch", cfg.Branch, "preferred origin branch")
	flags.StringVar(
		&cfg.VersionsFile,
		"versions-file",
		cfg.VersionsFile,
		"JSON map of service to TAG:COMMIT; overrides built-in versions",
	)
	flags.IntVar(&cfg.Depth, "depth", cfg.Depth, "exact repository depth below base-dir")
	flags.IntVarP(&cfg.Jobs, "jobs", "j", cfg.Jobs, "parallel repositories (1..64)")
	flags.IntVar(&cfg.Attempts, "attempts", cfg.Attempts, "total origin-check attempts, including the first")
	flags.BoolVarP(
		&cfg.DryRun,
		"dry-run",
		"n",
		cfg.DryRun,
		"offline preview using cached refs; do not change repositories",
	)
	flags.StringVar(
		&cfg.Local,
		"local",
		cfg.Local,
		"what to do with local commits and edits: skip, keep, or reset",
	)
	flags.StringVarP(&cfg.LogLevel, "log-level", "l", cfg.LogLevel, "log level: debug, info, warn, error (any case)")
	flags.StringVar(&cfg.WorkspaceFile, "workspace", "", "Explicit workspace JSON; exact targets, no fallback")
	flags.StringArrayVar(&cfg.Repositories, "repo", nil, "Select a workspace project by its relative path; repeatable")
	flags.StringArrayVar(&cfg.Groups, "group", nil, "Select a workspace group; repeatable")
	flags.StringVar(&cfg.Output, "output", cfg.Output, "Output format: text or json")
	flags.DurationVar(&cfg.ProbeTimeout, "probe-timeout", cfg.ProbeTimeout, "timeout per git ls-remote probe")
	flags.DurationVar(&cfg.RetryDelay, "retry-delay", cfg.RetryDelay, "delay between failed network probes")
	flags.DurationVar(&cfg.FetchTimeout, "fetch-timeout", cfg.FetchTimeout, "timeout per fetch")
	flags.DurationVar(&cfg.LocalTimeout, "local-timeout", cfg.LocalTimeout, "timeout per local Git command")
}

// Error returns the underlying CLI error text.
func (e *commandError) Error() string {
	return e.cause.Error()
}

// Unwrap returns the underlying CLI error.
func (e *commandError) Unwrap() error {
	return e.cause
}
