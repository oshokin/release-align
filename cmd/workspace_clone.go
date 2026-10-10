package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/oshokin/release-align/internal/app"
)

// workspaceCloneCommand owns the flags of workspace clone.
type workspaceCloneCommand struct {
	// cfg is the shared program configuration. The root flag writes its log level here.
	cfg *app.Config
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
func newWorkspaceCloneCommand(cfg *app.Config) *cobra.Command {
	options := &app.WorkspaceCloneOptions{
		Attempts:       cfg.Attempts,
		ProbeTimeout:   cfg.ProbeTimeout,
		RetryDelay:     cfg.RetryDelay,
		FetchTimeout:   cfg.FetchTimeout,
		CatalogTimeout: cfg.CatalogTimeout,
		CatalogBudget:  cfg.CatalogBudget,
		LocalTimeout:   cfg.LocalTimeout,
		CloneTimeout:   cfg.CloneTimeout,
		Output:         cfg.Output,
	}
	handler := &workspaceCloneCommand{
		cfg:     cfg,
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
	flags.BoolVar(
		&options.IncludeArchived,
		"include-archived",
		false,
		"download archived projects; does not update them later",
	)
	flags.StringVar(&options.Output, "output", options.Output, "output format: text or json")
	flags.IntVar(&options.Attempts, "attempts", options.Attempts, "transport checks in total, including the first")
	flags.DurationVar(&options.ProbeTimeout, "probe-timeout", options.ProbeTimeout, "timeout of one git ls-remote")
	flags.DurationVar(&options.RetryDelay, "retry-delay", options.RetryDelay, "pause between failed transport checks")
	flags.DurationVar(&options.FetchTimeout, "fetch-timeout", options.FetchTimeout, "timeout per fetch")
	flags.DurationVar(
		&options.CatalogTimeout,
		"catalog-timeout",
		options.CatalogTimeout,
		"timeout of one GitLab projects page",
	)
	flags.DurationVar(
		&options.CatalogBudget,
		"catalog-budget",
		options.CatalogBudget,
		"deadline for one GitLab group listing",
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
		return c.failUsage(command, err)
	}

	c.options.Attempts = cfg.Attempts
	c.options.ProbeTimeout = cfg.ProbeTimeout
	c.options.RetryDelay = cfg.RetryDelay
	c.options.FetchTimeout = cfg.FetchTimeout
	c.options.CatalogTimeout = cfg.CatalogTimeout
	c.options.CatalogBudget = cfg.CatalogBudget
	c.options.LocalTimeout = cfg.LocalTimeout
	c.options.CloneTimeout = cfg.CloneTimeout

	c.options.SetTimeoutLocks(timeoutLocks(command))
	c.options.SetProgress(command.ErrOrStderr())

	spec, err := app.LoadWorkspace(c.options.WorkspaceFile)
	if err != nil {
		return c.fail(command, err)
	}

	if err = applyCommandEnv(command, c.cfg); err != nil {
		return c.failUsage(command, err)
	}

	if err = c.cfg.TakeLogLevel(durationLocked(command, "log-level", "LOG_LEVEL"), spec.LogLevel); err != nil {
		return c.failUsage(command, err)
	}

	ctx := withCommandLog(command, c.cfg.LogLevel)
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
			cause: errors.Join(err, writeErr),
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
			cause: errors.Join(err, writeErr),
		}

		return failed
	}

	return c.cloneCommandError(err)
}

// failUsage writes the JSON error for a flag or environment mistake and keeps exit status 2.
func (c *workspaceCloneCommand) failUsage(command *cobra.Command, err error) error {
	if c.options.Output == cloneOutputJSON {
		report := &app.CloneReport{
			SchemaVersion: 1,
			Error:         err.Error(),
		}

		writeErr := c.writeCloneReport(command, report)
		if writeErr != nil {
			failed := &commandError{
				code:  exitFailed,
				cause: errors.Join(err, writeErr),
			}

			return failed
		}
	}

	usage := &commandError{
		code:  exitUsage,
		cause: err,
	}

	return usage
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
		return c.writeCloneJSON(command.OutOrStdout(), report)
	}

	return c.formatCloneReport(command.OutOrStdout(), report)
}

// formatCloneReport writes the text summary.
func (c *workspaceCloneCommand) formatCloneReport(w io.Writer, report *app.CloneReport) error {
	if w == nil || report == nil {
		return nil
	}

	state := reportCompleted
	if report.Error != "" {
		state = "incomplete"
	}

	if _, err := fmt.Fprintf(w, "Clone: %s.\n", state); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(
		w,
		"Downloaded: %d; reused: %d; added to workspace: %d.\n",
		report.Cloned,
		report.Reused,
		report.Added,
	); err != nil {
		return err
	}

	if report.Failed > 0 {
		if _, err := fmt.Fprintf(w, "Clone attempts failed: %d.\n", report.Failed); err != nil {
			return err
		}
	}

	if report.SkippedEmpty > 0 {
		if _, err := fmt.Fprintf(w, "Skipped, no default branch: %d.\n", report.SkippedEmpty); err != nil {
			return err
		}
	}

	if report.Error != "" {
		return c.writeCloneFailure(w, report)
	}

	if _, err := fmt.Fprintln(
		w,
		"Clones use their remote default branches; release alignment has not run.",
	); err != nil {
		return err
	}

	if report.Next == "" {
		return nil
	}

	_, err := fmt.Fprintf(w, "Next:\n  %s\n", report.Next)

	return err
}

// writeCloneFailure separates a saved checkout from a workspace file that was not updated.
// Recovery replaces Next, because alignment is not the first remaining step.
func (*workspaceCloneCommand) writeCloneFailure(w io.Writer, report *app.CloneReport) error {
	if report.Recovery == "" {
		return nil
	}

	if _, err := fmt.Fprintln(
		w,
		"Workspace file was not updated. Downloaded directories were kept.",
	); err != nil {
		return err
	}

	_, err := fmt.Fprintf(w, "Recovery:\n  %s\n", report.Recovery)

	return err
}

// writeCloneJSON writes one JSON document.
func (*workspaceCloneCommand) writeCloneJSON(w io.Writer, report *app.CloneReport) error {
	if report == nil {
		return errNilReport
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")

	return enc.Encode(report)
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
