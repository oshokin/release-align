package cmd

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// failedReportWriter simulates a closed result pipe.
type failedReportWriter struct{}

// Write rejects a result while diagnostics remain available on stderr.
func (*failedReportWriter) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}

// TestJSONWriteFailurePreservesTheOperationError keeps both causes for the CLI boundary.
func TestJSONWriteFailurePreservesTheOperationError(t *testing.T) {
	t.Parallel()

	for _, operation := range []string{"clone", "archive", "sync", "status"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()

			out := new(failedReportWriter)
			command := NewRootCommand(out, io.Discard)
			args := []string{
				"workspace", operation, "--output", "json",
				"--workspace", filepath.Join(t.TempDir(), "missing.yml"),
			}
			command.SetArgs(args)
			err := command.ExecuteContext(t.Context())

			if !errors.Is(err, io.ErrClosedPipe) || !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("lost a failure cause: %v", err)
			}
		})
	}
}
