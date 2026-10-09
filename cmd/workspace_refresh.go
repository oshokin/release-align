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

// refreshLog is the logger and Git client for one refresh run.
type refreshLog struct {
	// ctx carries the text logger.
	ctx context.Context
	// client runs local Git during the scan.
	client *gitter.Client
}

// workspaceRefreshCommand compares a workspace file with local clones.
type workspaceRefreshCommand struct {
	// cfg is the shared program configuration. The root flag writes its log level here.
	cfg *app.Config
	// baseDir is the root that contains the clones.
	baseDir string
	// file is the workspace document.
	file string
	// add lists paths to append.
	add []string
	// sync appends new local clones and drops listed paths that are gone.
	sync bool
	// remote compares the file with the non-archived GitLab catalog.
	remote bool
	// delete removes checkouts dropped by this same --remote --sync run.
	delete bool
}

const (
	// refreshNoChanges is the preview line when the file was left as it was.
	refreshNoChanges = "No changes written."
)

var (
	// errWorkspaceRefreshFlags means the file has no base-dir and the flag was omitted.
	errWorkspaceRefreshFlags = errors.New("workspace refresh requires --base-dir when the file does not record one")
	// errWorkspaceRefreshExclusive means both --add and --sync were set.
	errWorkspaceRefreshExclusive = errors.New("workspace refresh accepts either repeated --add or --sync")
	// errWorkspaceRefreshPaths means a path was passed without --sync.
	errWorkspaceRefreshPaths = errors.New("paths are arguments of --sync")
	// errWorkspaceRefreshRemoteDelete means --delete was set without --remote and --sync.
	errWorkspaceRefreshRemoteDelete = errors.New("workspace refresh --delete requires --remote and --sync")
	// errWorkspaceRefreshRemoteAdd means --remote was combined with --add.
	errWorkspaceRefreshRemoteAdd = errors.New("workspace refresh --remote cannot be combined with --add")
	// errWorkspaceRefreshRemotePaths means --remote was given path arguments.
	errWorkspaceRefreshRemotePaths = errors.New("workspace refresh --remote does not take paths")
)

