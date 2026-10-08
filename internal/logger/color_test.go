package logger

import (
	"bytes"
	"os"
	"regexp"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// TestTextLogColorsLevelAndListsFiles verifies colored levels and a file list in the text log.
func TestTextLogColorsLevelAndListsFiles(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	sink := &plainSyncer{
		Writer: &buf,
	}
	core := newTextCore(zapcore.InfoLevel, sink, true)
	log := zap.New(core).Sugar()
	log.With("repo", "NEXA-QA/demo_checks").Warn(
		"skipped: working tree has staged, unstaged or untracked changes\n  a.go\n  b.go",
	)

	out := buf.String()
	plain := stripANSI(out)

	if !strings.Contains(plain, " WARN ") || !strings.Contains(plain, "NEXA-QA/demo_checks  skipped:") {
		t.Fatal(plain)
	}

	if !strings.Contains(plain, "\n  a.go\n  b.go\n") || strings.Contains(plain, "{") {
		t.Fatal(plain)
	}

	if !strings.Contains(out, "\033[1;33mWARN") || !strings.Contains(out, "NEXA-QA/demo_checks") {
		t.Fatal(out)
	}
}

// TestSyncDoesNotFsyncStdout verifies that Sync does not fsync stdout.
func TestSyncDoesNotFsyncStdout(t *testing.T) {
	t.Parallel()

	log := NewWithWriter(zapcore.InfoLevel, os.Stdout)
	if err := log.Sync(); err != nil {
		t.Fatal(err)
	}
}

// TestColorFollowsNO_COLOR verifies that NO_COLOR turns log color off.
func TestColorFollowsNO_COLOR(t *testing.T) {
	t.Parallel()

	blocked := func(string) (string, bool) { return "1", true }
	allowed := func(string) (string, bool) { return "", false }
	dumb := func(string) string { return "dumb" }
	term := func(string) string { return "xterm-256color" }

	if envAllowsColor(blocked, term) || envAllowsColor(allowed, dumb) {
		t.Fatal("color stayed on")
	}

	if !envAllowsColor(allowed, term) {
		t.Fatal("color stayed off")
	}

	var buf bytes.Buffer

	if colorEnabled(&buf) {
		t.Fatal("a buffer is not a terminal")
	}
}

// TestTextLogColorsClockAndPace verifies the clock and the pace values use their own colors.
func TestTextLogColorsClockAndPace(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	sink := &plainSyncer{
		Writer: &buf,
	}
	core := newTextCore(zapcore.InfoLevel, sink, true)
	log := zap.New(core).Sugar()
	log.Infow("fetched", "elapsed", "4s", "left", "12s", "percent", "25%")

	out := buf.String()
	for _, code := range []string{ansiTime, ansiElapsed + "4s", ansiLeft + "12s", ansiPercent + "25%"} {
		if !strings.Contains(out, code) {
			t.Fatal(out)
		}
	}
}

// TestTextLogSeparatesFieldsWithCommas verifies key=value pairs are comma-separated.
func TestTextLogSeparatesFieldsWithCommas(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := NewWithWriter(zapcore.InfoLevel, &buf)
	log.Infow("fetched", "repo", "group/name", "outcome", "updated", "ready", false)

	plain := stripANSI(buf.String())
	if !strings.Contains(plain, "group/name  fetched, outcome=updated, ready=false") {
		t.Fatal(plain)
	}
}

// stripANSI removes color codes from a log line.
func stripANSI(s string) string {
	return regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(s, "")
}
