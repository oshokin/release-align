package cmd

import (
	"errors"

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
	ctx := withCommandLog(command, false, c.cfg.LogLevel)

	_, err = app.StashWorkspace(ctx, client, c.file, options)
	if err != nil {
		return c.stashCommandError(err)
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
