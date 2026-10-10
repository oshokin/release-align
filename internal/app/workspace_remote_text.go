package app

import (
	"fmt"
	"io"
	"runtime"
	"strings"
)

// catalogSection is one heading and the paths printed under it.
type catalogSection struct {
	// title is the heading, including the trailing colon.
	title string
	// paths are the project paths for this heading. An empty list is omitted.
	paths []string
}

// goosWindows selects PowerShell quoting.
const goosWindows = "windows"

// WriteRemoteInventory writes the plain-text catalog section.
func WriteRemoteInventory(w io.Writer, cfg *Config, report *WorkspaceReport) error {
	if w == nil || report == nil {
		return nil
	}

	if err := writeSelectedLine(w, report); err != nil {
		return err
	}

	inventory := report.RemoteInventory
	if inventory == nil {
		_, err := fmt.Fprintln(w, remoteNotCheckedMsg)

		return err
	}

	if err := writeInventoryHead(w, inventory); err != nil {
		return err
	}

	if inventory.Catalog == nil || inventory.Status != remoteStatusChecked {
		return nil
	}

	if catalogQuiet(inventory.Catalog) {
		_, err := fmt.Fprintln(w, "No project differences.")

		return err
	}

	if err := writeCatalogLists(w, inventory.Catalog); err != nil {
		return err
	}

	return writeNextCommands(w, cfg, inventory.Catalog)
}

// writeSelectedLine states readiness of the workspace selection, not of the server.
func writeSelectedLine(w io.Writer, report *WorkspaceReport) error {
	ready := 0

	for _, row := range report.Rows {
		if row != nil && row.Ready {
			ready++
		}
	}

	line := selectedReadyLine(report, ready)
	if report.Release != "" {
		line += " for " + report.Release
	}

	_, err := fmt.Fprintln(w, line)

	return err
}

// selectedReadyLine counts active rows. Archived rows are a separate count, not part of the denominator.
func selectedReadyLine(report *WorkspaceReport, ready int) string {
	if report.ActionableCount == 0 && report.SkippedArchivedCount > 0 {
		return fmt.Sprintf("Selected repositories: 0 active, %d archived", report.SkippedArchivedCount)
	}

	line := fmt.Sprintf("Selected repositories: %d/%d ready", ready, report.ActionableCount)
	if report.SkippedArchivedCount > 0 {
		line += fmt.Sprintf(", %d archived", report.SkippedArchivedCount)
	}

	return line
}

// writeInventoryHead prints the catalog status and scope.
func writeInventoryHead(w io.Writer, inventory *RemoteInventory) error {
	switch inventory.Status {
	case remoteStatusChecked:
		if _, err := fmt.Fprintln(w, "GitLab inventory: checked now"); err != nil {
			return err
		}
	case remoteStatusFailed:
		if _, err := fmt.Fprintln(w, "GitLab inventory: failed"); err != nil {
			return err
		}
	default:
		if _, err := fmt.Fprintln(w, remoteNotCheckedMsg); err != nil {
			return err
		}
	}

	if inventory.Error != "" && inventory.Status != remoteStatusChecked {
		if _, err := fmt.Fprintln(w, inventory.Error); err != nil {
			return err
		}
	}

	if inventory.URL == "" {
		return nil
	}

	_, err := fmt.Fprintf(
		w,
		"Scope: %s / %s (including subgroups)\n",
		inventoryHost(inventory.URL),
		strings.Join(inventory.Groups, ", "),
	)

	return err
}

// writeCatalogLists prints only the buckets that need attention.
func writeCatalogLists(w io.Writer, catalog *RemoteCatalog) error {
	if _, err := fmt.Fprintf(w, "Visible projects: %d\n", catalog.VisibleCount); err != nil {
		return err
	}

	notCloned := &catalogSection{
		title: "Not cloned, active:",
		paths: catalog.NotCloned,
	}
	notClonedArchived := &catalogSection{
		title: "Not cloned, archived:",
		paths: catalog.NotClonedArchived,
	}
	becameArchived := &catalogSection{
		title: "Now archived:",
		paths: catalog.BecameArchived,
	}
	becameActive := &catalogSection{
		title: "Now active:",
		paths: catalog.BecameActive,
	}
	localUnlisted := &catalogSection{
		title: "Cloned, not in workspace:",
		paths: catalog.LocalUnlisted,
	}
	conflicts := &catalogSection{
		title: "Conflicts:",
		paths: catalog.Conflicts,
	}
	differentPath := &catalogSection{
		title: "Different path:",
		paths: catalog.DifferentPath,
	}
	notReturned := &catalogSection{
		title: "Not returned:",
		paths: catalog.NotReturned,
	}
	outsideScope := &catalogSection{
		title: "Outside scope:",
		paths: catalog.OutsideScope,
	}
	skippedEmpty := &catalogSection{
		title: "No default branch:",
		paths: catalog.SkippedEmpty,
	}
	sections := []*catalogSection{
		notCloned,
		notClonedArchived,
		becameArchived,
		becameActive,
		localUnlisted,
		conflicts,
		differentPath,
		notReturned,
		outsideScope,
		skippedEmpty,
	}

	for _, section := range sections {
		if err := writeNameSection(w, section.title, section.paths); err != nil {
			return err
		}
	}

	return nil
}

