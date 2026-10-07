package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestHelpVersionAndInvalidFlags verifies help, version, and rejected flags.
func TestHelpVersionAndInvalidFlags(t *testing.T) {
	commands := [][]string{
		{"--help"},
		{"version"},
		{"--version"},
	}

	for _, args := range commands {
		var out, err bytes.Buffer

		if code := Execute(args, &out, &err); code != exitOK {
			t.Fatal(args, code, err.String())
		}
	}

	var out, err bytes.Buffer

	if code := Execute([]string{"--jobs=0"}, &out, &err); code != exitUsage || !strings.Contains(err.String(), "jobs") {
		t.Fatal(code, err.String())
	}
}

// TestCobraHelpVersionAndCompletionIgnoreInvalidEnvironment verifies that help, version, and completion ignore a broken environment.
func TestCobraHelpVersionAndCompletionIgnoreInvalidEnvironment(t *testing.T) {
	t.Setenv("JOBS", "invalid")

	commands := [][]string{
		{"--help"},
		{"help"},
		{"version"},
		{"--version"},
		{"completion", "bash"},
		{"status", "--help"},
		{"workspace", "--help"},
		{"workspace", "init", "--help"},
		{"workspace", "refresh", "--help"},
		{"workspace", "clone", "--help"},
		{"workspace", "archive", "--help"},
	}

	for _, args := range commands {
		var out, err bytes.Buffer

		if code := Execute(args, &out, &err); code != exitOK || out.Len() == 0 {
			t.Fatalf("%v: code=%d out=%s err=%s", args, code, out.String(), err.String())
		}
	}
}

// TestCobraFlagsOverrideEnvironmentAndCommandsDoNotShareState verifies that flags beat the environment and commands do not share state.
func TestCobraFlagsOverrideEnvironmentAndCommandsDoNotShareState(t *testing.T) {
	t.Setenv("JOBS", "invalid")

	var out, err bytes.Buffer

	overridden := []string{"--jobs", "2", "--workspace", "missing.json", "--output", "json"}
	if code := Execute(overridden, &out, &err); code != exitUsage || !strings.Contains(out.String(), "missing.json") {
		t.Fatalf("%d %s %s", code, out.String(), err.String())
	}

	out.Reset()
	err.Reset()

	fromEnv := []string{"--workspace", "missing.json", "--output", "json"}
	if code := Execute(fromEnv, &out, &err); code != exitUsage || !strings.Contains(out.String(), "JOBS") {
		t.Fatalf("%d %s %s", code, out.String(), err.String())
	}
}

// TestWorkspaceUsageAndJSONEnvelope verifies usage errors and the single JSON error envelope.
func TestWorkspaceUsageAndJSONEnvelope(t *testing.T) {
	rejected := [][]string{
		{"--group", "search"},
		{"--repo", "search/mailbek"},
		{"--workspace", "missing.json", "--versions-file", "versions.json"},
		{"--workspace", "missing.json", "--depth", "3"},
		{"--workspace", "missing.json", "--local", "keep"},
		{"status"},
		{"status", "--dry-run", "--workspace", "missing.json"},
	}

	for _, args := range rejected {
		var out, err bytes.Buffer

		if code := Execute(args, &out, &err); code != exitUsage {
			t.Fatalf("%v: %d %s", args, code, err.String())
		}
	}

	var out, err bytes.Buffer

	if code := Execute([]string{"--output", "json"}, &out, &err); code != exitUsage || !json.Valid(out.Bytes()) {
		t.Fatalf("%d %s %s", code, out.String(), err.String())
	}

	if strings.Contains(out.String(), "\x1b") || !strings.Contains(err.String(), "workspace") {
		t.Fatalf("out=%s err=%s", out.String(), err.String())
	}
}

// TestHelpMentionsExactWorkspaceTargets verifies that help describes exact workspace targets.
func TestHelpMentionsExactWorkspaceTargets(t *testing.T) {
	var out, err bytes.Buffer

	if code := Execute(
		[]string{"--help"},
		&out,
		&err,
	); code != exitOK ||
		!strings.Contains(out.String(), "exact targets") {
		t.Fatal(code, out.String(), err.String())
	}

	out.Reset()
	err.Reset()

	if code := Execute(
		[]string{"status", "--help"},
		&out,
		&err,
	); code != exitOK ||
		!strings.Contains(out.String(), "--workspace") {
		t.Fatal(code, out.String(), err.String())
	}
}

// TestCobraRejectsInvalidArguments verifies that unknown arguments are rejected.
func TestCobraRejectsInvalidArguments(t *testing.T) {
	commands := [][]string{
		{"--unknown"},
		{"--jobs", "oops"},
		{"--attempts", "0"},
		{"unexpected"},
		{"version", "unexpected"},
	}

	for _, args := range commands {
		var out, err bytes.Buffer

		if code := Execute(args, &out, &err); code != exitUsage {
			t.Fatalf("%v: %d %s", args, code, err.String())
		}
	}
}
