package app

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
)

// ResolvedRevision is the exact object a project must match.
type ResolvedRevision struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	OID   string `json:"oid"`
}

// ObservedState is the worktree read after planning or checkout.
type ObservedState struct {
	Head      string `json:"head"`
	Branch    string `json:"branch"`
	Dirty     bool   `json:"dirty"`
	Operation string `json:"operation,omitempty"`
	Verified  bool   `json:"verified"`
}

// WorkspaceRow is one selected project in the report.
type WorkspaceRow struct {
	Path       string            `json:"path"`
	Expected   *ResolvedRevision `json:"expected,omitempty"`
	Actual     *ObservedState    `json:"actual,omitempty"`
	Outcome    string            `json:"outcome"`
	ReasonCode string            `json:"reason_code,omitempty"`
	Message    string            `json:"message,omitempty"`
	StashOID   string            `json:"stash_oid,omitempty"`
	Ready      bool              `json:"ready"`
}

// WorkspaceReport is the text and JSON result of a workspace run.
type WorkspaceReport struct {
	SchemaVersion  int             `json:"schema_version"`
	Release        string          `json:"release,omitempty"`
	Mode           string          `json:"mode"`
	Freshness      string          `json:"freshness"`
	DryRun         bool            `json:"dry_run"`
	ExpectedCount  int             `json:"expected_count"`
	InventoryCount int             `json:"inventory_count"`
	Scope          string          `json:"scope,omitempty"`
	Coverage       bool            `json:"coverage_complete"`
	Ready          bool            `json:"ready"`
	Errors         []string        `json:"errors"`
	Rows           []*WorkspaceRow `json:"repositories"`
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
	want := make(map[string]bool, len(expected))
	for _, p := range expected {
		if !canonicalProjectPath(p) || want[p] {
			return fmt.Errorf("%w: %q", errWorkspaceExpectedPath, p)
		}
		want[p] = true
	}
	seen := make(map[string]bool, len(r.Rows))
	allReady := true

	for _, row := range r.Rows {
		if row == nil || !want[row.Path] || seen[row.Path] {
			return errWorkspaceRows
		}
		seen[row.Path] = true
		row.Ready = !r.DryRun && row.MatchesContract()
		allReady = allReady && row.Ready
	}

	for _, p := range expected {
		if !seen[p] {
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
