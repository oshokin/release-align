package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/oshokin/release-align/internal/app"
	"github.com/oshokin/release-align/internal/gitter"
)

// workspaceInitCommand owns only the flags used by offline inventory creation.
type workspaceInitCommand struct {
	// cfg is the shared program configuration. The root flag writes its log level here.
	cfg *app.Config
	// options are the discovery flags.
	options *app.WorkspaceInitOptions
	// file is the workspace path to create.
	file string
}

const (
	// defaultInitBranch is stored when --branch is omitted.
	defaultInitBranch = "master"
	// defaultWorkspaceFile is created in the current directory when --file is omitted.
	defaultWorkspaceFile = "release-align.yml"
	// gitlabURLFromBaseNotice is printed when the GitLab URL came from the base directory name.
	gitlabURLFromBaseNotice = "GitLab URL %s taken from the base directory name.\n"
	// gitlabGroupsFromOriginsNotice is printed when groups were taken from clone URLs.
	gitlabGroupsFromOriginsNotice = "GitLab groups taken from clone URLs on %s: %s.\n"
)

// errWorkspaceInitFlags means a required init flag was omitted or cleared.
var errWorkspaceInitFlags = errors.New("workspace init requires --base-dir")

// newWorkspaceCommand groups every workspace verb. Sync flags stay on sync and status.
func newWorkspaceCommand(cfg *app.Config) *cobra.Command {
	command := &cobra.Command{
		Use:   "workspace",
		Short: "Manage local workspace inventories",
	}
	options := new(app.WorkspaceInitOptions)
	handler := &workspaceInitCommand{
		cfg:     cfg,
		options: options,
	}
	initCommand := &cobra.Command{
		Use:   "init",
		Short: "Create an inventory from existing local clones and directory groups",
		Long: "Scan --base-dir for Git working trees and write release-align.yml.\n" +
			"The file is a west manifest plus a release-align block. It is not a full west workspace.\n" +
			"A group is a parent directory prefix: lamiona/search/calyra is in lamiona and lamiona/search.\n" +
			"A repository directly under --base-dir has no group. Hidden directories and directory symlinks are skipped.\n" +
			"--branch defaults to master and is not checked against the server. --file defaults to release-align.yml and must not already exist.\n" +
			"An omitted --gitlab-url uses the last --base-dir component when that name is a DNS host.\n" +
			"An omitted --gitlab-group list is filled from the first path segment of origins on that host.\n" +
			"Clones that are not on disk are omitted. Review the file before sync.",
		Args: cobra.NoArgs,
		RunE: handler.run,
	}
	flags := initCommand.Flags()
	flags.StringVar(
		&options.BaseDir,
		"base-dir",
		"",
		"directory containing existing clones; omitted flag uses RELEASE_ALIGN_BASE_DIR",
	)
	flags.StringVar(
		&options.Branch,
		"branch",
		defaultInitBranch,
		"default branch stored as refs/heads/<branch>; existence is checked by sync/status",
	)
	flags.StringVar(
		&handler.file,
		"file",
		defaultWorkspaceFile,
		"new workspace YAML filename (.yml or .yaml); must not exist",
	)
	flags.StringVar(&options.Release, "release", "", "optional human-readable release label")
	flags.StringVar(
		&options.GitLabURL,
		"gitlab-url",
		"",
		"https origin saved for later --remote and clone; omitted flag uses a DNS base directory name",
	)
	flags.StringArrayVar(
		&options.GitLabGroups,
		"gitlab-group",
		nil,
		"GitLab group path to save; repeatable; omitted flag uses origins on the saved host; no API call",
	)
	command.AddCommand(newSyncCommand(cfg))
	command.AddCommand(newStatusCommand(cfg))
	command.AddCommand(initCommand)
	command.AddCommand(newWorkspaceRefreshCommand(cfg))
	command.AddCommand(newWorkspaceStashCommand(cfg))
	command.AddCommand(newWorkspaceCloneCommand(cfg))
	command.AddCommand(newWorkspaceArchiveCommand(cfg))

	return command
}

// run refuses an existing destination, scans, checks the destination again, and then creates it.
func (c *workspaceInitCommand) run(command *cobra.Command, _ []string) error {
	if err := c.resolveBaseDir(command); err != nil {
		return err
	}

	if err := app.AbsentWorkspaceFile(c.file); err != nil {
		return &commandError{
			code:  exitFailed,
			cause: err,
		}
	}

	if err := applyCommandEnv(command, c.cfg); err != nil {
		return &commandError{
			code:  exitUsage,
			cause: err,
		}
	}

	if err := c.cfg.TakeLogLevel(durationLocked(command, "log-level", "LOG_LEVEL"), ""); err != nil {
		return &commandError{
			code:  exitUsage,
			cause: err,
		}
	}

	client := &gitter.Client{
		LocalTimeout: c.cfg.LocalTimeout,
		NoLazyFetch:  true,
	}

	ctx := withCommandLog(command, c.cfg.LogLevel)

	spec, err := app.ScanWorkspace(ctx, client, c.options)
	if err != nil {
		return &commandError{
			code:  exitFailed,
			cause: err,
		}
	}

	if err = command.Context().Err(); err != nil {
		return err
	}

	if err = app.AbsentWorkspaceFile(c.file); err != nil {
		return &commandError{
			code:  exitFailed,
			cause: err,
		}
	}

	spec.LogLevel = c.cfg.LogLevel
	if err = app.CreateWorkspaceFile(c.file, spec); err != nil {
		return &commandError{
			code:  exitFailed,
			cause: err,
		}
	}

	return c.printInitNotes(command, spec)
}

// printInitNotes reports what was written and which GitLab fields were inferred.
func (c *workspaceInitCommand) printInitNotes(command *cobra.Command, spec *app.WorkspaceSpec) error {
	_, err := fmt.Fprintf(
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

	if spec.GitLabURLFromBase {
		_, err = fmt.Fprintf(command.OutOrStdout(), gitlabURLFromBaseNotice, spec.GitLab.URL)
		if err != nil {
			return &commandError{
				code:  exitFailed,
				cause: err,
			}
		}
	}

	if len(c.options.GitLabGroups) == 0 && spec.GitLab != nil && len(spec.GitLab.Groups) > 0 {
		_, err = fmt.Fprintf(
			command.OutOrStdout(),
			gitlabGroupsFromOriginsNotice,
			spec.GitLab.URL,
			strings.Join(spec.GitLab.Groups, ", "),
		)
		if err != nil {
			return &commandError{
				code:  exitFailed,
				cause: err,
			}
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

// resolveBaseDir stores --base-dir or RELEASE_ALIGN_BASE_DIR. Init does not read a workspace file.
func (c *workspaceInitCommand) resolveBaseDir(command *cobra.Command) error {
	choice := &baseDirChoice{
		command: command,
		current: c.options.BaseDir,
	}

	base, err := savedBaseDir(choice)
	if err != nil {
		return &commandError{
			code:  exitUsage,
			cause: err,
		}
	}

	c.options.BaseDir = base
	if c.options.Branch == "" || c.file == "" {
		return &commandError{
			code:  exitUsage,
			cause: errWorkspaceInitFlags,
		}
	}

	return nil
}
