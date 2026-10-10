package app

import (
	"bytes"
	"strings"
	"testing"
)

// TestRemoteTextKeepsTheBlockingReason prints the dirty path without using the logger.
func TestRemoteTextKeepsTheBlockingReason(t *testing.T) {
	t.Parallel()

	row := &WorkspaceRow{
		Path:       "group/search",
		Outcome:    outcomeBlocked,
		ReasonCode: reasonDirty,
		Message:    messageDirtyTree,
	}
	report := &WorkspaceReport{
		Mode:            ModeStatus,
		Freshness:       freshnessCached,
		ActionableCount: 1,
		Rows:            []*WorkspaceRow{row},
		RemoteInventory: &RemoteInventory{
			Status: remoteStatusSkipped,
			Error:  remoteNotCheckedMsg,
		},
	}

	var out bytes.Buffer

	cfg := new(Config)
	if err := WriteRemoteInventory(&out, cfg, report); err != nil {
		t.Fatal(err)
	}

	text := out.String()
	if !strings.Contains(text, "Status: not ready.") ||
		!strings.Contains(text, "Ready: 0/1 active.") ||
		!strings.Contains(text, "group/search: "+messageDirtyTree) ||
		!strings.Contains(text, "Refs: cached.") ||
		strings.Count(text, remoteNotCheckedMsg) != 1 {
		t.Fatal(text)
	}
}

// TestRemoteTextDescribesAValidPlan does not call a dry-run a failed status.
func TestRemoteTextDescribesAValidPlan(t *testing.T) {
	t.Parallel()

	row := &WorkspaceRow{
		Path:     "group/search",
		Outcome:  outcomePlanned,
		Message:  messagePlanAdmissible,
		Expected: &ResolvedRevision{Kind: revisionBranch, Value: "Release-26.3.0", OID: "abc"},
		Actual:   &ObservedState{Head: "def", Branch: "other", Verified: true},
	}
	report := &WorkspaceReport{
		Mode:            ModePlan,
		DryRun:          true,
		Freshness:       freshnessCached,
		ActionableCount: 1,
		Rows:            []*WorkspaceRow{row},
	}

	var out bytes.Buffer

	cfg := new(Config)
	if err := WriteRemoteInventory(&out, cfg, report); err != nil {
		t.Fatal(err)
	}

	text := out.String()
	if !strings.Contains(text, "Plan: valid.") ||
		!strings.Contains(text, "No checkout or merge performed.") ||
		!strings.Contains(text, "group/search: would switch to Release-26.3.0") ||
		strings.Contains(text, "not ready") {
		t.Fatal(text)
	}
}

// TestRemoteTextNamesABlockedPlan keeps a blocker out of a valid plan.
func TestRemoteTextNamesABlockedPlan(t *testing.T) {
	t.Parallel()

	row := &WorkspaceRow{
		Path:       "group/search",
		Outcome:    outcomeBlocked,
		ReasonCode: reasonDirty,
		Message:    messageDirtyTree,
	}
	report := &WorkspaceReport{
		Mode:            ModePlan,
		DryRun:          true,
		Freshness:       freshnessCached,
		ActionableCount: 1,
		Rows:            []*WorkspaceRow{row},
	}

	var out bytes.Buffer

	cfg := new(Config)
	if err := WriteRemoteInventory(&out, cfg, report); err != nil {
		t.Fatal(err)
	}

	text := out.String()
	if !strings.Contains(text, "Plan: blocked.") ||
		!strings.Contains(text, "group/search: "+messageDirtyTree) ||
		strings.Contains(text, "Plan: valid.") {
		t.Fatal(text)
	}
}

// TestRemoteTextDoesNotCallArchivedRowsReady leaves an archived-only selection without a ready claim.
func TestRemoteTextDoesNotCallArchivedRowsReady(t *testing.T) {
	t.Parallel()

	report := &WorkspaceReport{
		Mode:                 ModeStatus,
		Freshness:            freshnessCached,
		SkippedArchivedCount: 2,
		Rows: []*WorkspaceRow{{
			Path:       "group/old",
			Outcome:    outcomeSkipped,
			ReasonCode: reasonArchived,
		}},
	}

	var out bytes.Buffer

	cfg := new(Config)
	if err := WriteRemoteInventory(&out, cfg, report); err != nil {
		t.Fatal(err)
	}

	text := out.String()
	if !strings.Contains(text, "No active repositories selected; 2 archived skipped.") ||
		strings.Contains(text, "ready") {
		t.Fatal(text)
	}
}
