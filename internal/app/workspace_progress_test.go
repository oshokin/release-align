package app

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap/zapcore"

	"github.com/oshokin/release-align/internal/logger"
)

// TestLogWorkspaceReportCollapsesCanceledRows keeps one line for an untouched remainder.
func TestLogWorkspaceReportCollapsesCanceledRows(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	ctx := logger.ToContext(t.Context(), logger.NewWithWriter(zapcore.InfoLevel, &buf))
	kept := &WorkspaceRow{
		Path:       "a/kept",
		Outcome:    outcomeBlocked,
		ReasonCode: reasonMissingRepository,
		Message:    "repository directory is absent",
	}
	one := &WorkspaceRow{
		Path:       "b/one",
		Outcome:    outcomeCanceled,
		ReasonCode: reasonCanceled,
		Message:    messageRunStopped,
	}
	two := &WorkspaceRow{
		Path:       "c/two",
		Outcome:    outcomeCanceled,
		ReasonCode: reasonCanceled,
		Message:    messageRunStopped,
	}
	rows := []*WorkspaceRow{kept, one, two}
	report := &WorkspaceReport{
		SchemaVersion:   1,
		Mode:            ModeSync,
		Freshness:       freshnessCached,
		Scope:           "workspace",
		ExpectedCount:   3,
		InventoryCount:  3,
		Started:         time.Now().Add(-8 * time.Second),
		ProgressStarted: time.Now().Add(-8 * time.Second),
		ProgressDone:    1,
		ProgressTotal:   4,
		Rows:            rows,
	}

	LogWorkspaceReport(ctx, report, true)

	plain := buf.String()
	if !strings.Contains(plain, "a/kept") || !strings.Contains(plain, "outcome=blocked") {
		t.Fatal(plain)
	}

	if strings.Contains(plain, "b/one") || strings.Contains(plain, "c/two") {
		t.Fatal(plain)
	}

	counted := strings.Contains(plain, "count=2")
	paced := strings.Contains(plain, "percent=25%") && strings.Contains(plain, "left=")

	if !counted || !paced {
		t.Fatal(plain)
	}
}
