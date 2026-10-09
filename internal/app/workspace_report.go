package app

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

// ResolvedRevision is the exact object a project must match.
type ResolvedRevision struct {
	// Kind is branch, tag, or commit.
	Kind string `json:"kind"`
	// Value is the requested branch, tag, or commit name.
	Value string `json:"value"`
	// OID is the full object id that value resolved to.
	OID string `json:"oid"`
}

// ObservedState is the worktree read after planning or checkout.
type ObservedState struct {
	// Head is the commit currently checked out.
	Head string `json:"head"`
	// Branch is the current branch name, empty when detached.
	Branch string `json:"branch"`
	// Dirty reports uncommitted changes that block a switch.
	Dirty bool `json:"dirty"`
	// Operation is the Git command that was in progress, if any.
	Operation string `json:"operation,omitempty"`
	// Verified reports that the observed commit was read from the worktree.
	Verified bool `json:"verified"`
}

// WorkspaceRow is one selected project in the report.
type WorkspaceRow struct {
	// Path is the workspace project path.
	Path string `json:"path"`
	// Expected is the revision the run was asked to match.
	Expected *ResolvedRevision `json:"expected,omitempty"`
	// Actual is the worktree state after the operation.
	Actual *ObservedState `json:"actual,omitempty"`
	// Outcome is the short result name for this row.
	Outcome string `json:"outcome"`
	// ReasonCode is a stable code when the row is not ready.
	ReasonCode string `json:"reason_code,omitempty"`
	// Message explains the outcome in plain text.
	Message string `json:"message,omitempty"`
	// StashOID is set only when a stash was recorded. This run does not stash.
	StashOID string `json:"stash_oid,omitempty"`
	// Ready reports that this selected project matches its revision.
	Ready bool `json:"ready"`
}

// WorkspaceReport is the text and JSON result of a workspace run.
type WorkspaceReport struct {
	// SchemaVersion is the report document version.
	SchemaVersion int `json:"schema_version"`
	// Release is the requested release name when one was set.
	Release string `json:"release,omitempty"`
	// Mode is status, sync, or dry-run.
	Mode string `json:"mode"`
	// Freshness says whether refs were read locally or fetched.
	Freshness string `json:"freshness"`
	// DryRun reports that no checkout was attempted.
	DryRun bool `json:"dry_run"`
	// ExpectedCount is the number of selected projects.
	ExpectedCount int `json:"expected_count"`
	// InventoryCount is the number of rows written.
	InventoryCount int `json:"inventory_count"`
	// Scope is the workspace group filter, when one was set.
	Scope string `json:"scope,omitempty"`
	// Coverage reports that every selected project has a row.
	Coverage bool `json:"coverage_complete"`
	// Ready reports that every selected project matches.
	Ready bool `json:"ready"`
	// Errors lists failures of this run. It is empty, not null, when there are none.
	Errors []string `json:"errors"`
	// Rows are the selected projects in workspace order.
	Rows []*WorkspaceRow `json:"repositories"`
	// RemoteInventory is the GitLab comparison for this invocation.
	RemoteInventory *RemoteInventory `json:"remote_inventory,omitempty"`
	// Started is when this run began. It is not part of the JSON report.
	Started time.Time `json:"-"`
	// ProgressStarted is when the latest phase began.
	ProgressStarted time.Time `json:"-"`
	// ProgressDone is how many units of the latest phase have finished.
	ProgressDone int `json:"-"`
	// ProgressTotal is the size of the latest phase.
	ProgressTotal int `json:"-"`
}

// RemoteInventory is the GitLab catalog comparison for one invocation.
// Counts are present only after a complete listing.
type RemoteInventory struct {
	// Status is checked, failed, or not_checked.
	Status string `json:"status"`
	// CheckedAt is the local time of a completed listing.
	CheckedAt string `json:"checked_at,omitempty"`
	// URL is the GitLab origin from the workspace file.
	URL string `json:"url,omitempty"`
	// Groups are the namespace paths that were listed.
	Groups []string `json:"groups,omitempty"`
	// IncludeSubgroups reports that subgroups were requested.
	IncludeSubgroups *bool `json:"include_subgroups,omitempty"`
	// IncludeArchived reports that archived projects were excluded.
	IncludeArchived *bool `json:"include_archived,omitempty"`
	// IncludeShared reports that shared projects were excluded.
	IncludeShared *bool `json:"include_shared,omitempty"`
	// Reason explains why a check was skipped.
	Reason string `json:"reason,omitempty"`
	// Error is the inventory failure text. It is absent when the listing completed.
	Error string `json:"error,omitempty"`
	// Catalog is present only after every configured group was read.
	Catalog *RemoteCatalog `json:"catalog,omitempty"`
}

