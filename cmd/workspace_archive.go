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

// workspaceArchiveCommand owns the flags of workspace archive.
type workspaceArchiveCommand struct {
	// cfg is the shared program configuration. The root flag writes its log level here.
	cfg *app.Config
	// options are the parsed archive flags.
	options *app.WorkspaceArchiveOptions
}

// errArchiveOutput means --output is neither text nor json.
var errArchiveOutput = errors.New("output must be text or json")

// newWorkspaceArchiveCommand packs workspace revisions into one ZIP.
func newWorkspaceArchiveCommand(cfg *app.Config) *cobra.Command {
	options := &app.WorkspaceArchiveOptions{
		Output:         cfg.Output,
		ArchiveTimeout: cfg.ArchiveTimeout,
		LocalTimeout:   cfg.LocalTimeout,
	}
	handler := &workspaceArchiveCommand{
		cfg:     cfg,
		options: options,
	}
	command := &cobra.Command{
		Use:   "archive",
		Short: "Pack workspace revisions into one ZIP",
		Long: "Requires --file, the destination ZIP.\n" +
			"--workspace defaults to release-align.yml. An omitted --base-dir uses release-align.base-dir from that file.\n" +
			"Without --repo and --group, every repository in the workspace is packed.\n" +
			"Revisions come from each pin, or from defaults.revision. The command does not fetch or check out.\n" +
			"Uncommitted changes are not included. An existing --file is left unchanged.\n" +
			"A path that is not the GitLab namespace is not cloned here.\n\n" +
			"  release-align workspace archive --workspace ./release-align.yml --base-dir \"$RELEASE_ALIGN_BASE_DIR\" --group lamiona/search --file ./lamiona-search.zip",
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
	flags.StringVar(&options.BaseDir, "base-dir", "", "directory containing the clones; omitted flag uses the file")
	flags.StringVar(&options.File, "file", "", "destination ZIP; must not already exist (required)")
	flags.StringArrayVar(&options.Repositories, "repo", nil, "exact workspace path; repeatable")
	flags.StringArrayVar(&options.Groups, "group", nil, "workspace group; repeatable")
	flags.StringVar(&options.Output, "output", options.Output, "report format: text or json")
	flags.DurationVar(
		&options.LocalTimeout,
		"local-timeout",
		options.LocalTimeout,
		"timeout of one local Git command",
	)
	flags.DurationVar(
		&options.ArchiveTimeout,
		"archive-timeout",
		options.ArchiveTimeout,
		"timeout of git archive and ZIP copy for one repository",
	)

	return command
}

// run resolves workspace revisions and publishes one ZIP.
func (c *workspaceArchiveCommand) run(command *cobra.Command, _ []string) error {
	if c.options.Output != cloneOutputText && c.options.Output != cloneOutputJSON {
		return &commandError{
			code:  exitUsage,
			cause: errArchiveOutput,
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

	cfg := c.archiveEnvConfig()
	if err = applyCommandEnv(command, cfg); err != nil {
		return c.failUsage(command, err)
	}

	c.options.LocalTimeout = cfg.LocalTimeout
	c.options.ArchiveTimeout = cfg.ArchiveTimeout
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

	report, err := app.ArchiveWorkspace(ctx, c.options)

	if err != nil && report != nil && report.Error == "" {
		report.Error = err.Error()
	}

	if c.options.Output == cloneOutputText && app.ExitCodeForWorkspace(err) == exitUsage {
		return c.archiveCommandError(err)
	}

	writeErr := c.writeArchiveReport(command, report, err)
	if writeErr != nil {
		return &commandError{
			code:  exitFailed,
			cause: errors.Join(err, writeErr),
		}
	}

	return c.archiveCommandError(err)
}

// fail writes one JSON document when that format was requested, then returns the status.
// Text usage errors stay on stderr. An unrecognized --output never reaches this helper.
func (c *workspaceArchiveCommand) fail(command *cobra.Command, err error) error {
	if c.options.Output != cloneOutputJSON {
		return c.archiveCommandError(err)
	}

	report := &app.ArchiveReport{
		SchemaVersion: 1,
		Error:         err.Error(),
	}

	writeErr := c.writeArchiveReport(command, report, err)
	if writeErr != nil {
		failed := &commandError{
			code:  exitFailed,
			cause: errors.Join(err, writeErr),
		}

		return failed
	}

	return c.archiveCommandError(err)
}

// failUsage writes the JSON error for a flag or environment mistake and keeps exit status 2.
func (c *workspaceArchiveCommand) failUsage(command *cobra.Command, err error) error {
	if c.options.Output == cloneOutputJSON {
		report := &app.ArchiveReport{
			SchemaVersion: 1,
			Error:         err.Error(),
		}

		writeErr := c.writeArchiveReport(command, report, err)
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

// archiveEnvConfig carries archive timeouts into the shared environment reader and back.
func (c *workspaceArchiveCommand) archiveEnvConfig() *app.Config {
	cfg := app.DefaultConfig()
	cfg.LocalTimeout = c.options.LocalTimeout
	cfg.ArchiveTimeout = c.options.ArchiveTimeout
	cfg.Output = c.options.Output

	return cfg
}

// writeArchiveReport writes JSON to stdout or the text summary.
func (c *workspaceArchiveCommand) writeArchiveReport(
	command *cobra.Command,
	report *app.ArchiveReport,
	runErr error,
) error {
	if c.options.Output == cloneOutputJSON {
		return c.writeArchiveJSON(command.OutOrStdout(), report)
	}

	return c.formatArchiveReport(command.OutOrStdout(), report, runErr)
}

// formatArchiveReport writes the text summary.
// The error text stays on stderr. A published file is still named after a late cancel.
func (c *workspaceArchiveCommand) formatArchiveReport(w io.Writer, report *app.ArchiveReport, runErr error) error {
	if w == nil || report == nil {
		return nil
	}

	if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
		return c.writeArchiveInterrupted(w, report)
	}

	if report.Error != "" {
		return c.writeArchiveFailed(w, report)
	}

	_, err := fmt.Fprintf(
		w,
		"%s\nArchived: %d\nFile: %s\nGitlinks: %d\n",
		report.Source,
		report.Repositories,
		report.File,
		report.Gitlinks,
	)
	if err != nil {
		return err
	}

	if report.Gitlinks == 0 {
		return nil
	}

	_, err = fmt.Fprintln(
		w,
		"Gitlinks are recorded in the manifest. Submodule contents are not in the archive.",
	)

	return err
}

// writeArchiveInterrupted says the run stopped. It claims a missing file only when none was published.
func (*workspaceArchiveCommand) writeArchiveInterrupted(w io.Writer, report *app.ArchiveReport) error {
	if _, err := fmt.Fprintln(w, "Archive: interrupted."); err != nil {
		return err
	}

	if report.File != "" {
		_, err := fmt.Fprintf(w, "File: %s\n", report.File)

		return err
	}

	_, err := fmt.Fprintln(w, "No new archive was published.")

	return err
}

// writeArchiveFailed names a published file when one exists and does not repeat the error text.
func (*workspaceArchiveCommand) writeArchiveFailed(w io.Writer, report *app.ArchiveReport) error {
	if _, err := fmt.Fprintln(w, "Archive: failed."); err != nil {
		return err
	}

	if report.File != "" {
		_, err := fmt.Fprintf(w, "File: %s\n", report.File)

		return err
	}

	_, err := fmt.Fprintln(w, "No archive file was published.")

	return err
}

// writeArchiveJSON writes one JSON document.
func (*workspaceArchiveCommand) writeArchiveJSON(w io.Writer, report *app.ArchiveReport) error {
	if report == nil {
		return errNilReport
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")

	return enc.Encode(report)
}

// archiveCommandError maps archive failures onto process statuses.
func (*workspaceArchiveCommand) archiveCommandError(err error) error {
	if err == nil {
		return nil
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
