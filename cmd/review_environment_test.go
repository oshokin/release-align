package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestReviewWorkspaceIgnoresLegacyEnvironment confirms irrelevant values cannot reject the inventory mode.
func TestReviewWorkspaceIgnoresLegacyEnvironment(t *testing.T) {
	t.Setenv("MAX_DEPTH", "oops")
	t.Setenv("RELEASE_BRANCH", "-invalid")
	t.Setenv("VERSIONS_FILE", "unrelated.json")

	var out, errOut bytes.Buffer

	args := []string{"workspace", "status", "--workspace", "missing.json", "--output", "json"}
	code := Execute(args, &out, &errOut)

	if code != exitUsage || !json.Valid(out.Bytes()) || !strings.Contains(out.String(), "missing.json") {
		t.Fatalf("%d %s %s", code, out.String(), errOut.String())
	}
}

// TestReviewExplicitFlagOverridesInvalidEnvironment verifies flags win before environment parsing.
func TestReviewExplicitFlagOverridesInvalidEnvironment(t *testing.T) {
	t.Setenv("RELEASE_ALIGN_JOBS", "invalid")

	var out, errOut bytes.Buffer

	args := []string{
		"workspace", "sync",
		"--dry-run",
		"--base-dir",
		t.TempDir(),
		"--jobs",
		"2",
		"--workspace",
		"missing.json",
		"--output",
		"json",
	}

	if code := Execute(args, &out, &errOut); code != exitUsage || !strings.Contains(out.String(), "missing.json") {
		t.Fatalf("%d %s %s", code, out.String(), errOut.String())
	}
}

// TestReviewEnvironmentFailureHasJSONEnvelope verifies a post-parse usage error is machine-readable.
func TestReviewEnvironmentFailureHasJSONEnvelope(t *testing.T) {
	t.Setenv("RELEASE_ALIGN_JOBS", "invalid")

	var out, errOut bytes.Buffer

	args := []string{"workspace", "status", "--workspace", "missing.json", "--output", "json"}

	if code := Execute(args, &out, &errOut); code != exitUsage {
		t.Fatalf("%d %s %s", code, out.String(), errOut.String())
	}

	text := out.String()
	dec := json.NewDecoder(strings.NewReader(text))

	var doc map[string]any

	if err := dec.Decode(&doc); err != nil || dec.More() || strings.Contains(text, "Checking origin") {
		t.Fatalf("stdout=%s stderr=%s err=%v", text, errOut.String(), err)
	}
}
