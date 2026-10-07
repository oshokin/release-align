package app

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"
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

	for _, want := range shellArgSamples() {
		script := "printf '%s' " + quotePOSIX(want)
		command := exec.Command("sh", "-c", script)
		assertShellRoundTrip(t, command, want)
	}
}

// TestAuditPowerShellArgRoundTrip verifies a literal path survives PowerShell parsing.
func TestAuditPowerShellArgRoundTrip(t *testing.T) {
	if runtime.GOOS != goosWindows {
		t.Skip("PowerShell quoting is the Windows hint")
	}

	if _, err := exec.LookPath("powershell"); err != nil {
		t.Skip("PowerShell unavailable")
	}

	t.Setenv("RA_AUDIT_LITERAL", "expanded")

	for _, want := range shellArgSamples() {
		script := powershellWrite(quotePowerShell(want))
		command := exec.Command("powershell", "-NoProfile", "-Command", script)
		assertShellRoundTrip(t, command, want)
	}
}

// shellArgSamples are paths that must stay literal in the suggested command.
func shellArgSamples() []string {
	return []string{
		"dir/my file",
		"dir/o'brien",
		"dir/$RA_AUDIT_LITERAL",
		"dir/`date`",
		"dir/;id",
		"dir/a&b",
		"dir/a|b",
		"",
	}
}

// powershellWrite prints one already-quoted argument without a trailing newline.
func powershellWrite(quoted string) string {
	return "$OutputEncoding = [Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false; " +
		"[Console]::Out.Write(" + quoted + ")"
}

// assertShellRoundTrip runs one shell and compares the printed argument.
func assertShellRoundTrip(t *testing.T, command *exec.Cmd, want string) {
	t.Helper()

	got, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}

	if decodeShellOutput(got) != want {
		t.Fatalf("shell hint changed the argument: want=%q got=%q", want, got)
	}
}

// decodeShellOutput accepts UTF-8 and the UTF-16 Windows PowerShell may emit.
func decodeShellOutput(raw []byte) string {
	if len(raw) >= 2 && raw[0] == 0xFF && raw[1] == 0xFE {
		raw = raw[2:]
	}

	if len(raw) < 2 || len(raw)%2 != 0 || raw[1] != 0 {
		return string(raw)
	}

	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = uint16(raw[i*2]) | uint16(raw[i*2+1])<<8
	}

	return string(utf16.Decode(units))
}
