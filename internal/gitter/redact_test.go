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
