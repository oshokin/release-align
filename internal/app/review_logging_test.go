package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap/zapcore"

	"github.com/oshokin/release-align/internal/gitter"
	"github.com/oshokin/release-align/internal/logger"
)

// TestReportDoesNotHideRunErrors covers failures before rows and after local readiness.
func TestReportDoesNotHideRunErrors(t *testing.T) {
	t.Parallel()

	cases := []*WorkspaceReport{
		{Mode: ModePlan, Errors: []string{"revision lookup failed"}},
		{Mode: ModeSync, Ready: true, Errors: []string{"inventory failed"}},
		{Mode: ModeStatus, Ready: true, Errors: []string{"inventory failed"}},
	}
	for _, report := range cases {
		var out bytes.Buffer

		cfg := new(Config)
		if err := WriteRemoteInventory(&out, cfg, report); err != nil {
			t.Fatal(err)
		}

		text := out.String()
		if strings.Contains(text, "Plan: valid") || strings.Contains(text, ": completed") ||
			strings.Contains(text, "Status: ready") {
			t.Fatal(text)
		}
	}
}

// TestPlanShowsBranchSwitchAtSameCommit prevents omitting a real checkout from the plan.
func TestPlanShowsBranchSwitchAtSameCommit(t *testing.T) {
	t.Parallel()

	expected := &ResolvedRevision{Kind: revisionBranch, Value: "release", OID: "abc"}
	actual := &ObservedState{Verified: true, Branch: "main", Head: "abc"}
	row := &WorkspaceRow{Path: "group/api", Outcome: outcomePlanned, Expected: expected, Actual: actual}
	report := &WorkspaceReport{Mode: ModePlan, Rows: []*WorkspaceRow{row}}
	cfg := new(Config)

	var out bytes.Buffer

	if err := WriteRemoteInventory(&out, cfg, report); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "group/api: would switch to release") {
		t.Fatal(out.String())
	}
}

// TestPlanCancellationBeforeRowsIsInterrupted covers cancellation during revision resolution.
func TestPlanCancellationBeforeRowsIsInterrupted(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	cfg := DefaultConfig()
	cfg.DryRun = true
	spec := &WorkspaceSpec{SchemaVersion: 1, shortDefault: "master", Projects: []*ProjectSpec{{Path: "group/api"}}}
	cfg.BaseDir = t.TempDir()

	if err := os.MkdirAll(filepath.Join(cfg.BaseDir, "group", "api"), 0o700); err != nil {
		t.Fatal(err)
	}

	report, runErr := RunWorkspace(ctx, cfg, spec, ModeSync)
	if !errors.Is(runErr, context.Canceled) || len(report.Rows) != 0 {
		t.Fatalf("expected cancellation before rows, got %v: %+v", runErr, report)
	}

	var out bytes.Buffer

	if err := WriteRemoteInventory(&out, cfg, report); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "Plan: interrupted.") {
		t.Fatal(out.String())
	}
}

// TestArchiveDoesNotCountAFailedPack verifies progress from a real failed Git invocation.
func TestArchiveDoesNotCountAFailedPack(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	ctx := logger.ToContext(t.Context(), logger.NewWithWriter(zapcore.InfoLevel, &logs))
	dir := t.TempDir()

	temp, err := os.CreateTemp(dir, "archive-*")
	if err != nil {
		t.Fatal(err)
	}

	defer temp.Close()

	opts := &WorkspaceArchiveOptions{ArchiveTimeout: time.Second}
	client := &gitter.Client{LocalTimeout: time.Second, NoLazyFetch: true}
	job := &archiveJob{ctx: ctx, opts: opts, git: client, destination: filepath.Join(dir, "out.zip")}
	repo := &plannedRepo{Path: "group/api", Dir: dir, Commit: strings.Repeat("a", 40)}
	planned := []*plannedRepo{repo}

	if err = job.fill(temp, planned); err == nil {
		t.Fatal("archive in a non-repository unexpectedly succeeded")
	}

	text := logs.String()
	if !strings.Contains(text, "done=0/1") || strings.Contains(text, "done=1/1") ||
		strings.Contains(text, "percent=100%") {
		t.Fatal(text)
	}
}
