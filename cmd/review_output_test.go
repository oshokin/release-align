package cmd

import (
	"bytes"
	"testing"
)

// TestReviewInvalidOutputNeverStartsRun rejects misspelled formats even without a workspace.
func TestReviewInvalidOutputNeverStartsRun(t *testing.T) {
	var out, err bytes.Buffer

	args := []string{"--output", "jsno", "--dry-run", "--base-dir", t.TempDir()}

	if code := Execute(args, &out, &err); code != exitUsage || out.Len() != 0 {
		t.Fatalf("code=%d out=%s err=%s", code, out.String(), err.String())
	}
}
