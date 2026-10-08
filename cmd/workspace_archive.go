package cmd

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/oshokin/release-align/internal/app"
)

// workspaceArchiveCommand owns the flags of workspace archive.
type workspaceArchiveCommand struct {
	// options are the parsed archive flags.
	options *app.WorkspaceArchiveOptions
}

// errArchiveOutput means --output is neither text nor json.
var errArchiveOutput = errors.New("output must be text or json")

// newWorkspaceArchiveCommand packs workspace revisions into one ZIP.
func newWorkspaceArchiveCommand() *cobra.Command {
	defaults := app.DefaultConfig()
	options := &app.WorkspaceArchiveOptions{
		Output:         defaults.Output,
		ArchiveTimeout: defaults.ArchiveTimeout,
		LocalTimeout:   defaults.LocalTimeout,
	}
	handler := &workspaceArchiveCommand{
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
		return c.archiveCommandError(err)
	}

	c.options.BaseDir = base

	cfg := c.archiveEnvConfig()
	if err = applyCommandEnv(command, cfg); err != nil {
		return &commandError{
			code:  exitUsage,
			cause: err,
		}
	}

	c.options.LocalTimeout = cfg.LocalTimeout
	c.options.ArchiveTimeout = cfg.ArchiveTimeout
	c.options.SetTimeoutLocks(timeoutLocks(command))
	c.options.SetProgress(command.ErrOrStderr())

	report, err := app.ArchiveWorkspace(command.Context(), c.options)
	if command.Context().Err() != nil {
		return err
	}

	if err != nil && report != nil && report.Error == "" {
		report.Error = err.Error()
	}

	if c.options.Output == cloneOutputText && app.ExitCodeForWorkspace(err) == exitUsage {
		return c.archiveCommandError(err)
	}

	writeErr := c.writeArchiveReport(command, report)
	if writeErr != nil {
		return &commandError{
			code:  exitFailed,
			cause: writeErr,
		}
	}

	return c.archiveCommandError(err)
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
func (c *workspaceArchiveCommand) writeArchiveReport(command *cobra.Command, report *app.ArchiveReport) error {
	if c.options.Output == cloneOutputJSON {
		return app.WriteArchiveReport(command.OutOrStdout(), report)
	}

	return app.FormatArchiveReport(command.OutOrStdout(), report)
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
