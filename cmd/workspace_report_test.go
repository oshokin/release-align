package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oshokin/release-align/internal/app"
)

// TestRefreshReportShowsTheWrittenFileWhenDeleteStops keeps a stored drop visible after a delete error.
func TestRefreshReportShowsTheWrittenFileWhenDeleteStops(t *testing.T) {
	t.Parallel()

	handler := &workspaceRefreshCommand{
		file:   "workspace.yml",
		delete: true,
	}
	result := &app.WorkspaceRefreshResult{
		Listed:  2,
		Remote:  true,
		Written: true,
		Removed: []string{"group/legacy"},
	}

	var out bytes.Buffer

	err := handler.writeRefreshReport(&out, result, errWorkspaceRefreshFlags)
	text := out.String()

	if err != nil ||
		!strings.Contains(text, "Refresh: incomplete.") ||
		!strings.Contains(text, "Workspace file updated: 1 entry removed.") ||
		!strings.Contains(text, "Directories deleted: 0.") ||
		!strings.Contains(text, "Deletion did not complete: group/legacy.") ||
		strings.Contains(text, "Directories were kept.") {
		t.Fatal(text, err)
	}
}

// TestCloneReportNamesPublicationFailure does not treat a YAML miss as a finished clone.
func TestCloneReportNamesPublicationFailure(t *testing.T) {
	t.Parallel()

	handler := new(workspaceCloneCommand)
	report := &app.CloneReport{
		Cloned:   3,
		Reused:   1,
		Recovery: "release-align workspace clone --repo group/search",
		Error:    "write failed",
		Next:     "release-align workspace sync",
	}

	var out bytes.Buffer

	err := handler.formatCloneReport(&out, report)
	text := out.String()

	if err != nil ||
		!strings.Contains(text, "Clone: incomplete.") ||
		!strings.Contains(text, "Downloaded: 3; reused: 1; added to workspace: 0.") ||
		!strings.Contains(text, "Workspace file was not updated. Downloaded directories were kept.") ||
		!strings.Contains(text, "Recovery:") ||
		strings.Contains(text, "Next:") {
		t.Fatal(text, err)
	}
}

// TestArchiveInterruptedTextDoesNotClaimAFile stays silent about a ZIP that was not published.
func TestArchiveInterruptedTextDoesNotClaimAFile(t *testing.T) {
	t.Parallel()

	handler := new(workspaceArchiveCommand)
	report := &app.ArchiveReport{Error: context.Canceled.Error()}

	var out bytes.Buffer

	err := handler.formatArchiveReport(&out, report, context.Canceled)
	text := out.String()

	if err != nil ||
		!strings.Contains(text, "Archive: interrupted.") ||
		!strings.Contains(text, "No new archive was published.") ||
		strings.Contains(text, "context canceled") {
		t.Fatal(text, err)
	}
}

// TestCloneJSONIncludesABrokenTimeout covers a config error that used to skip the JSON document.
func TestCloneJSONIncludesABrokenTimeout(t *testing.T) {
	t.Setenv("RELEASE_ALIGN_CLONE_TIMEOUT", "broken")

	var out, diagnostic bytes.Buffer

	args := []string{
		"workspace", "clone",
		"--output", "json",
		"--base-dir", t.TempDir(),
		"--repo", "group/search",
	}
	code := Execute(args, &out, &diagnostic)

	var report map[string]any

	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}

	if code != exitUsage || report["error"] == nil || report["error"] == "" {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), diagnostic.String())
	}
}

// TestArchiveJSONIncludesAMissingWorkspace covers an early load error in JSON mode.
func TestArchiveJSONIncludesAMissingWorkspace(t *testing.T) {
	var out, diagnostic bytes.Buffer

	args := []string{
		"workspace", "archive",
		"--output", "json",
		"--base-dir", t.TempDir(),
		"--workspace", filepath.Join(t.TempDir(), "missing.yml"),
		"--file", filepath.Join(t.TempDir(), "out.zip"),
	}
	code := Execute(args, &out, &diagnostic)

	var report map[string]any

	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}

	if code == exitOK || report["error"] == nil || report["error"] == "" {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), diagnostic.String())
	}
}
