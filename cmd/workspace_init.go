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
	// options are the discovery flags.
	options *app.WorkspaceInitOptions
	// file is the workspace path to create.
	file string
}

// errWorkspaceInitFlags means a required init flag was omitted.
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
		Long: "Scan --base-dir for Git working trees and write a new release-align.yml.\n" +
			"The file is a west manifest plus a release-align block. It is not a full west workspace.\n" +
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
	flags.StringVar(&handler.file, "file", "", "new workspace YAML filename (.yml or .yaml); must not exist (required)")
	flags.StringVar(&options.Release, "release", "", "optional human-readable release label")
	flags.StringVar(
		&options.GitLabURL,
		"gitlab-url",
		"",
		"https origin saved for later --remote and clone; no API call",
	)
	flags.StringArrayVar(
		&options.GitLabGroups,
		"gitlab-group",
		nil,
		"GitLab group path to save; repeatable; no API call",
	)
	command.AddCommand(initCommand)
	command.AddCommand(newWorkspaceRefreshCommand())
	command.AddCommand(newWorkspaceCloneCommand())
	command.AddCommand(newWorkspaceArchiveCommand())

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

	if len(spec.URLGaps) == 0 {
		return nil
	}

	_, err = fmt.Fprintf(
		command.OutOrStdout(),
		"URL omitted for %d projects. Fill url before using the file with west.\n",
		len(spec.URLGaps),
	)
	if err != nil {
		return &commandError{
			code:  exitFailed,
			cause: err,
		}
	}

	return nil
}
