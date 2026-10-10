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

const (
	// textStatus is the status operation name in the text result.
	textStatus = "Status"
	// planValid means a dry-run found no blocker.
	planValid = "valid"
	// runIncomplete means the command did not finish successfully.
	runIncomplete = "incomplete"
	// runInterrupted means the run stopped before it finished.
	runInterrupted = "interrupted"
)

// WriteRemoteInventory writes the plain-text catalog section.
func WriteRemoteInventory(w io.Writer, cfg *Config, report *WorkspaceReport) error {
	if w == nil || report == nil {
		return nil
	}

	if err := writeOperationSummary(w, report); err != nil {
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

// writeOperationSummary prints the command result from the report already built.
// A dry-run plan is not described as a finished checkout.
func writeOperationSummary(w io.Writer, report *WorkspaceReport) error {
	if report == nil || report.Mode == "" {
		return nil
	}

	if report.Mode == ModePlan {
		return writePlanSummary(w, report)
	}

	title := textStatus
	if report.Mode == ModeSync {
		title = "Sync"
	}

	return writeRunSummary(w, report, title)
}

// writeRunSummary prints status or sync. Archived-only selections are not called ready.
func writeRunSummary(w io.Writer, report *WorkspaceReport, title string) error {
	if report.ActionableCount == 0 && report.SkippedArchivedCount > 0 && len(report.Errors) == 0 {
		if _, err := fmt.Fprintf(
			w,
			"No active repositories selected; %d archived skipped.\n",
			report.SkippedArchivedCount,
		); err != nil {
			return err
		}

		return writeRefsLine(w, report)
	}

	state := runState(report, title)
	if _, err := fmt.Fprintln(w, runHeadline(title, state, report.Release)); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(w, activeCountLine(report, state)); err != nil {
		return err
	}

	if err := writeAttention(w, report); err != nil {
		return err
	}

	return writeRefsLine(w, report)
}

// writePlanSummary says whether the cached plan can run. It does not use Ready.
func writePlanSummary(w io.Writer, report *WorkspaceReport) error {
	state := planValid
	if rowsCanceled(report) {
		state = runInterrupted
	} else if planBlocked(report) {
		state = "blocked"
	}

	if _, err := fmt.Fprintf(
		w,
		"Plan: %s. No checkout or merge performed. Refs: %s.\n",
		state,
		refsWord(report),
	); err != nil {
		return err
	}

	if state == planValid {
		return writePlanActions(w, report)
	}

	return writeAttention(w, report)
}

// runState is the word after the operation name.
func runState(report *WorkspaceReport, title string) string {
	if rowsCanceled(report) {
		return runInterrupted
	}

	if len(report.Errors) > 0 {
		return runIncomplete
	}

	if title == textStatus {
		if report.Ready {
			return logReady
		}

		return "not ready"
	}

	if report.Ready {
		return "completed"
	}

	return runIncomplete
}

// runHeadline names the operation. A release label is included when the workspace has one.
func runHeadline(title, state, release string) string {
	if release == "" {
		return title + ": " + state + "."
	}

	return title + ": " + state + " for " + release + "."
}

// activeCountLine counts active rows. Archived rows stay out of the denominator.
func activeCountLine(report *WorkspaceReport, state string) string {
	ready := readyCount(report)

	line := fmt.Sprintf("Ready: %d/%d active", ready, report.ActionableCount)
	if state == logReady {
		line = fmt.Sprintf("Active: %d/%d", ready, report.ActionableCount)
	}

	if report.SkippedArchivedCount > 0 {
		line += fmt.Sprintf("; archived skipped: %d", report.SkippedArchivedCount)
	}

	return line + "."
}

// readyCount is the number of selected rows that already match.
func readyCount(report *WorkspaceReport) int {
	ready := 0

	for _, row := range report.Rows {
		if row != nil && row.Ready {
			ready++
		}
	}

	return ready
}

// writeAttention lists rows that still need a person. Ready and archived skips are omitted.
func writeAttention(w io.Writer, report *WorkspaceReport) error {
	rows := attentionRows(report)
	if len(rows) == 0 {
		return nil
	}

	if _, err := fmt.Fprintln(w, "Needs attention:"); err != nil {
		return err
	}

	for _, row := range rows {
		if _, err := fmt.Fprintf(w, "  %s: %s\n", row.Path, attentionText(row)); err != nil {
			return err
		}
	}

	return nil
}

// attentionRows keeps the rows a person has to look at.
func attentionRows(report *WorkspaceReport) []*WorkspaceRow {
	rows := make([]*WorkspaceRow, 0)

	for _, row := range report.Rows {
		if needsAttention(row) {
			rows = append(rows, row)
		}
	}

	return rows
}

// needsAttention reports a row that is not a finished match, an archived skip, or a planned switch.
func needsAttention(row *WorkspaceRow) bool {
	if row == nil || row.Ready || row.Outcome == outcomeSkipped || row.Outcome == outcomePlanned {
		return false
	}

	if row.Outcome == outcomeObserved || row.Outcome == outcomeUpdated {
		return row.ReasonCode != ""
	}

	return row.Message != "" || row.ReasonCode != "" || row.Outcome != ""
}

// attentionText prefers the row message already stored on the report.
func attentionText(row *WorkspaceRow) string {
	if row.Message != "" {
		return row.Message
	}

	if row.ReasonCode != "" {
		return row.ReasonCode
	}

	return row.Outcome
}

// writePlanActions lists switches the plan would perform. A row already at the pin is omitted.
func writePlanActions(w io.Writer, report *WorkspaceReport) error {
	for _, row := range report.Rows {
		line := planAction(row)
		if line == "" {
			continue
		}

		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}

	return nil
}

// planAction is one dry-run switch. An empty string means the row is not a switch.
func planAction(row *WorkspaceRow) string {
	if row == nil || row.Outcome != outcomePlanned || rowAlreadyAtTarget(row) {
		return ""
	}

	return "  " + row.Path + ": would switch to " + planTarget(row)
}

// planTarget is the revision name the plan would check out.
func planTarget(row *WorkspaceRow) string {
	if row.Expected == nil {
		return row.Message
	}

	if row.Expected.Value != "" {
		return row.Expected.Value
	}

	return row.Expected.OID
}

// rowAlreadyAtTarget reports a planned row whose observed HEAD is already the pin.
func rowAlreadyAtTarget(row *WorkspaceRow) bool {
	if row.Actual == nil || row.Expected == nil {
		return false
	}

	if row.Expected.Kind == revisionBranch && row.Actual.Branch != row.Expected.Value {
		return false
	}

	return row.Actual.Verified && !row.Actual.Dirty && row.Actual.Operation == "" &&
		row.Actual.Head != "" && row.Actual.Head == row.Expected.OID
}

// planBlocked reports a dry-run row that cannot be switched.
func planBlocked(report *WorkspaceReport) bool {
	if len(report.Errors) > 0 {
		return true
	}

	for _, row := range report.Rows {
		if row == nil || row.Outcome == outcomeSkipped || row.Outcome == outcomePlanned ||
			row.Outcome == outcomeCanceled {
			continue
		}

		if row.ReasonCode != "" || row.Outcome == outcomeBlocked {
			return true
		}
	}

	return false
}

// rowsCanceled reports that the run stopped before every selected repository was finished.
func rowsCanceled(report *WorkspaceReport) bool {
	if report.Interrupted {
		return true
	}

	for _, row := range report.Rows {
		if row != nil && (row.Outcome == outcomeCanceled || row.ReasonCode == reasonCanceled) {
			return true
		}
	}

	return false
}

// writeRefsLine says whether the refs were read locally or fetched.
func writeRefsLine(w io.Writer, report *WorkspaceReport) error {
	_, err := fmt.Fprintf(w, "Refs: %s.\n", refsWord(report))

	return err
}

// refsWord is the freshness word used in the text result.
func refsWord(report *WorkspaceReport) string {
	if report != nil && report.Freshness == freshnessFetched {
		return freshnessFetched
	}

	return freshnessCached
}

// writeInventoryHead prints the catalog status and scope.
func writeInventoryHead(w io.Writer, inventory *RemoteInventory) error {
	line := inventoryStatusLine(inventory)
	if _, err := fmt.Fprintln(w, line); err != nil {
		return err
	}

	if inventory.Error != "" && inventory.Status != remoteStatusChecked && inventory.Error != line {
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

// inventoryStatusLine is the one catalog sentence. The same text is not printed again as the error.
func inventoryStatusLine(inventory *RemoteInventory) string {
	switch inventory.Status {
	case remoteStatusChecked:
		return "GitLab inventory: checked now"
	case remoteStatusFailed:
		return "GitLab inventory: failed"
	default:
		return remoteNotCheckedMsg
	}
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
