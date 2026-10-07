package gitter

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// statusTextError is a test error whose text is classified as network or local.
type statusTextError string

// Error returns the stored text.
func (e statusTextError) Error() string {
	return string(e)
}

// TestNetworkClassification verifies which Git errors count as a network failure.
func TestNetworkClassification(t *testing.T) {
	networkErrors := []string{
		"Could not resolve hostname",
		"Failed to connect",
		"The requested URL returned error: 503",
		"Connection refused",
		"unexpected disconnect",
	}

	for _, s := range networkErrors {
		if !NetworkError(statusTextError(s)) {
			t.Fatal(s)
		}
	}

	localErrors := []string{
		"Permission denied (publickey)",
		"repository not found",
		"Authentication failed",
		"SSL certificate problem",
		"would clobber existing tag",
	}

	for _, s := range localErrors {
		if NetworkError(statusTextError(s)) {
			t.Fatal(s)
		}
	}

	if !NetworkError(context.DeadlineExceeded) || NetworkError(context.Canceled) {
		t.Fatal("context classification")
	}
}

// TestCommandTimeoutAndNoPrompt verifies command timeouts and that Git is not allowed to ask for a password.
func TestCommandTimeoutAndNoPrompt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix process tree fixture")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "git")
	body := "#!/bin/sh\nif [ \"$GIT_TERMINAL_PROMPT\" != 0 ]; then exit 17; fi\nsleep 30 &\nwait\n"

	if e := os.WriteFile(script, []byte(body), 0o700); e != nil {
		t.Fatal(e)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	start := time.Now()
	client := &Client{
		LocalTimeout: 100 * time.Millisecond,
	}
	_, e := client.Local(context.Background(), dir, "status")

	if !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}

	if time.Since(start) > 2*time.Second {
		t.Fatal("subprocess timeout did not bound wait")
	}
}

// TestNoShellInterpolation verifies that Git arguments are not passed through a shell.
func TestNoShellInterpolation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix echo fixture")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "git")

	if e := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0o700); e != nil {
		t.Fatal(e)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	arg := "$(touch PWNED); echo bad"
	client := &Client{
		LocalTimeout: time.Second,
	}
	out, e := client.Local(context.Background(), dir, arg)

	if e != nil || !strings.Contains(out, arg) {
		t.Fatal(out, e)
	}

	if _, statErr := os.Stat(filepath.Join(dir, "PWNED")); !os.IsNotExist(statErr) {
		t.Fatal("shell interpolation")
	}
}

// TestProbeNoLazyFetchLeavesUnknownOptionUnused checks that a rejected flag is not a fatal probe.
func TestProbeNoLazyFetchLeavesUnknownOptionUnused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell git fixture")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "git")
	body := "#!/bin/sh\nfor arg in \"$@\"; do\n" +
		"  if [ \"$arg\" = \"--no-lazy-fetch\" ]; then echo 'unknown option' >&2; exit 129; fi\ndone\n" +
		"echo unexpected >&2\nexit 99\n"

	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	client := &Client{
		LocalTimeout: time.Second,
	}
	if err := client.ProbeNoLazyFetch(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestProbeNoLazyFetchKeepsLaunchFailures checks a missing executable and a command timeout.
func TestProbeNoLazyFetchKeepsLaunchFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell git fixture")
	}

	missing := t.TempDir()
	originalPath := os.Getenv("PATH")

	t.Setenv("PATH", missing)

	client := &Client{
		LocalTimeout: time.Second,
	}
	err := client.ProbeNoLazyFetch(context.Background())

	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatal(err)
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "git")

	if err = os.WriteFile(script, []byte("#!/bin/sh\nsleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+originalPath)

	client.LocalTimeout = 100 * time.Millisecond
	err = client.ProbeNoLazyFetch(context.Background())

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
