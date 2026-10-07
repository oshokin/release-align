package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fixture is the temporary origin, seed, and working copy used by one integration test.
type fixture struct {
	// base is the workspace root that contains cloned repositories.
	base string
	// remote is the bare origin used by the test.
	remote string
	// seed is the repository that pushes into the bare origin.
	seed string
	// repo is the working copy under base.
	repo string
	// cfg is the run configuration for this fixture.
	cfg *Config
}

// git runs one Git command in a test repository and returns stdout.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir

	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, b)
	}

	return strings.TrimSpace(string(b))
}

// write creates a file in a test repository.
func write(t *testing.T, path, value string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

// setup builds a bare remote and a clone for one test.
func setup(t *testing.T) *fixture {
	t.Helper()

	root := t.TempDir()
	// Isolate tests from the developer's Git configuration and signing/hooks.
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "no-config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.invalid")

	gitEnvKeys := []string{
		"GIT_DIR",
		"GIT_WORK_TREE",
		"GIT_INDEX_FILE",
		"GIT_COMMON_DIR",
		"GIT_SSH_COMMAND",
		"GIT_SSH",
	}

	for _, key := range gitEnvKeys {
		t.Setenv(key, "")

		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}

	f := &fixture{
		base:   filepath.Join(root, "workspace"),
		remote: filepath.Join(root, "origin.git"),
		seed:   filepath.Join(root, "seed"),
	}
	f.repo = filepath.Join(f.base, "group", "repo with spaces")

	if err := os.MkdirAll(filepath.Dir(f.repo), 0o700); err != nil {
		t.Fatal(err)
	}

	git(t, root, "init", "--bare", "--initial-branch=master", f.remote)
	git(t, root, "clone", f.remote, f.seed)
	write(t, filepath.Join(f.seed, "file"), "initial\n")
	git(t, f.seed, "add", "file")
	git(t, f.seed, "commit", "-m", "initial")
	git(t, f.seed, "push", "-u", "origin", "master")
	git(t, root, "clone", f.remote, f.repo)

	f.cfg = DefaultConfig()
	f.cfg.BaseDir = f.base
	f.cfg.ProbeTimeout = time.Second
	f.cfg.RetryDelay = time.Millisecond
	f.cfg.LocalTimeout = 5 * time.Second
	f.cfg.FetchTimeout = 5 * time.Second

	return f
}

// branch creates and pushes a branch on the fixture remote.
func branch(t *testing.T, f *fixture, name string) string {
	t.Helper()
	git(t, f.seed, "switch", "-c", name)
	write(t, filepath.Join(f.seed, "release-file"), name)
	git(t, f.seed, "add", ".")
	git(t, f.seed, "commit", "-m", name)
	git(t, f.seed, "push", "-u", "origin", name)

	return git(t, f.seed, "rev-parse", "HEAD")
}

// sshScript installs a fake SSH client that records or fails fetches.
func sshScript(t *testing.T, body string) string {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("shell transport fixture")
	}

	path := filepath.Join(t.TempDir(), "ssh-helper")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GIT_SSH_COMMAND", "'"+path+"'")
	t.Setenv("GIT_SSH_VARIANT", "ssh")

	return path
}
