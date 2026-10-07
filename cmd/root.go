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
	root := &cobra.Command{
		Use:   "release-align",
		Short: "Switch local Git clones to the revisions in a workspace file",
		Long: "Requires --workspace. Each selected project is moved to its exact branch, tag, or commit. " +
			"The run fails when a selected project is not ready. " +
			"--remote asks GitLab for the configured groups after that and does not clone.",
		Version:       fullVersion(),
		SilenceUsage:  true,
		SilenceErrors: true,
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
			return runCommand(cmd, cfg)
		},
	}
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetVersionTemplate("{{.Version}}\n")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &commandError{
			code:  exitUsage,
			cause: err,
		}
	})

	bindFlags(root, cfg)
	root.AddCommand(newVersionCommand())
	root.AddCommand(newStatusCommand(cfg))
	root.AddCommand(newWorkspaceCommand())

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

// runCommand loads the workspace and syncs the selected projects.
func runCommand(cmd *cobra.Command, cfg *app.Config) error {
	if err := applyCommandEnv(cmd, cfg); err != nil {
		return usageCommand(cmd, cfg, app.ModeSync, err)
	}

	return runWorkspaceCommand(cmd, cfg, app.ModeSync)
}

// bindFlags registers the root command flags on cfg.
func bindFlags(root *cobra.Command, cfg *app.Config) {
	flags := root.Flags()
	flags.StringVar(&cfg.BaseDir, "base-dir", cfg.BaseDir, "repository root directory")
	flags.StringVar(&cfg.Branch, "branch", cfg.Branch, "replace default_branch; applied only when this flag is set")
	flags.IntVarP(&cfg.Jobs, "jobs", "j", cfg.Jobs, "parallel repositories (1..64)")
	flags.IntVar(&cfg.Attempts, "attempts", cfg.Attempts, "total origin-check attempts, including the first")
	flags.BoolVarP(
		&cfg.DryRun,
		"dry-run",
		"n",
		cfg.DryRun,
		"offline preview using cached refs; do not change repositories",
	)
	flags.StringVarP(&cfg.LogLevel, "log-level", "l", cfg.LogLevel, "log level: debug, info, warn, error (any case)")
	flags.StringVar(&cfg.WorkspaceFile, "workspace", "", "Workspace JSON; exact targets, required")
	flags.StringArrayVar(&cfg.Repositories, "repo", nil, "Select a workspace project by its relative path; repeatable")
	flags.StringArrayVar(&cfg.Groups, "group", nil, "Select a workspace group; repeatable")
	flags.StringVar(&cfg.Output, "output", cfg.Output, "Output format: text or json")
	flags.BoolVar(
		&cfg.Remote,
		"remote",
		false,
		"After the local operation, compare GitLab groups with the disk and workspace",
	)
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