// newWorkspaceRefreshCommand builds the offline inventory refresh command.
func newWorkspaceRefreshCommand(cfg *app.Config) *cobra.Command {
	handler := &workspaceRefreshCommand{
		cfg: cfg,
	}
	command := &cobra.Command{
		Use:   "refresh [--sync [path...]]",
		Short: "Compare a workspace file with local clones or the GitLab catalog",
		Long: "Scan the clones and compare them with the workspace file. Without --add or --sync, nothing is written.\n" +
			"--file defaults to release-align.yml. An omitted --base-dir uses release-align.base-dir from that file.\n" +
			"Groups of new entries come from parent directories, as in workspace init. Existing groups, pins, and order stay.\n" +
			"--sync applies the disk diff: it appends every new local clone and drops every listed path whose directory is gone.\n" +
			"--sync PATH limits that diff to the named paths. Repeat the path as an argument.\n" +
			"--add appends exact paths and leaves missing listed paths in the file.\n" +
			"A bare --sync refuses to write when no clones are found and every listed path is missing.\n" +
			"--remote lists paths that the non-archived GitLab catalog did not return. Archived projects are in that list.\n" +
			"--remote --sync drops those paths from the file and leaves the directories.\n" +
			"--delete is allowed only with --remote --sync and removes only the directories that run just dropped.\n" +
			"The command does not fetch or switch branches. Lazy-fetch suppression is best effort and depends on the installed Git.",
		Args:          cobra.ArbitraryArgs,
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
	flags.BoolVar(&handler.sync, "sync", false, "apply the whole diff; optional PATH arguments limit the add or drop")
	flags.BoolVar(&handler.remote, "remote", false, "compare listed paths with the non-archived GitLab catalog")
	flags.BoolVar(
		&handler.delete,
		"delete",
		false,
		"with --remote --sync, remove the directories that this run dropped from the file",
	)

	return command
}

// run previews or appends. Usage failures exit 2. A comparison or write failure exits 1.
func (c *workspaceRefreshCommand) run(command *cobra.Command, args []string) error {
	if len(args) > 0 && !c.sync {
		return &commandError{
			code:  exitUsage,
			cause: errWorkspaceRefreshPaths,
		}
	}

	if c.sync && len(c.add) > 0 {
		return &commandError{
			code:  exitUsage,
			cause: errWorkspaceRefreshExclusive,
		}
	}

	if err := c.remoteFlags(args); err != nil {
		return err
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

	logged, err := c.refreshLogger(command)
	if err != nil {
		return err
	}

	options := &app.WorkspaceRefreshOptions{
		BaseDir:   c.baseDir,
		Add:       c.add,
		Sync:      c.sync,
		SyncPaths: args,
		Remote:    c.remote,
		Delete:    c.delete,
	}

	result, err := app.RefreshWorkspace(logged.ctx, logged.client, c.file, options)
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

// refreshLogger loads the workspace level onto the shared config and builds the Git client.
func (c *workspaceRefreshCommand) refreshLogger(command *cobra.Command) (*refreshLog, error) {
	spec, err := app.LoadWorkspace(c.file)
	if err != nil {
		return nil, c.refreshCommandError(err)
	}

	if err = applyCommandEnv(command, c.cfg); err != nil {
		return nil, &commandError{
			code:  exitUsage,
			cause: err,
		}
	}

	if err = c.cfg.TakeLogLevel(durationLocked(command, "log-level", "LOG_LEVEL"), spec.LogLevel); err != nil {
		return nil, &commandError{
			code:  exitUsage,
			cause: err,
		}
	}

	client := &gitter.Client{
		LocalTimeout: c.cfg.LocalTimeout,
		NoLazyFetch:  true,
	}
	logged := &refreshLog{
		ctx:    withCommandLog(command, false, c.cfg.LogLevel),
		client: client,
	}

	return logged, nil
}

// remoteFlags rejects combinations that would delete the wrong set of paths.
func (c *workspaceRefreshCommand) remoteFlags(args []string) error {
	if c.delete && (!c.remote || !c.sync) {
		return &commandError{
			code:  exitUsage,
			cause: errWorkspaceRefreshRemoteDelete,
		}
	}

	if c.remote && len(c.add) > 0 {
		return &commandError{
			code:  exitUsage,
			cause: errWorkspaceRefreshRemoteAdd,
		}
	}

	if c.remote && len(args) > 0 {
		return &commandError{
			code:  exitUsage,
			cause: errWorkspaceRefreshRemotePaths,
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

	if result.Remote {
		return c.writeRemoteReport(out, result)
	}

	if _, err := fmt.Fprintf(out, "Workspace: %s\nListed: %d\n", c.file, result.Listed); err != nil {
		return err
	}

	if result.Written {
		if err := c.writeAddedPaths(out, result.Added); err != nil {
			return err
		}

		if err := c.writeRemovedPaths(out, result.Removed); err != nil {
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
		if _, err := fmt.Fprintln(out, c.refreshHint(result)); err != nil {
			return err
		}
	}

	_, err := fmt.Fprintln(
		out,
		"Readiness was not checked. Run release-align workspace status for the selected workspace.",
	)

	return err
}

// writeRemoteReport prints paths the non-archived catalog did not return.
func (c *workspaceRefreshCommand) writeRemoteReport(out io.Writer, result *app.WorkspaceRefreshResult) error {
	if _, err := fmt.Fprintf(out, "Workspace: %s\nListed: %d\n", c.file, result.Listed); err != nil {
		return err
	}

	if err := c.writeRemoteAbsent(out, result); err != nil {
		return err
	}

	if result.Written {
		if err := c.writeRemovedPaths(out, result.Removed); err != nil {
			return err
		}
	}

	if err := c.writeDeletedPaths(out, result); err != nil {
		return err
	}

	if !result.Written {
		line := refreshNoChanges
		if len(result.RemoteAbsent) > 0 {
			line = "No changes written. Use --remote --sync to drop these paths from the workspace."
		}

		_, err := fmt.Fprintln(out, line)

		return err
	}

	return nil
}

// writeRemoteAbsent lists in-scope paths the catalog omitted.
func (*workspaceRefreshCommand) writeRemoteAbsent(out io.Writer, result *app.WorkspaceRefreshResult) error {
	if result.Written {
		return nil
	}

	if _, err := fmt.Fprintf(out, "Absent from GitLab: %d\n", len(result.RemoteAbsent)); err != nil {
		return err
	}

	for _, path := range result.RemoteAbsent {
		if _, err := fmt.Fprintf(out, "  ! %s\n", path); err != nil {
			return err
		}
	}

	return nil
}

// writeDeletedPaths lists directories removed with the paths just dropped, or says they were kept.
func (*workspaceRefreshCommand) writeDeletedPaths(out io.Writer, result *app.WorkspaceRefreshResult) error {
	if !result.Written {
		return nil
	}

	if len(result.Deleted) == 0 {
		_, err := fmt.Fprintln(out, "Directories were kept.")

		return err
	}

	if _, err := fmt.Fprintf(out, "Deleted: %d\n", len(result.Deleted)); err != nil {
		return err
	}

	for _, path := range result.Deleted {
		if _, err := fmt.Fprintf(out, "  - %s\n", path); err != nil {
			return err
		}
	}

	return nil
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

// writeRemovedPaths lists the listed paths that were dropped.
func (*workspaceRefreshCommand) writeRemovedPaths(out io.Writer, removed []string) error {
	if _, err := fmt.Fprintf(out, "Removed: %d\n", len(removed)); err != nil {
		return err
	}

	for _, path := range removed {
		if _, err := fmt.Fprintf(out, "  - %s\n", path); err != nil {
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

// refreshHint tells the operator how to apply the diff that was just printed.
func (*workspaceRefreshCommand) refreshHint(result *app.WorkspaceRefreshResult) string {
	missing := len(result.Missing) > 0
	unlisted := len(result.Unlisted) > 0

	switch {
	case missing && unlisted:
		return "No changes written. Use --sync PATH to add or drop that path, or --sync to apply the whole diff."
	case missing:
		return "No changes written. Use --sync PATH to drop that repository, or --sync to drop every missing one."
	case unlisted:
		return "No changes written. Use --add PATH to add that repository, or --sync to add every new local clone."
	default:
		return refreshNoChanges
	}
}

// writeMissingPaths lists saved paths that are not on disk.
func (*workspaceRefreshCommand) writeMissingPaths(out io.Writer, missing []string) error {
	if _, err := fmt.Fprintf(out, "Missing listed repositories: %d\n", len(missing)); err != nil {
		return err
	}

	for _, path := range missing {
		if _, err := fmt.Fprintf(out, "  ! %s  drop with --sync\n", path); err != nil {
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
