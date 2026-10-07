package cmd

import (
	"context"
	"errors"
	"time"

	"github.com/spf13/cobra"

	"github.com/oshokin/release-align/internal/app"
)

// workspaceCloneCommand owns the flags of workspace clone.
type workspaceCloneCommand struct {
	// options are the parsed clone flags.
	options *app.WorkspaceCloneOptions
}

// newWorkspaceCloneCommand downloads missing projects and registers them.
func newWorkspaceCloneCommand() *cobra.Command {
	defaults := app.DefaultConfig()
	options := &app.WorkspaceCloneOptions{
		Attempts:     defaults.Attempts,
		ProbeTimeout: defaults.ProbeTimeout,
		RetryDelay:   defaults.RetryDelay,
		FetchTimeout: defaults.FetchTimeout,
		LocalTimeout: defaults.LocalTimeout,
		CloneTimeout: 15 * time.Minute,
		Output:       defaults.Output,
	}
	handler := &workspaceCloneCommand{
		options: options,
	}
	command := &cobra.Command{
		Use:   "clone",
		Short: "Clone selected GitLab projects and add them to the workspace",
		Long: "Requires --workspace, --base-dir, and either repeatable --repo or --all.\n" +
			"--all uses the gitlab.groups saved in the workspace, including subgroups, not every project on the server.\n" +
			"An existing matching checkout is reused. Occupied paths are not replaced.\n" +
			"Clones stay on the remote default branch; run release-align to align the release.\n\n" +
			"  release-align workspace clone --workspace ./mailion.workspace.json --base-dir \"$BASE_DIR\" --repo mailion/search/new-indexer\n" +
			"  release-align workspace clone --workspace ./mailion.workspace.json --base-dir \"$BASE_DIR\" --all",
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
	flags.StringVar(&options.WorkspaceFile, "workspace", "", "workspace JSON (required)")
	flags.StringVar(&options.BaseDir, "base-dir", "", "directory that will contain <namespace> clones (required)")
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
	c.options.SetProgress(command.ErrOrStderr())
	report, err := app.CloneWorkspace(command.Context(), c.options)

	writeErr := writeCloneReport(command, c.options, report)
	if writeErr != nil {
		return &commandError{
			code:  exitFailed,
			cause: writeErr,
		}
	}

	return cloneCommandError(err)
}

// writeCloneReport writes JSON to stdout or the text summary.
func writeCloneReport(command *cobra.Command, opts *app.WorkspaceCloneOptions, report *app.CloneReport) error {
	if opts.Output == "json" {
		return app.WriteCloneReport(command.OutOrStdout(), report)
	}

	return app.FormatCloneReport(command.OutOrStdout(), report)
}

// cloneCommandError maps clone failures onto process statuses.
func cloneCommandError(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
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
