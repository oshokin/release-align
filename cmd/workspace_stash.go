package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/oshokin/release-align/internal/app"
	"github.com/oshokin/release-align/internal/gitter"
)

// workspaceStashCommand saves dirty worktrees named by the workspace file.
type workspaceStashCommand struct {
	// cfg is the shared program configuration. The root flag writes its log level here.
	cfg *app.Config
	// baseDir is the root that contains the clones.
	baseDir string
	// file is the workspace document.
	file string
}

// stashRecordList is one section of the stash report.
type stashRecordList struct {
	// title is the heading. An empty record list omits the section.
	title string
	// records are the stash ids in this section.
	records []*app.StashRecord
	// note is appended to each line. Empty for a new stash.
	note string
}

// errWorkspaceStashFlags means the file has no base-dir and the flag was omitted.
var errWorkspaceStashFlags = errors.New("workspace stash requires --base-dir when the file does not record one")

// newWorkspaceStashCommand builds the stash command. It does not pop or switch branches.
func newWorkspaceStashCommand(cfg *app.Config) *cobra.Command {
	handler := &workspaceStashCommand{
		cfg: cfg,
	}
	command := &cobra.Command{
		Use:   "stash",
		Short: "Stash dirty worktrees listed in the workspace file",
		Long: "Run git stash push -u in each dirty repository named by the workspace file.\n" +
			"A clean worktree is left alone. A missing directory is reported and skipped.\n" +
			"A second run does not create another stash when one with the message release-align already exists.\n" +
			"Each repository is logged with phase=stash. A new or existing stash line includes the oid.\n" +
			"The command does not pop, fetch, or switch branches.\n" +
			"--file defaults to release-align.yml. An omitted --base-dir uses release-align.base-dir from that file.",
		Args:          cobra.NoArgs,
		RunE:          handler.run,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	command.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &commandError{
			code:  exitUsage,
			cause: err,
		}
	})

	flags := command.Flags()
	flags.StringVar(
		&handler.baseDir,
		"base-dir",
		"",
		"directory containing existing clones; omitted flag uses the file",
	)
	flags.StringVar(&handler.file, "file", defaultWorkspaceFile, "workspace YAML (.yml or .yaml)")

	return command
}

// run stashes dirty listed repositories. A Git failure exits 1 after the other repositories are visited.
func (c *workspaceStashCommand) run(command *cobra.Command, _ []string) error {
	choice := &baseDirChoice{
		command:   command,
		workspace: c.file,
		current:   c.baseDir,
	}

	base, err := savedBaseDir(choice)
	if err != nil {
		return c.stashCommandError(err)
	}

	c.baseDir = base
	if c.file == "" {
		return &commandError{
			code:  exitUsage,
			cause: errWorkspaceStashFlags,
		}
	}

	spec, err := app.LoadWorkspace(c.file)
	if err != nil {
		return c.stashCommandError(err)
	}

	if err = applyCommandEnv(command, c.cfg); err != nil {
		return &commandError{
			code:  exitUsage,
			cause: err,
		}
	}

	if err = c.cfg.TakeLogLevel(durationLocked(command, "log-level", "LOG_LEVEL"), spec.LogLevel); err != nil {
		return &commandError{
			code:  exitUsage,
			cause: err,
		}
	}

	client := &gitter.Client{
		LocalTimeout: c.cfg.LocalTimeout,
		NoLazyFetch:  true,
	}
	options := &app.WorkspaceStashOptions{
		BaseDir: c.baseDir,
	}
	ctx := withCommandLog(command, c.cfg.LogLevel)

	result, runErr := app.StashWorkspace(ctx, client, c.file, options)
	if writeErr := c.writeStashReport(command.OutOrStdout(), result, runErr); writeErr != nil {
		return c.stashCommandError(errors.Join(runErr, writeErr))
	}

	if runErr != nil {
		return c.stashCommandError(runErr)
	}

	return nil
}

// writeStashReport prints the stash result. A nil result means the run did not start.
// An existing stash was not updated, so the line says that no new stash was created.
func (c *workspaceStashCommand) writeStashReport(
	w io.Writer,
	result *app.WorkspaceStashResult,
	runErr error,
) error {
	if w == nil || result == nil {
		return nil
	}

	if _, err := fmt.Fprintf(
		w,
		"Stash: %s. Created: %d. Existing: %d. Clean: %d. Missing: %d.\n",
		c.stashState(runErr),
		len(result.Stashed),
		len(result.Kept),
		result.Clean,
		len(result.Missing),
	); err != nil {
		return err
	}

	saved := &stashRecordList{
		title:   "Saved:",
		records: result.Stashed,
	}
	if err := c.writeStashRecords(w, saved); err != nil {
		return err
	}

	existing := &stashRecordList{
		title:   "Existing:",
		records: result.Kept,
		note:    "; no new stash created",
	}
	if err := c.writeStashRecords(w, existing); err != nil {
		return err
	}

	return c.writeStashRemainder(w, result)
}

// stashState is the one-word outcome printed before the counts.
func (*workspaceStashCommand) stashState(runErr error) string {
	switch {
	case runErr == nil:
		return reportCompleted
	case errors.Is(runErr, context.Canceled), errors.Is(runErr, context.DeadlineExceeded):
		return "interrupted"
	default:
		return "failed"
	}
}

// writeStashRecords prints one heading and the paths that have a stash oid.
func (*workspaceStashCommand) writeStashRecords(w io.Writer, list *stashRecordList) error {
	if list == nil || len(list.records) == 0 {
		return nil
	}

	if _, err := fmt.Fprintln(w, list.title); err != nil {
		return err
	}

	for _, record := range list.records {
		if record == nil {
			continue
		}

		if _, err := fmt.Fprintf(w, "  %s  %s%s\n", record.Path, record.OID, list.note); err != nil {
			return err
		}
	}

	return nil
}

// writeStashRemainder prints missing paths, failures, and paths the run did not reach.
func (c *workspaceStashCommand) writeStashRemainder(w io.Writer, result *app.WorkspaceStashResult) error {
	if err := c.writeStashPaths(w, "Missing:", result.Missing); err != nil {
		return err
	}

	if len(result.Failed) > 0 {
		if _, err := fmt.Fprintln(w, "Failed:"); err != nil {
			return err
		}

		for _, failure := range result.Failed {
			if failure == nil {
				continue
			}

			if _, err := fmt.Fprintf(w, "  %s  %s\n", failure.Path, failure.Error); err != nil {
				return err
			}
		}
	}

	return c.writeStashPaths(w, "Not started:", result.NotStarted)
}

// writeStashPaths prints one heading and its paths. An empty list is omitted.
func (*workspaceStashCommand) writeStashPaths(w io.Writer, title string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}

	if _, err := fmt.Fprintln(w, title); err != nil {
		return err
	}

	for _, path := range paths {
		if _, err := fmt.Fprintf(w, "  %s\n", path); err != nil {
			return err
		}
	}

	return nil
}

// stashCommandError maps a stash failure onto a process status.
func (*workspaceStashCommand) stashCommandError(err error) error {
	code := exitFailed
	if errors.Is(err, errWorkspaceStashFlags) || errors.Is(err, errBaseDirMissing) {
		code = exitUsage
	}

	return &commandError{
		code:  code,
		cause: err,
	}
}
