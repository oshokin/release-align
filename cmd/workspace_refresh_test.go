package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestWorkspaceRefreshPreviewAddAndGroupSync covers init, a new clone, preview, add, and a group sync.
func TestWorkspaceRefreshPreviewAddAndGroupSync(t *testing.T) {
	root := t.TempDir()
	isolateGit(t, root)
	base := filepath.Join(root, "src")
	remote := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	first := filepath.Join(base, "lamiona", "search", "calyra")
	second := filepath.Join(base, "lamiona", "search", "new-indexer")
	file := filepath.Join(root, "release-align.yml")

	gitCmd(t, root, "init", "--bare", "--initial-branch=master", remote)
	gitCmd(t, root, "clone", remote, seed)
	writeTestFile(t, filepath.Join(seed, "file"), "initial\n")
	gitCmd(t, seed, "add", "file")
	gitCmd(t, seed, "commit", "-m", "initial")
	gitCmd(t, seed, "push", "-u", "origin", "master")
	gitCmd(t, root, "clone", remote, first)

	if code := runCLI(
		t,
		"workspace",
		"init",
		"--base-dir",
		base,
		"--branch",
		"master",
		"--file",
		file,
	); code != exitOK {
		t.Fatal(code)
	}

	created := readTestFile(t, file)
	gitCmd(t, root, "clone", remote, second)

	var out, errOut bytes.Buffer

	args := []string{"workspace", "refresh", "--base-dir", base, "--file", file}

	if code := Execute(args, &out, &errOut); code != exitOK ||
		!strings.Contains(out.String(), "+ lamiona/search/new-indexer") ||
		!strings.Contains(out.String(), "groups: lamiona, lamiona/search") ||
		!strings.Contains(out.String(), "No changes written") ||
		!strings.Contains(out.String(), "Readiness was not checked") ||
		strings.Contains(out.String(), "ready=true") ||
		!bytes.Equal(readTestFile(t, file), created) {
		t.Fatalf("preview %d\n%s\n%s", code, out.String(), errOut.String())
	}

	out.Reset()
	errOut.Reset()

	statusArgs := []string{"workspace", "status", "--base-dir", base, "--workspace", file}
	if code := Execute(statusArgs, &out, &errOut); code != exitOK || strings.Contains(out.String(), "new-indexer") {
		t.Fatalf("status %d\n%s\n%s", code, out.String(), errOut.String())
	}

	out.Reset()
	errOut.Reset()

	addArgs := []string{
		"workspace", "refresh", "--base-dir", base, "--file", file, "--add", "lamiona/search/new-indexer",
	}
	if code := Execute(addArgs, &out, &errOut); code != exitOK ||
		!strings.Contains(out.String(), "Added: 1") ||
		!strings.Contains(readTestFileString(t, file), "lamiona/search/new-indexer") {
		t.Fatalf("add %d\n%s\n%s", code, out.String(), errOut.String())
	}

	out.Reset()
	errOut.Reset()

	syncArgs := []string{
		"workspace", "sync",
		"--base-dir", base,
		"--workspace", file,
		"--group", "lamiona/search",
		"--attempts", "1",
		"--jobs", "1",
	}
	if code := Execute(syncArgs, &out, &errOut); code != exitOK {
		t.Fatalf("sync %d\n%s\n%s", code, out.String(), errOut.String())
	}
}

// TestWorkspaceRefreshUsage covers missing flags, combined add modes, and a comma inside one path.
func TestWorkspaceRefreshUsage(t *testing.T) {
	rejected := [][]string{
		{"workspace", "refresh", "--base-dir", "x", "--file", "y", "--add", "a", "--add-all"},
		{"workspace", "refresh", "--base-dir", "x", "--file", "y", "extra"},
	}

	for _, args := range rejected {
		var out, errOut bytes.Buffer

		if code := Execute(args, &out, &errOut); code != exitUsage {
			t.Fatalf("%v: %d %s", args, code, errOut.String())
		}
	}

	root := t.TempDir()
	isolateGit(t, root)
	base := filepath.Join(root, "src")
	remote := filepath.Join(root, "origin.git")
	repo := filepath.Join(base, "lamiona", "search", "calyra")
	file := filepath.Join(root, "release-align.yml")
	gitCmd(t, root, "init", "--bare", "--initial-branch=master", remote)
	gitCmd(t, root, "clone", remote, repo)
	writeTestFile(t, filepath.Join(repo, "file"), "initial\n")
	gitCmd(t, repo, "add", "file")
	gitCmd(t, repo, "commit", "-m", "initial")

	if code := runCLI(
		t,
		"workspace",
		"init",
		"--base-dir",
		base,
		"--branch",
		"master",
		"--file",
		file,
	); code != exitOK {
		t.Fatal(code)
	}

	var out, errOut bytes.Buffer

	args := []string{"workspace", "refresh", "--base-dir", base, "--file", file, "--add", "lamiona/search/new,indexer"}
	if code := Execute(args, &out, &errOut); code != exitUsage || !strings.Contains(errOut.String(), "new,indexer") {
		t.Fatalf("%d %s %s", code, out.String(), errOut.String())
	}
}

// runCLI executes one command and fails the test when stderr is unexpected only at the call site.
func runCLI(t *testing.T, args ...string) int {
	t.Helper()

	var out, errOut bytes.Buffer

	code := Execute(args, &out, &errOut)
	if code != exitOK {
		t.Log(out.String())
		t.Log(errOut.String())
	}

	return code
}

// isolateGit clears repository-selection variables for one CLI test.
func isolateGit(t *testing.T, root string) {
	t.Helper()

	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "no-config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.invalid")

	keys := []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_SSH_COMMAND", "GIT_SSH"}
	for _, key := range keys {
		t.Setenv(key, "")

		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
}

// gitCmd runs Git and fails the test on a nonzero status.
func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

// writeTestFile creates one fixture file.
func writeTestFile(t *testing.T, path, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// readTestFile returns the workspace bytes.
func readTestFile(t *testing.T, path string) []byte {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return body
}

// readTestFileString returns the workspace text.
func readTestFileString(t *testing.T, path string) string {
	t.Helper()

	return string(readTestFile(t, path))
}
