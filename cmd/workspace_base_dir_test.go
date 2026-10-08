package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestSavedBaseDirPriority checks flag, then BASE_DIR, then the workspace file.
func TestSavedBaseDirPriority(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, defaultWorkspaceFile)
	writeWorkspaceBase(t, file, "/from-file")
	t.Setenv("RELEASE_ALIGN_BASE_DIR", "")

	command, current := parseBaseDir(t, "--base-dir", "/from-flag")
	t.Setenv("RELEASE_ALIGN_BASE_DIR", "/from-env")
	choice := &baseDirChoice{
		command:   command,
		workspace: file,
		current:   current,
	}
	got, err := savedBaseDir(choice)

	if err != nil || got != "/from-flag" {
		t.Fatal(got, err)
	}

	command, current = parseBaseDir(t)
	choice = &baseDirChoice{
		command:   command,
		workspace: file,
		current:   current,
	}
	got, err = savedBaseDir(choice)

	if err != nil || got != "/from-env" {
		t.Fatal(got, err)
	}

	t.Setenv("RELEASE_ALIGN_BASE_DIR", "")
	t.Setenv("BASE_DIR", "/from-global")
	command, current = parseBaseDir(t)
	choice = &baseDirChoice{
		command:   command,
		workspace: file,
		current:   current,
	}
	got, err = savedBaseDir(choice)

	if err != nil || got != "/from-file" {
		t.Fatal(got, err)
	}

	command, current = parseBaseDir(t)
	choice = &baseDirChoice{
		command:     command,
		current:     current,
		recorded:    "/from-spec",
		recordedSet: true,
	}
	got, err = savedBaseDir(choice)

	if err != nil || got != "/from-spec" {
		t.Fatal(got, err)
	}

	choice.recorded = ""
	if _, err = savedBaseDir(choice); err == nil || !strings.Contains(err.Error(), "BASE_DIR") {
		t.Fatal(err)
	}

	command, current = parseBaseDir(t)
	choice = &baseDirChoice{
		command: command,
		current: current,
	}

	if _, err = savedBaseDir(choice); err == nil || !strings.Contains(err.Error(), "BASE_DIR") {
		t.Fatal(err)
	}

	command, current = parseBaseDir(t, "--base-dir", "")
	t.Setenv("RELEASE_ALIGN_BASE_DIR", "/from-env")
	choice = &baseDirChoice{
		command:   command,
		workspace: file,
		current:   current,
	}
	if _, err = savedBaseDir(choice); err == nil {
		t.Fatal("empty flag fell through")
	}
}

// TestInitBaseDirUsesFlagThenEnvironment stores the first source that is set.
func TestInitBaseDirUsesFlagThenEnvironment(t *testing.T) {
	root, base := checkoutCalyra(t)
	other := t.TempDir()
	t.Setenv("RELEASE_ALIGN_BASE_DIR", other)

	var out, errOut bytes.Buffer

	args := []string{"workspace", "init", "--base-dir", base, "--file", "from-flag.yml"}
	if code := Execute(args, &out, &errOut); code != exitOK {
		t.Fatal(code, out.String(), errOut.String())
	}

	recorded := canonicalTestPath(t, base)
	body := readTestFileString(t, filepath.Join(root, "from-flag.yml"))

	if !strings.Contains(body, recorded) || strings.Contains(body, other) {
		t.Fatal(body)
	}

	t.Setenv("RELEASE_ALIGN_BASE_DIR", base)
	out.Reset()
	errOut.Reset()

	if code := Execute([]string{"workspace", "init", "--file", "from-env.yml"}, &out, &errOut); code != exitOK {
		t.Fatal(code, out.String(), errOut.String())
	}

	body = readTestFileString(t, filepath.Join(root, "from-env.yml"))
	if !strings.Contains(body, recorded) {
		t.Fatal(body)
	}
}

// TestCommandsRejectMissingBaseDir exits 2 for every command when no source is set.
func TestCommandsRejectMissingBaseDir(t *testing.T) {
	t.Setenv("RELEASE_ALIGN_BASE_DIR", "")

	root := t.TempDir()
	file := filepath.Join(root, defaultWorkspaceFile)
	writeWorkspaceBase(t, file, "")
	zip := filepath.Join(root, "sources.zip")
	commands := [][]string{
		{"workspace", "init"},
		{"status", "--workspace", file},
		{"--workspace", file},
		{"workspace", "refresh", "--file", file},
		{"workspace", "clone", "--workspace", file, "--repo", "lamiona/search/calyra"},
		{"workspace", "archive", "--workspace", file, "--file", zip},
	}

	for _, args := range commands {
		var out, errOut bytes.Buffer

		code := Execute(args, &out, &errOut)
		if code != exitUsage || !strings.Contains(errOut.String(), "BASE_DIR") {
			t.Fatalf("%v: %d\n%s\n%s", args, code, out.String(), errOut.String())
		}
	}
}

