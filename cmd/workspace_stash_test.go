package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// TestWorkspaceStashPrintsTheOIDAndSkipsASecondPush covers a dirty tree and a repeat run.
func TestWorkspaceStashPrintsTheOIDAndSkipsASecondPush(t *testing.T) {
	root := t.TempDir()
	isolateGit(t, root)
	base := filepath.Join(root, "src")
	remote := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	repo := filepath.Join(base, "lamiona", "search", "calyra")
	file := filepath.Join(root, "release-align.yml")

	gitCmd(t, root, "init", "--bare", "--initial-branch=master", remote)
	gitCmd(t, root, "clone", remote, seed)
	writeTestFile(t, filepath.Join(seed, "file"), "initial\n")
	gitCmd(t, seed, "add", "file")
	gitCmd(t, seed, "commit", "-m", "initial")
	gitCmd(t, seed, "push", "-u", "origin", "master")
	gitCmd(t, root, "clone", remote, repo)

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

	writeTestFile(t, filepath.Join(repo, "file"), "dirty\n")
	writeTestFile(t, filepath.Join(repo, "extra"), "untracked\n")

	var out, errOut bytes.Buffer

	args := []string{"workspace", "stash", "--base-dir", base, "--file", file}
	if code := Execute(args, &out, &errOut); code != exitOK ||
		!strings.Contains(out.String(), " WARN ") ||
		!strings.Contains(out.String(), "phase=stash") ||
		!strings.Contains(out.String(), "stashed ") ||
		!strings.Contains(out.String(), "lamiona/search/calyra") {
		t.Fatalf("stash %d\n%s\n%s", code, out.String(), errOut.String())
	}

	out.Reset()
	errOut.Reset()
	writeTestFile(t, filepath.Join(repo, "file"), "newer\n")

	if code := Execute(args, &out, &errOut); code != exitOK ||
		!strings.Contains(out.String(), "already stashed ") ||
		!strings.Contains(out.String(), "lamiona/search/calyra") ||
		readTestFileString(t, filepath.Join(repo, "file")) != "newer\n" {
		t.Fatalf("repeat %d\n%s\n%s", code, out.String(), errOut.String())
	}
}

// TestLogLevelIsARootFlagAndTheWorkspaceCanSetIt covers inheritance, a bad name, and the file.
func TestLogLevelIsARootFlagAndTheWorkspaceCanSetIt(t *testing.T) {
	var out, errOut bytes.Buffer

	if code := Execute([]string{"workspace", "stash", "--help"}, &out, &errOut); code != exitOK ||
		!strings.Contains(out.String(), "--log-level") {
		t.Fatalf("help %d\n%s", code, out.String())
	}

	root := t.TempDir()
	isolateGit(t, root)
	base := filepath.Join(root, "src")
	remote := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	repo := filepath.Join(base, "lamiona", "search", "calyra")
	file := filepath.Join(root, "release-align.yml")

	gitCmd(t, root, "init", "--bare", "--initial-branch=master", remote)
	gitCmd(t, root, "clone", remote, seed)
	writeTestFile(t, filepath.Join(seed, "file"), "initial\n")
	gitCmd(t, seed, "add", "file")
	gitCmd(t, seed, "commit", "-m", "initial")
	gitCmd(t, seed, "push", "-u", "origin", "master")
	gitCmd(t, root, "clone", remote, repo)

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
	); code != exitOK ||
		!strings.Contains(readTestFileString(t, file), "log-level: info") {
		t.Fatal(code)
	}

	written := strings.Replace(readTestFileString(t, file), "log-level: info", "log-level: warn", 1)
	writeTestFile(t, file, written)
	out.Reset()
	errOut.Reset()

	args := []string{"workspace", "stash", "--base-dir", base, "--file", file}
	if code := Execute(args, &out, &errOut); code != exitOK || strings.Contains(out.String(), " INFO ") {
		t.Fatalf("file %d\n%s\n%s", code, out.String(), errOut.String())
	}

	out.Reset()
	errOut.Reset()
	args = []string{"--log-level", "info", "workspace", "stash", "--base-dir", base, "--file", file}

	if code := Execute(args, &out, &errOut); code != exitOK || !strings.Contains(out.String(), " INFO ") ||
		!strings.Contains(out.String(), "clean") {
		t.Fatalf("flag %d\n%s\n%s", code, out.String(), errOut.String())
	}

	out.Reset()
	errOut.Reset()

	if code := Execute(
		[]string{"workspace", "stash", "--log-level", "nope", "--base-dir", base, "--file", file},
		&out,
		&errOut,
	); code != exitUsage ||
		!strings.Contains(errOut.String(), "nope") {
		t.Fatalf("bad %d\n%s", code, errOut.String())
	}
}

// TestWorkspaceStashHelpIsACommand checks that stash is registered and does not pop.
func TestWorkspaceStashHelpIsACommand(t *testing.T) {
	var out, errOut bytes.Buffer

	if code := Execute([]string{"workspace", "stash", "--help"}, &out, &errOut); code != exitOK ||
		!strings.Contains(out.String(), "does not pop") {
		t.Fatalf("%d\n%s\n%s", code, out.String(), errOut.String())
	}
}
