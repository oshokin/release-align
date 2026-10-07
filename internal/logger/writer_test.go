package logger

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap/zapcore"
)

// TestWriterSerializesConcurrentRepositoryLogs verifies that concurrent repository logs stay intact.
func TestWriterSerializesConcurrentRepositoryLogs(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	ctx := ToContext(t.Context(), NewWithWriter(zapcore.InfoLevel, &out))

	var wg sync.WaitGroup

	for i := range 32 {
		wg.Go(func() {
			InfoKV(ctx, fmt.Sprintf("message-%02d", i), "repo", fmt.Sprintf("group/repo-%02d", i))
		})
	}

	wg.Wait()

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 32 {
		t.Fatalf("expected 32 complete lines, got %d", len(lines))
	}

	for _, line := range lines {
		short := len(line) < len(timeLayout) || line[10] != ' '
		marked := strings.Contains(line, "Z") || strings.Contains(line, "{")

		if short || marked {
			t.Fatal(line)
		}

		hasLevel := strings.Contains(line, " INFO ")
		hasRepo := strings.Contains(line, "group/repo-")
		hasMessage := strings.Contains(line, "message-")

		if !hasLevel || !hasRepo || !hasMessage {
			t.Fatal(line)
		}
	}
}
