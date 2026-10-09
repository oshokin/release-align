package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// TestWorkspaceInitDefaultsBranchAndFile creates an inventory without --branch or --file.
func TestWorkspaceInitDefaultsBranchAndFile(t *testing.T) {
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

	var out, errOut bytes.Buffer

	if code := Execute([]string{"workspace", "init", "--base-dir", base}, &out, &errOut); code != exitOK {
		t.Fatal(code, out.String(), errOut.String())
	}

	if strings.Contains(out.String(), "taken from the base directory name") {
		t.Fatal(out.String())
	}

	body := readTestFileString(t, filepath.Join(root, defaultWorkspaceFile))

	recorded, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(body, "revision: refs/heads/"+defaultInitBranch) ||
		!strings.Contains(body, "path: lamiona/search/calyra") ||
		!strings.Contains(body, "base-dir: "+yamlDoubleQuoted(recorded)) ||
		strings.Contains(body, "gitlab:") {
		t.Fatal(body)
	}
}

// TestWorkspaceInitRejectsExistingFileBeforeScan leaves the file unchanged and does not scan.
func TestWorkspaceInitRejectsExistingFileBeforeScan(t *testing.T) {
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
	writeTestFile(t, filepath.Join(root, defaultWorkspaceFile), "keep\n")
	t.Chdir(root)

	var out, errOut bytes.Buffer

	code := Execute([]string{"workspace", "init", "--base-dir", base}, &out, &errOut)
	exists := strings.Contains(errOut.String(), "file already exists")
	scanned := strings.Contains(out.String(), "phase=scan")

	if code == exitOK || !exists || scanned {
		t.Fatal(code, out.String(), errOut.String())
	}

	if got := readTestFileString(t, filepath.Join(root, defaultWorkspaceFile)); got != "keep\n" {
		t.Fatal(got)
	}
}

// TestWorkspaceInitGitLabURLFromBaseDirectory writes the host and says where it came from.
func TestWorkspaceInitGitLabURLFromBaseDirectory(t *testing.T) {
	root := t.TempDir()
	isolateGit(t, root)
	base := filepath.Join(root, "Git.Example.COM")
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

	var out, errOut bytes.Buffer

	args := []string{"workspace", "init", "--base-dir", base, "--file", defaultWorkspaceFile}
	if code := Execute(args, &out, &errOut); code != exitOK {
		t.Fatal(code, out.String(), errOut.String())
	}

	if !strings.Contains(out.String(), "GitLab URL https://git.example.com taken from the base directory name.") {
		t.Fatal(out.String())
	}

	body := readTestFileString(t, filepath.Join(root, defaultWorkspaceFile))
	if !strings.Contains(body, "url: https://git.example.com") || !strings.Contains(body, remote) {
		t.Fatal(body)
	}

	out.Reset()
	errOut.Reset()

	explicit := []string{
		"workspace", "init",
		"--base-dir", base,
		"--file", "explicit.yml",
		"--gitlab-url", "https://Saved.Example",
		"--gitlab-group", "lamiona",
	}
	if code := Execute(explicit, &out, &errOut); code != exitOK {
		t.Fatal(code, out.String(), errOut.String())
	}

	if strings.Contains(out.String(), "taken from the base directory name") {
		t.Fatal(out.String())
	}

	saved := readTestFileString(t, filepath.Join(root, "explicit.yml"))
	if !strings.Contains(saved, "url: https://Saved.Example") ||
		strings.Contains(saved, "url: https://git.example.com") {
		t.Fatal(saved)
	}

	gitCmd(t, checkout, "remote", "set-url", "origin", "git@git.example.com:lamiona/search/calyra.git")
	out.Reset()
	errOut.Reset()

	guessed := []string{"workspace", "init", "--base-dir", base, "--file", "guessed.yml"}
	if code := Execute(guessed, &out, &errOut); code != exitOK {
		t.Fatal(code, out.String(), errOut.String())
	}

	if !strings.Contains(out.String(), "GitLab groups taken from clone URLs on https://git.example.com: lamiona.") {
		t.Fatal(out.String())
	}

	guessedBody := readTestFileString(t, filepath.Join(root, "guessed.yml"))
	if !strings.Contains(guessedBody, "groups: [lamiona]") {
		t.Fatal(guessedBody)
	}
}
