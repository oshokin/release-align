package gitter

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap/zapcore"

	"github.com/oshokin/release-align/internal/logger"
)

// TestDebugGitLogRedactsArguments keeps credentials out of the new command-start diagnostics.
func TestDebugGitLogRedactsArguments(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	ctx := logger.ToContext(t.Context(), logger.NewWithWriter(zapcore.DebugLevel, &logs))
	client := &Client{LocalTimeout: time.Second}
	// An unknown subcommand rejects the argument locally: this test makes no network request.
	_, err := client.Local(
		ctx,
		t.TempDir(),
		"release-align-invalid",
		"https://user:FAKE_PASSWORD@example.invalid/x?private_token=FAKE_TOKEN",
	)
	if err == nil {
		t.Fatal("expected local Git failure")
	}

	text := logs.String()
	if !strings.Contains(text, "Running Git") || !strings.Contains(text, "Git finished") ||
		strings.Contains(text, "FAKE_PASSWORD") || strings.Contains(text, "FAKE_TOKEN") {
		t.Fatal(text)
	}
}
