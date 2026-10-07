package cmd

import (
	"bytes"
	"encoding/json"
	"testing"
)

// TestAuditCloneInvalidOutputProducesNoReport covers a rejected --output value.
func TestAuditCloneInvalidOutputProducesNoReport(t *testing.T) {
	var out, diagnostic bytes.Buffer

	args := []string{"workspace", "clone", "--output", "jsno"}
	code := Execute(args, &out, &diagnostic)

	if code != exitUsage || out.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), diagnostic.String())
	}
}

// TestAuditCloneFailureJSONExplainsError rejects a success-shaped failure report.
func TestAuditCloneFailureJSONExplainsError(t *testing.T) {
	var out, diagnostic bytes.Buffer

	args := []string{"workspace", "clone", "--output", "json"}
	code := Execute(args, &out, &diagnostic)

	var report map[string]any

	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}

	if code == exitOK || report["error"] == nil || report["error"] == "" {
		t.Fatalf(
			"failed operation lacks JSON error: code=%d stdout=%q stderr=%q",
			code,
			out.String(),
			diagnostic.String(),
		)
	}
}