// writeNameSection writes a heading and one path per line.
func writeNameSection(w io.Writer, title string, paths []string) error {
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

// writeNextCommands suggests clone commands and does not run them.
func writeNextCommands(w io.Writer, cfg *Config, catalog *RemoteCatalog) error {
	if cfg == nil {
		return nil
	}

	if len(catalog.NotCloned) > 0 || len(catalog.LocalUnlisted) > 0 {
		_, err := fmt.Fprintf(
			w,
			"Next: clone missing projects and include the visible scope in this workspace.\n"+
				"%s\n  %s\nOr choose a project:\n  %s\n",
			shellHint(),
			cloneAllCommand(cfg),
			cloneOneCommand(cfg, firstAttention(catalog), false),
		)
		if err != nil {
			return err
		}
	}

	if len(catalog.NotClonedArchived) == 0 {
		return nil
	}

	_, err := fmt.Fprintf(
		w,
		"To download an archived project:\n  %s\n",
		cloneOneCommand(cfg, catalog.NotClonedArchived[0], true),
	)

	return err
}

// catalogQuiet reports a checked catalog with nothing to show besides the count.
func catalogQuiet(catalog *RemoteCatalog) bool {
	return len(catalog.NotCloned) == 0 && len(catalog.NotClonedArchived) == 0 &&
		len(catalog.BecameArchived) == 0 && len(catalog.BecameActive) == 0 &&
		len(catalog.LocalUnlisted) == 0 &&
		len(catalog.Conflicts) == 0 && len(catalog.DifferentPath) == 0 &&
		len(catalog.NotReturned) == 0 && len(catalog.OutsideScope) == 0 &&
		len(catalog.SkippedEmpty) == 0
}

// firstAttention picks one path for the single-repo example.
func firstAttention(catalog *RemoteCatalog) string {
	if len(catalog.NotCloned) > 0 {
		return catalog.NotCloned[0]
	}

	if len(catalog.LocalUnlisted) > 0 {
		return catalog.LocalUnlisted[0]
	}

	return "group/project"
}

// cloneAllCommand is the --all invocation for this workspace.
func cloneAllCommand(cfg *Config) string {
	return "release-align workspace clone --workspace " + shellArg(cfg.WorkspaceFile) +
		" --base-dir " + shellArg(cfg.BaseDir) + " --all"
}

// cloneOneCommand is one --repo invocation.
func cloneOneCommand(cfg *Config, path string, includeArchived bool) string {
	command := "release-align workspace clone --workspace " + shellArg(cfg.WorkspaceFile) +
		" --base-dir " + shellArg(cfg.BaseDir) + " --repo " + shellArg(path)
	if includeArchived {
		command += " --include-archived"
	}

	return command
}

// inventoryHost returns the host portion of a GitLab URL.
func inventoryHost(raw string) string {
	const prefix = "https://"

	host := strings.TrimPrefix(raw, prefix)
	host, _, _ = strings.Cut(host, "/")

	return host
}

// shellHint names the shell the suggested commands are quoted for.
func shellHint() string {
	if runtime.GOOS == goosWindows {
		return "PowerShell:"
	}

	return "POSIX shell:"
}

// shellArg quotes one argument for the shell named by shellHint.
func shellArg(value string) string {
	if runtime.GOOS == goosWindows {
		return quotePowerShell(value)
	}

	return quotePOSIX(value)
}

// quotePOSIX preserves one literal argument for a POSIX shell.
func quotePOSIX(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// quotePowerShell preserves one literal argument for PowerShell single quotes.
func quotePowerShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
