package app

import (
	"bytes"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"go.uber.org/zap/zapcore"

	"github.com/oshokin/release-align/internal/logger"
)

// TestLogWorkspaceReportCollapsesCanceledRows keeps one line for an untouched remainder.
func TestLogWorkspaceReportCollapsesCanceledRows(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	kept := &WorkspaceRow{
		Path:       "a/kept",
		Outcome:    outcomeBlocked,
		ReasonCode: reasonDiverged,
		Message:    "HEAD abc on other is not branch master",
	}
	matched := &WorkspaceRow{
		Path:    "d/match",
		Outcome: outcomeObserved,
		Ready:   true,
		Message: "local HEAD matches the requested revision",
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
	rows := []*WorkspaceRow{kept, matched, one, two}
	report := &WorkspaceReport{
		SchemaVersion:  1,
		Mode:           ModeSync,
		Freshness:      freshnessCached,
		Scope:          "workspace",
		ExpectedCount:  4,
		InventoryCount: 4,
		ProgressDone:   1,
		ProgressTotal:  4,
		Rows:           rows,
	}

	synctest.Test(t, func(t *testing.T) {
		ctx := logger.ToContext(t.Context(), logger.NewWithWriter(zapcore.InfoLevel, &buf))
		started := time.Now().Add(-8 * time.Second)
		report.Started = started
		report.ProgressStarted = started
		LogWorkspaceReport(ctx, report, true)
	})

	plain := buf.String()
	warned := strings.Contains(plain, "WARN")
	blocked := strings.Contains(plain, "a/kept") && strings.Contains(plain, "outcome=blocked")

	if !warned || !blocked {
		t.Fatal(plain)
	}

	if !strings.Contains(plain, "INFO") || !strings.Contains(plain, "d/match") {
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

// TestLogWorkspaceReportCollapsesPlanBlocked keeps one count for repositories the plan never started.
func TestLogWorkspaceReportCollapsesPlanBlocked(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	ctx := logger.ToContext(t.Context(), logger.NewWithWriter(zapcore.InfoLevel, &buf))
	blocked := &WorkspaceRow{
		Path:       "a/real",
		Outcome:    outcomeBlocked,
		ReasonCode: reasonGitFailed,
		Message:    "git command failed: exit status 1: error:; fatal: could not read from remote",
	}
	one := &WorkspaceRow{
		Path:       "b/one",
		Outcome:    outcomeNotStarted,
		ReasonCode: reasonPlanBlocked,
		Message:    "another selected repository blocks the workspace plan",
	}
	two := &WorkspaceRow{
		Path:       "c/two",
		Outcome:    outcomeNotStarted,
		ReasonCode: reasonPlanBlocked,
		Message:    "another selected repository blocks the workspace plan",
	}
	rows := []*WorkspaceRow{one, blocked, two}
	report := &WorkspaceReport{
		SchemaVersion:  1,
		Mode:           ModeSync,
		Freshness:      freshnessCached,
		Scope:          "workspace",
		ExpectedCount:  3,
		InventoryCount: 3,
		Rows:           rows,
	}

	LogWorkspaceReport(ctx, report, true)

	plain := buf.String()
	warned := strings.Contains(plain, "a/real") && strings.Contains(plain, "WARN")
	counted := strings.Contains(plain, "count=2") && strings.Contains(plain, "plan_blocked")
	hidden := !strings.Contains(plain, "b/one") && !strings.Contains(plain, "c/two")

	if !warned || !counted || !hidden {
		t.Fatal(plain)
	}
}
