package gitter

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// TestCommandErrorRedactsDiagnosticCredentials covers every caller that formats the Git error.
func TestCommandErrorRedactsDiagnosticCredentials(t *testing.T) {
	cause := os.ErrPermission
	failure := &CommandError{
		Args:   []string{"ls-remote", "origin"},
		Err:    cause,
		Output: "Could not resolve https://fakeuser:FAKE_SECRET@example.invalid/repo?private_token=FAKE_TOKEN",
	}
	output := failure.Error()
	secrets := []string{"FAKE_SECRET", "FAKE_TOKEN", "fakeuser"}

	for _, secret := range secrets {
		if strings.Contains(output, secret) {
			t.Fatal(output)
		}
	}

	if !errors.Is(failure, cause) || !NetworkError(failure) {
		t.Fatal("classification lost")
	}
}

// TestCommandErrorJoinsStderrLines keeps a later Git line on the same diagnostic.
func TestCommandErrorJoinsStderrLines(t *testing.T) {
	t.Parallel()

	failure := &CommandError{
		Args: []string{"fetch", "origin"},
		Err:  os.ErrPermission,
		Output: "error:\n" +
			"fatal: could not read from remote repository\n",
	}
	output := failure.Error()

	if strings.Contains(output, "\n") {
		t.Fatal(output)
	}

	if !strings.Contains(output, "error:; fatal: could not read from remote repository") {
		t.Fatal(output)
	}
}
