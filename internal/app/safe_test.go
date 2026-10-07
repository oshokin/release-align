package app

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"go.uber.org/zap/zapcore"

	"github.com/oshokin/release-align/internal/logger"
)

// TestPorcelainPaths verifies rename and path parsing from porcelain status.
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

// TestDirtyTreeReasonLimitsPaths verifies that a dirty-tree message keeps only the first paths.
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

// TestDirtyPathLimitFollowsLevel verifies that the debug log level lists every dirty path.
func TestDirtyPathLimitFollowsLevel(t *testing.T) {
	t.Parallel()

	info := logger.ToContext(context.Background(), logger.NewWithWriter(zapcore.InfoLevel, io.Discard))
	debug := logger.ToContext(context.Background(), logger.NewWithWriter(zapcore.DebugLevel, io.Discard))

	r := new(runner)
	if r.dirtyPathLimit(info) != maxDirtyPaths || r.dirtyPathLimit(debug) != 0 {
		t.Fatal(r.dirtyPathLimit(info), r.dirtyPathLimit(debug))
	}
}