// RemoteCatalog is the diff between GitLab, the disk, and the workspace file.
type RemoteCatalog struct {
	// VisibleCount is the number of non-archived projects in the listing.
	VisibleCount int `json:"visible_count"`
	// NotCloned lists projects that can be downloaded.
	NotCloned []string `json:"not_cloned"`
	// LocalUnlisted lists clones that are not yet in the workspace file.
	LocalUnlisted []string `json:"local_unlisted"`
	// Conflicts lists paths that must not be replaced automatically.
	Conflicts []string `json:"conflicts"`
	// NotReturned lists workspace paths missing from a complete listing of their group.
	NotReturned []string `json:"not_returned"`
	// DifferentPath lists projects whose clone is not at the expected path.
	DifferentPath []string `json:"different_path"`
	// OutsideScope lists workspace paths that are not in the configured groups.
	OutsideScope []string `json:"outside_scope"`
	// SkippedEmpty lists projects that have no default branch.
	SkippedEmpty []string `json:"skipped_empty"`
}

// MatchesContract is independent of outcome text and refuses unverified state.
func (r *WorkspaceRow) MatchesContract() bool {
	if r == nil || r.Expected == nil || r.Actual == nil || r.ReasonCode != "" || r.StashOID != "" {
		return false
	}

	e, a := r.Expected, r.Actual
	if !a.Verified || a.Dirty || a.Operation != "" || !workspaceOID.MatchString(e.OID) || a.Head != e.OID {
		return false
	}

	switch e.Kind {
	case revisionBranch:
		return e.Value != "" && a.Branch == e.Value
	case revisionTag:
		return e.Value != ""
	case revisionCommit:
		return e.Value == e.OID
	default:
		return false
	}
}

// Finalize validates complete coverage before computing readiness.
// The collector owns this report exclusively; no worker mutates it concurrently.
func (r *WorkspaceReport) Finalize(expected []string) error {
	if r == nil {
		return errWorkspaceNilReport
	}

	r.Ready, r.Coverage = false, false
	r.SchemaVersion, r.ExpectedCount = 1, len(expected)

	if r.Errors == nil {
		r.Errors = []string{}
	}

	want := make(map[string]struct{}, len(expected))
	for _, p := range expected {
		_, duplicate := want[p]
		if !canonicalProjectPath(p) || duplicate {
			return fmt.Errorf("%w: %q", errWorkspaceExpectedPath, p)
		}

		want[p] = struct{}{}
	}

	seen := make(map[string]struct{}, len(r.Rows))
	allReady := true

	for _, row := range r.Rows {
		if row == nil {
			return errWorkspaceRows
		}

		_, expected := want[row.Path]
		_, duplicate := seen[row.Path]

		if !expected || duplicate {
			return errWorkspaceRows
		}

		seen[row.Path] = struct{}{}
		row.Ready = !r.DryRun && row.MatchesContract()
		allReady = allReady && row.Ready
	}

	for _, p := range expected {
		if _, found := seen[p]; !found {
			missing := &WorkspaceRow{
				Path:       p,
				Outcome:    outcomeNotStarted,
				ReasonCode: reasonNotObserved,
				Message:    "No final observation was collected",
			}
			r.Rows = append(r.Rows, missing)
			allReady = false
		}
	}

	slices.SortFunc(r.Rows, func(a, b *WorkspaceRow) int { return strings.Compare(a.Path, b.Path) })
	// Coverage means every expected item has a row, not every repo exists on disk.
	r.Coverage = len(r.Rows) == len(expected)
	r.Ready = len(expected) > 0 && r.Coverage && allReady && !r.DryRun && len(r.Errors) == 0

	return nil
}

// WriteWorkspaceReport writes one JSON document and a trailing newline.
func WriteWorkspaceReport(dst io.Writer, report *WorkspaceReport) error {
	if report == nil {
		return errWorkspaceNilReport
	}

	enc := json.NewEncoder(dst)
	enc.SetIndent("", "  ")

	return enc.Encode(report)
}
