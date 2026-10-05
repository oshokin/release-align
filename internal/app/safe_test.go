package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"go.uber.org/zap/zapcore"

	"github.com/oshokin/release-align/internal/logger"
)

func TestPorcelainPaths(t *testing.T) {
	t.Parallel()

	r := new(runner)
	got := r.porcelainPaths("R  renamed\x00file\x00?? file\x00?? new file\x00?? untracked\x00")
	want := []string{"file -> renamed", "file", "new file", "untracked"}

	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatal(got)
	}

	if len(r.porcelainPaths("")) != 0 {
		t.Fatal("empty status")
	}
}

func TestLogNotUpdatedSortsByPath(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	ctx := logger.ToContext(context.Background(), logger.NewWithWriter(nil, &buf))
	skipped := &RepositoryReport{
		Path:   "b/repo",
		Status: statusSkipped,
		Reason: "working tree has staged, unstaged or untracked changes\n  file",
	}
	failed := &RepositoryReport{
		Path:   "a/repo",
		Status: statusFailed,
		Reason: "fetch failed",
	}
	summary := &Summary{NotUpdated: []*RepositoryReport{skipped, failed}}

	LogNotUpdated(ctx, summary)

	log := buf.String()
	header := strings.Index(log, "Repositories not updated:")
	first := strings.Index(log, "a/repo [failed] fetch failed")
	second := strings.Index(log, "b/repo [skipped] working tree has staged, unstaged or untracked changes\n  file")

	if header < 0 || first < header || second < first {
		t.Fatal(log)
	}

	buf.Reset()
	LogNotUpdated(ctx, nil)

	empty := new(Summary)
	LogNotUpdated(ctx, empty)

	if buf.Len() != 0 {
		t.Fatal(buf.String())
	}
}

func TestDirtyTreeReasonLimitsPaths(t *testing.T) {
	t.Parallel()

	var status strings.Builder

	for i := range 12 {
		fmt.Fprintf(&status, "?? p%02d\x00", i)
	}

	r := new(runner)
	got := r.dirtyTreeReason(status.String(), maxDirtyPaths)

	if !strings.Contains(got, "\n  p00\n") || !strings.Contains(got, "\n  p09\n") {
		t.Fatal(got)
	}

	if strings.Contains(got, "p10") || !strings.Contains(got, "... and 2 more") {
		t.Fatal(got)
	}

	all := r.dirtyTreeReason(status.String(), 0)
	if !strings.Contains(all, "p11") || strings.Contains(all, "more") {
		t.Fatal(all)
	}
}

func TestDirtyPathLimitFollowsLevel(t *testing.T) {
	t.Parallel()

	info := logger.ToContext(context.Background(), logger.NewWithWriter(zapcore.InfoLevel, io.Discard))
	debug := logger.ToContext(context.Background(), logger.NewWithWriter(zapcore.DebugLevel, io.Discard))

	r := new(runner)
	if r.dirtyPathLimit(info) != maxDirtyPaths || r.dirtyPathLimit(debug) != 0 {
		t.Fatal(r.dirtyPathLimit(info), r.dirtyPathLimit(debug))
	}
}

func TestIdleCancelIsOnlyACount(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	ctx := logger.ToContext(context.Background(), logger.NewWithWriter(nil, &buf))
	summary := new(Summary)
	repo := &repository{relative: "g/idle"}
	res := &result{repo, statusCanceled, messageIdleCancel}
	r := new(runner)

	r.record(ctx, summary, res)

	if summary.Canceled != 1 || len(summary.NotUpdated) != 0 || buf.Len() != 0 {
		t.Fatalf("%+v %s", summary, buf.String())
	}
}
