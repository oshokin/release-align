package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/oshokin/release-align/internal/app"
	"github.com/oshokin/release-align/internal/gitter"
)

// workspaceInitCommand owns only the flags used by offline inventory creation.
type workspaceInitCommand struct {
	options *app.WorkspaceInitOptions
	file    string
}

var errWorkspaceInitFlags = errors.New("workspace init requires --base-dir, --branch and --file")

// newWorkspaceCommand groups inventory management without inheriting synchronization flags.
func newWorkspaceCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "workspace",
		Short: "Manage local workspace inventories",
	}
	options := new(app.WorkspaceInitOptions)
	handler := &workspaceInitCommand{
		options: options,
	}
	initCommand := &cobra.Command{
		Use:   "init",
		Short: "Create an inventory from existing local clones and directory groups",
		Long: "Scan --base-dir for Git working trees and write a new schema 1 workspace.\n" +
			"A group is a parent directory prefix: mailion/search/pasifae is in mailion and mailion/search.\n" +
			"A repository directly under --base-dir has no group. Hidden directories and directory symlinks are skipped.\n" +
			"The scan does not fetch, and it does not check that --branch exists. --file must not already exist.\n" +
			"Clones that are not on disk are omitted. Review the file before sync.",
		Args: cobra.NoArgs,
		RunE: handler.run,
	}
	flags := initCommand.Flags()
	flags.StringVar(&options.BaseDir, "base-dir", "", "directory containing existing clones (required)")
	flags.StringVar(
		&options.Branch,
		"branch",
		"",
		"explicit default branch; existence is checked by sync/status (required)",
	)
	flags.StringVar(&handler.file, "file", "", "new workspace JSON filename; must not exist (required)")
	flags.StringVar(&options.Release, "release", "", "optional human-readable release label")
	command.AddCommand(initCommand)

	return command
}

// run scans first, validates the complete document, and only then creates the destination.
func (c *workspaceInitCommand) run(command *cobra.Command, _ []string) error {
	if c.options.BaseDir == "" || c.options.Branch == "" || c.file == "" {
		return &commandError{
			code:  exitUsage,
			cause: errWorkspaceInitFlags,
		}
	}
	defaults := app.DefaultConfig()
	client := &gitter.Client{
		LocalTimeout: defaults.LocalTimeout,
		NoLazyFetch:  true,
	}

	spec, err := app.ScanWorkspace(command.Context(), client, c.options)
	if err != nil {
		return &commandError{
			code:  exitFailed,
			cause: err,
		}
	}

	if err = command.Context().Err(); err != nil {
		return err
	}

	if err = app.CreateWorkspaceFile(c.file, spec); err != nil {
		return &commandError{
			code:  exitFailed,
			cause: err,
		}
	}

	_, err = fmt.Fprintf(
		command.OutOrStdout(),
		"Created %s: %d local repositories. Review the inventory before synchronization.\n",
		c.file,
		len(spec.Projects),
	)
	if err != nil {
		return &commandError{
			code:  exitFailed,
			cause: err,
		}
	}

	return nil
}