// TestBaseDirEnvironmentBeatsFile uses BASE_DIR when the flag is omitted.
func TestBaseDirEnvironmentBeatsFile(t *testing.T) {
	root, base := checkoutCalyra(t)
	file := filepath.Join(root, defaultWorkspaceFile)
	writeWorkspaceBase(t, file, base)
	empty := t.TempDir()
	t.Setenv("RELEASE_ALIGN_BASE_DIR", empty)
	zip := filepath.Join(root, "sources.zip")

	var out, errOut bytes.Buffer

	status := []string{"status", "--workspace", file}
	if code := Execute(status, &out, &errOut); code == exitOK {
		t.Fatal("status used the file instead of BASE_DIR")
	}

	out.Reset()
	errOut.Reset()

	sync := []string{"--dry-run", "--workspace", file}
	if code := Execute(sync, &out, &errOut); code == exitOK {
		t.Fatal("sync used the file instead of BASE_DIR")
	}

	out.Reset()
	errOut.Reset()

	refresh := []string{"workspace", "refresh", "--file", file}
	if code := Execute(refresh, &out, &errOut); code != exitOK || !strings.Contains(out.String(), "Missing listed") {
		t.Fatalf("refresh %d\n%s\n%s", code, out.String(), errOut.String())
	}

	out.Reset()
	errOut.Reset()

	archive := []string{"workspace", "archive", "--workspace", file, "--file", zip}
	if code := Execute(archive, &out, &errOut); code == exitOK {
		t.Fatal("archive used the file instead of BASE_DIR")
	}

	out.Reset()
	errOut.Reset()

	bare := filepath.Join(root, "no-base.yml")
	writeWorkspaceBase(t, bare, "")
	clone := []string{"workspace", "clone", "--workspace", bare, "--repo", "lamiona/search/calyra"}
	code := Execute(clone, &out, &errOut)

	if code != exitUsage || !strings.Contains(errOut.String(), "gitlab source") {
		t.Fatalf("clone %d\n%s\n%s", code, out.String(), errOut.String())
	}
}

// TestBaseDirFlagBeatsEnvironment uses the flag when BASE_DIR points somewhere else.
func TestBaseDirFlagBeatsEnvironment(t *testing.T) {
	root, base := checkoutCalyra(t)
	file := filepath.Join(root, defaultWorkspaceFile)
	writeWorkspaceBase(t, file, t.TempDir())
	t.Setenv("RELEASE_ALIGN_BASE_DIR", t.TempDir())
	zip := filepath.Join(root, "sources.zip")
	commands := [][]string{
		{"status", "--workspace", file, "--base-dir", base},
		{"--dry-run", "--workspace", file, "--base-dir", base},
		{"workspace", "refresh", "--file", file, "--base-dir", base},
		{"workspace", "archive", "--workspace", file, "--base-dir", base, "--file", zip},
	}

	for _, args := range commands {
		var out, errOut bytes.Buffer

		if code := Execute(args, &out, &errOut); code != exitOK {
			t.Fatalf("%v: %d\n%s\n%s", args, code, out.String(), errOut.String())
		}
	}
}

// parseBaseDir parses one command that only has --base-dir.
func parseBaseDir(t *testing.T, args ...string) (*cobra.Command, string) {
	t.Helper()

	var current string

	command := &cobra.Command{
		Use:  "x",
		RunE: func(*cobra.Command, []string) error { return nil },
	}
	command.Flags().StringVar(&current, "base-dir", "", "")
	command.SetArgs(args)
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})

	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}

	return command, current
}

// writeWorkspaceBase writes a one-project workspace. An empty base omits release-align.base-dir.
func writeWorkspaceBase(t *testing.T, path, base string) {
	t.Helper()

	body := "" +
		"manifest:\n" +
		"  version: \"1.0\"\n" +
		"  defaults:\n" +
		"    revision: refs/heads/master\n" +
		"  projects:\n" +
		"    - name: calyra\n" +
		"      path: lamiona/search/calyra\n" +
		"release-align:\n" +
		"  schema-version: 1\n"
	if base != "" {
		body += "  base-dir: \"" + base + "\"\n"
	}

	writeTestFile(t, path, body)
}

// checkoutCalyra creates one local clone and chdirs to its parent.
func checkoutCalyra(t *testing.T) (string, string) {
	t.Helper()

	root := t.TempDir()
	isolateGit(t, root)
	base := filepath.Join(root, "src")
	remote := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	checkout := filepath.Join(base, "lamiona", "search", "calyra")

	gitCmd(t, root, "init", "--bare", "--initial-branch=master", remote)
	gitCmd(t, root, "clone", remote, seed)
	writeTestFile(t, filepath.Join(seed, "file"), "initial\n")
	gitCmd(t, seed, "add", "file")
	gitCmd(t, seed, "commit", "-m", "initial")
	gitCmd(t, seed, "push", "-u", "origin", "master")
	gitCmd(t, root, "clone", remote, checkout)
	t.Chdir(root)

	return root, base
}

// canonicalTestPath is the absolute path init stores for a base directory.
func canonicalTestPath(t *testing.T, path string) string {
	t.Helper()

	recorded, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}

	return recorded
}
