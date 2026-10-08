package cmd

import (
	"context"
	"errors"

	"github.com/spf13/cobra"

	"github.com/oshokin/release-align/internal/app"
)

// workspaceCloneCommand owns the flags of workspace clone.
type workspaceCloneCommand struct {
	// options are the parsed clone flags.
	options *app.WorkspaceCloneOptions
}

const (
	// cloneOutputText is the human-readable clone report.
	cloneOutputText = "text"
	// cloneOutputJSON is the machine-readable clone report.
	cloneOutputJSON = "json"
)

// errCloneOutput means --output is neither text nor json.
var errCloneOutput = errors.New("output must be text or json")

// newWorkspaceCloneCommand downloads missing projects and registers them.
func newWorkspaceCloneCommand() *cobra.Command {
	defaults := app.DefaultConfig()
	options := &app.WorkspaceCloneOptions{
		Attempts:     defaults.Attempts,
		ProbeTimeout: defaults.ProbeTimeout,
		RetryDelay:   defaults.RetryDelay,
		FetchTimeout: defaults.FetchTimeout,
		LocalTimeout: defaults.LocalTimeout,
		CloneTimeout: defaults.CloneTimeout,
		Output:       defaults.Output,
	}
	handler := &workspaceCloneCommand{
		options: options,
	}
	command := &cobra.Command{
		Use:   "clone",
		Short: "Clone selected GitLab projects and add them to the workspace",
		Long: "Requires either repeatable --repo or --all.\n" +
			"--workspace defaults to release-align.yml. An omitted --base-dir uses release-align.base-dir from that file.\n" +
			"--all uses the gitlab.groups saved in the workspace, including subgroups, not every project on the server.\n" +
			"An existing matching checkout is reused. Occupied paths are not replaced.\n" +
			"Clones stay on the remote default branch; run release-align workspace sync to align the release.\n\n" +
			"  release-align workspace clone --workspace ./release-align.yml --base-dir \"$RELEASE_ALIGN_BASE_DIR\" --repo lamiona/search/new-indexer\n" +
			"  release-align workspace clone --workspace ./release-align.yml --base-dir \"$RELEASE_ALIGN_BASE_DIR\" --all",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          handler.run,
	}
	command.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &commandError{
			code:  exitUsage,
			cause: err,
		}
	})
	flags := command.Flags()
	flags.StringVar(&options.WorkspaceFile, "workspace", defaultWorkspaceFile, "workspace YAML (.yml or .yaml)")
	flags.StringVar(
		&options.BaseDir,
		"base-dir",
		"",
		"directory that will contain <namespace> clones; omitted flag uses the file",
	)
	flags.StringArrayVar(&options.Repos, "repo", nil, "exact GitLab path_with_namespace; repeatable")
	flags.BoolVar(&options.All, "all", false, "clone and include every project in gitlab.groups")
	flags.StringVar(&options.Output, "output", options.Output, "output format: text or json")
	flags.IntVar(&options.Attempts, "attempts", options.Attempts, "transport checks in total, including the first")
	flags.DurationVar(&options.ProbeTimeout, "probe-timeout", options.ProbeTimeout, "timeout of one git ls-remote")
	flags.DurationVar(&options.RetryDelay, "retry-delay", options.RetryDelay, "pause between failed transport checks")
	flags.DurationVar(
		&options.FetchTimeout,
		"fetch-timeout",
		options.FetchTimeout,
		"budget for the GitLab catalog request",
	)
	flags.DurationVar(&options.LocalTimeout, "local-timeout", options.LocalTimeout, "timeout of one local Git command")
	flags.DurationVar(&options.CloneTimeout, "clone-timeout", options.CloneTimeout, "timeout of one git clone")

	return command
}

// run lists the saved GitLab scope, clones what is missing, and updates the workspace file.
func (c *workspaceCloneCommand) run(command *cobra.Command, _ []string) error {
	if c.options.Output != cloneOutputText && c.options.Output != cloneOutputJSON {
		return &commandError{
			code:  exitUsage,
			cause: errCloneOutput,
		}
	}

	choice := &baseDirChoice{
		command:   command,
		workspace: c.options.WorkspaceFile,
		current:   c.options.BaseDir,
	}

	base, err := savedBaseDir(choice)
	if err != nil {
		return c.fail(command, err)
	}

	c.options.BaseDir = base

	cfg := c.cloneEnvConfig()
	if err = applyCommandEnv(command, cfg); err != nil {
		return &commandError{
			code:  exitUsage,
			cause: err,
		}
	}

	c.options.Attempts = cfg.Attempts
	c.options.ProbeTimeout = cfg.ProbeTimeout
	c.options.RetryDelay = cfg.RetryDelay
	c.options.FetchTimeout = cfg.FetchTimeout
	c.options.LocalTimeout = cfg.LocalTimeout
	c.options.CloneTimeout = cfg.CloneTimeout

	c.options.SetTimeoutLocks(timeoutLocks(command))
	c.options.SetProgress(command.ErrOrStderr())

	ctx := withCommandLog(command, c.options.Output == cloneOutputJSON, cfg.LogLevel)
	report, err := app.CloneWorkspace(ctx, c.options)

	if err != nil && report != nil && report.Error == "" {
		report.Error = err.Error()
	}

	if c.options.Output == cloneOutputText && app.ExitCodeForWorkspace(err) == exitUsage {
		return c.cloneCommandError(err)
	}

	writeErr := c.writeCloneReport(command, report)
	if writeErr != nil {
		return &commandError{
			code:  exitFailed,
			cause: writeErr,
		}
	}

	return c.cloneCommandError(err)
}

// fail writes a JSON error when that format was requested, then returns the status.
func (c *workspaceCloneCommand) fail(command *cobra.Command, err error) error {
	if c.options.Output != cloneOutputJSON {
		return c.cloneCommandError(err)
	}

	report := &app.CloneReport{
		SchemaVersion: 1,
		Error:         err.Error(),
	}

	writeErr := c.writeCloneReport(command, report)
	if writeErr != nil {
		failed := &commandError{
			code:  exitFailed,
			cause: writeErr,
		}

		return failed
	}

	return c.cloneCommandError(err)
}

// cloneEnvConfig carries clone timeouts into the shared environment reader and back.
func (c *workspaceCloneCommand) cloneEnvConfig() *app.Config {
	cfg := app.DefaultConfig()
	cfg.Attempts = c.options.Attempts
	cfg.ProbeTimeout = c.options.ProbeTimeout
	cfg.RetryDelay = c.options.RetryDelay
	cfg.FetchTimeout = c.options.FetchTimeout
	cfg.LocalTimeout = c.options.LocalTimeout
	cfg.CloneTimeout = c.options.CloneTimeout
	cfg.Output = c.options.Output

	return cfg
}

// writeCloneReport writes JSON to stdout or the text summary.
func (c *workspaceCloneCommand) writeCloneReport(command *cobra.Command, report *app.CloneReport) error {
	if c.options.Output == cloneOutputJSON {
		return app.WriteCloneReport(command.OutOrStdout(), report)
	}

	return app.FormatCloneReport(command.OutOrStdout(), report)
}

// cloneCommandError maps clone failures onto process statuses.
func (*workspaceCloneCommand) cloneCommandError(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	if errors.Is(err, errBaseDirMissing) || errors.Is(err, errBaseDirChoice) {
		return &commandError{
			code:  exitUsage,
			cause: err,
		}
	}

	code := app.ExitCodeForWorkspace(err)
	if code == exitOK {
		code = exitFailed
	}

	return &commandError{
		code:  code,
		cause: err,
	}
}
