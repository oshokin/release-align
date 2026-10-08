package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/oshokin/release-align/internal/app"
	"github.com/oshokin/release-align/internal/gitter"
)

// workspaceRefreshCommand compares a workspace file with local clones.
type workspaceRefreshCommand struct {
	// baseDir is the root that contains the clones.
	baseDir string
	// file is the workspace document.
	file string
	// add lists paths to append.
	add []string
	// addAll appends every new local clone.
	addAll bool
}

var (
	// errWorkspaceRefreshFlags means the file has no base-dir and the flag was omitted.
	errWorkspaceRefreshFlags = errors.New("workspace refresh requires --base-dir when the file does not record one")
	// errWorkspaceRefreshExclusive means both --add and --add-all were set.
	errWorkspaceRefreshExclusive = errors.New("workspace refresh accepts either repeated --add or --add-all")
)

// newWorkspaceRefreshCommand builds the offline inventory refresh command.
func newWorkspaceRefreshCommand() *cobra.Command {
	handler := new(workspaceRefreshCommand)
	command := &cobra.Command{
		Use:   "refresh",
		Short: "Compare a workspace file with local clones and append selected new ones",
		Long: "Scan the clones and compare them with the workspace file. Without --add or --add-all, nothing is written.\n" +
			"--file defaults to release-align.yml. An omitted --base-dir uses release-align.base-dir from that file.\n" +
			"Groups of new entries come from parent directories, as in workspace init. Existing groups, pins, and order stay.\n" +
			"Missing listed paths are reported and kept. A repository that exists only on a server is not visible.\n" +
			"The command does not fetch or switch branches. Lazy-fetch suppression is best effort and depends on the installed Git.",
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
	flags.StringArrayVar(&handler.add, "add", nil, "exact relative path to add; repeatable; not a glob")
	flags.BoolVar(&handler.addAll, "add-all", false, "add every new local clone under base-dir")

	return command
}

// run previews or appends. Usage failures exit 2. A comparison or write failure exits 1.
func (c *workspaceRefreshCommand) run(command *cobra.Command, _ []string) error {
	if c.addAll && len(c.add) > 0 {
		return &commandError{
			code:  exitUsage,
			cause: errWorkspaceRefreshExclusive,
		}
	}

	choice := &baseDirChoice{
		command:   command,
		workspace: c.file,
		current:   c.baseDir,
	}

	base, err := savedBaseDir(choice)
	if err != nil {
		return c.refreshCommandError(err)
	}

	c.baseDir = base
	if c.file == "" {
		return &commandError{
			code:  exitUsage,
			cause: errWorkspaceRefreshFlags,
		}
	}

	defaults := app.DefaultConfig()
	client := &gitter.Client{
		LocalTimeout: defaults.LocalTimeout,
		NoLazyFetch:  true,
	}
	options := &app.WorkspaceRefreshOptions{
		BaseDir: c.baseDir,
		Add:     c.add,
		AddAll:  c.addAll,
	}

	result, err := app.RefreshWorkspace(command.Context(), client, c.file, options)
	if err != nil {
		return c.refreshCommandError(err)
	}

	if err = c.writeRefreshReport(command.OutOrStdout(), result); err != nil {
		return &commandError{
			code:  exitFailed,
			cause: err,
		}
	}

	return nil
}

// refreshCommandError maps a refresh failure onto a process status.
func (*workspaceRefreshCommand) refreshCommandError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	code := exitFailed
	if errors.Is(err, app.ErrWorkspaceRefreshUsage) || errors.Is(err, errBaseDirMissing) {
		code = exitUsage
	}

	return &commandError{
		code:  code,
		cause: err,
	}
}

// writeRefreshReport prints the inventory diff. It does not claim that revisions are ready.
func (c *workspaceRefreshCommand) writeRefreshReport(out io.Writer, result *app.WorkspaceRefreshResult) error {
	if result == nil {
		return errWorkspaceRefreshFlags
	}

	if _, err := fmt.Fprintf(out, "Workspace: %s\nListed: %d\n", c.file, result.Listed); err != nil {
		return err
	}

	if result.Written {
		if err := c.writeAddedPaths(out, result.Added); err != nil {
			return err
		}
	}

	if err := c.writeUnlistedPaths(out, result.Unlisted); err != nil {
		return err
	}

	if err := c.writeMissingPaths(out, result.Missing); err != nil {
		return err
	}

	if !result.Written {
		if _, err := fmt.Fprintln(
			out,
			"No changes written. Use --add PATH or --add-all to add local repositories.",
		); err != nil {
			return err
		}
	}

	_, err := fmt.Fprintln(out, "Readiness was not checked. Run release-align status for the selected workspace.")

	return err
}

// writeAddedPaths lists the paths that were appended.
func (*workspaceRefreshCommand) writeAddedPaths(out io.Writer, added []string) error {
	if _, err := fmt.Fprintf(out, "Added: %d\n", len(added)); err != nil {
		return err
	}

	for _, path := range added {
		if _, err := fmt.Fprintf(out, "  + %s\n", path); err != nil {
			return err
		}
	}

	return nil
}

// writeUnlistedPaths lists local clones that are still absent from the file.
func (c *workspaceRefreshCommand) writeUnlistedPaths(out io.Writer, projects []*app.ProjectSpec) error {
	if _, err := fmt.Fprintf(out, "Unlisted local repositories: %d\n", len(projects)); err != nil {
		return err
	}

	for _, project := range projects {
		if _, err := fmt.Fprintf(
			out,
			"  + %s  groups: %s\n",
			project.Path,
			c.formatGroups(project.Groups),
		); err != nil {
			return err
		}
	}

	return nil
}

// writeMissingPaths lists saved paths that are not on disk. They stay in the file.
func (*workspaceRefreshCommand) writeMissingPaths(out io.Writer, missing []string) error {
	if _, err := fmt.Fprintf(out, "Missing listed repositories: %d\n", len(missing)); err != nil {
		return err
	}

	for _, path := range missing {
		if _, err := fmt.Fprintf(out, "  ! %s  kept in workspace\n", path); err != nil {
			return err
		}
	}

	return nil
}

// formatGroups renders directory groups. A clone directly under base-dir has none.
func (*workspaceRefreshCommand) formatGroups(groups []string) string {
	if len(groups) == 0 {
		return "none"
	}

	return strings.Join(groups, ", ")
}
