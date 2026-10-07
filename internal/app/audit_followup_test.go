package app

import (
	"os/exec"
	"strings"
	"testing"
)

// TestAuditFoldProbeChangesOnlyCase covers a probe name whose suffix is numeric.
func TestAuditFoldProbeChangesOnlyCase(t *testing.T) {
	path := "/tmp/.release-align-case-12345678"
	got := foldProbe(path)

	if got == path || !strings.EqualFold(path, got) {
		t.Fatalf("probe must change case only: original=%q changed=%q", path, got)
	}
}

// TestAuditShellArgRoundTrip verifies a literal path survives POSIX shell parsing.
func TestAuditShellArgRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("POSIX shell unavailable")
	}

	t.Setenv("RA_AUDIT_LITERAL", "expanded")

	samples := []string{
		"dir/my file",
		"dir/o'brien",
		"dir/$RA_AUDIT_LITERAL",
		"dir/`date`",
		"dir/;id",
		"dir/a&b",
		"dir/a|b",
		"",
	}

	for _, want := range samples {
		command := exec.Command("sh", "-c", "printf '%s' "+shellArg(want))

		got, err := command.Output()
		if err != nil {
			t.Fatal(err)
		}

		if string(got) != want {
			t.Fatalf("shell hint changed the argument: want=%q got=%q", want, got)
		}
	}
}
