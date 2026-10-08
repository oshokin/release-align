package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStatusRejectsEmptyBaseDirWhenTheFileOmitsIt refuses the old built-in checkout path.
func TestStatusRejectsEmptyBaseDirWhenTheFileOmitsIt(t *testing.T) {
	t.Setenv("RELEASE_ALIGN_BASE_DIR", "")

	root := t.TempDir()
	file := filepath.Join(root, defaultWorkspaceFile)
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
	writeTestFile(t, file, body)

	var out, errOut bytes.Buffer

	code := Execute([]string{"workspace", "status", "--workspace", file}, &out, &errOut)
	if code != exitUsage || !strings.Contains(errOut.String(), "BASE_DIR") {
		t.Fatalf("%d %s %s", code, out.String(), errOut.String())
	}
}

// TestCommandsUseSavedBaseDirAndDefaultWorkspace covers every command that reads a workspace.
func TestCommandsUseSavedBaseDirAndDefaultWorkspace(t *testing.T) {
	t.Setenv("RELEASE_ALIGN_BASE_DIR", "")

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

	if code := runCLI(t, "workspace", "init", "--base-dir", base); code != exitOK {
		t.Fatal(code)
	}

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}

	if code := Execute([]string{"workspace", "refresh"}, out, errOut); code != exitOK ||
		!strings.Contains(out.String(), "No changes written") {
		t.Fatalf("refresh %d\n%s\n%s", code, out.String(), errOut.String())
	}

	out.Reset()
	errOut.Reset()

	if code := Execute([]string{"workspace", "status"}, out, errOut); code != exitOK {
		t.Fatalf("status %d\n%s\n%s", code, out.String(), errOut.String())
	}

	out.Reset()
	errOut.Reset()

	if code := Execute([]string{"workspace", "sync", "--dry-run"}, out, errOut); code != exitOK {
		t.Fatalf("sync %d\n%s\n%s", code, out.String(), errOut.String())
	}

	out.Reset()
	errOut.Reset()

	zip := filepath.Join(root, "sources.zip")
	if code := Execute([]string{"workspace", "archive", "--file", zip}, out, errOut); code != exitOK {
		t.Fatalf("archive %d\n%s\n%s", code, out.String(), errOut.String())
	}

	if _, err := os.Stat(zip); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	errOut.Reset()

	code := Execute([]string{"workspace", "clone", "--repo", "lamiona/search/calyra"}, out, errOut)
	if code != exitUsage || !strings.Contains(errOut.String(), "gitlab source") {
		t.Fatalf("clone %d\n%s\n%s", code, out.String(), errOut.String())
	}
}
