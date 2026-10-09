package logger

import (
	"bytes"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"go.uber.org/zap/zapcore"
)

// TestProgressEstimatesTheRemainder checks percent and the average-based remainder.
func TestProgressEstimatesTheRemainder(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	synctest.Test(t, func(t *testing.T) {
		ctx := ToContext(t.Context(), NewWithWriter(zapcore.InfoLevel, &buf))
		progress := NewProgress("fetch", 4)

		time.Sleep(4 * time.Second)
		progress.Advance(ctx, "group/name", "fetched")
	})

	plain := buf.String()
	for _, part := range []string{
		"group/name",
		"fetched",
		"phase=fetch",
		"done=1/4",
		"percent=25%",
		"elapsed=4s",
		"left=12s",
	} {
		if !strings.Contains(plain, part) {
			t.Fatal(plain)
		}
	}

	percent, left := PaceText(0, 4, time.Second)
	if percent != "0%" || left != "unknown" {
		t.Fatal(percent, left)
	}

	percent, left = PaceText(4, 4, time.Second)
	if percent != "100%" || left != "0s" {
		t.Fatal(percent, left)
	}
}

// TestProgressWithoutATotalCountsFinds checks a scan that does not know its size.
func TestProgressWithoutATotalCountsFinds(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	ctx := ToContext(t.Context(), NewWithWriter(zapcore.InfoLevel, &buf))
	progress := NewProgress("scan", 0)
	progress.Advance(ctx, "group/name", "found")
	progress.Finish(ctx, "scan finished")

	plain := buf.String()
	if !strings.Contains(plain, "found=1") || strings.Contains(plain, "percent=") {
		t.Fatal(plain)
	}

	if strings.Count(plain, "scan finished") != 1 || strings.Contains(plain, "group/name  scan finished") {
		t.Fatal(plain)
	}
}
