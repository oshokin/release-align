package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap/zapcore"

	"github.com/oshokin/release-align/internal/gitter"
	"github.com/oshokin/release-align/internal/logger"
)

// TestStashSavesDirtyAndUntrackedWork checks a dirty file, an untracked file, and a clean neighbor.
func TestStashSavesDirtyAndUntrackedWork(t *testing.T) {
	f := setup(t)
	clean := cloneRel(t, f, "group/clean")
	write(t, filepath.Join(f.repo, "file"), "dirty\n")
	write(t, filepath.Join(f.repo, "new-file"), "untracked\n")
	file := stashFile(t, "group/repo with spaces", clean)

	options := &WorkspaceStashOptions{BaseDir: f.base}

	result, err := StashWorkspace(t.Context(), stashGit(f), file, options)
	if err != nil || result.Clean != 1 || len(result.Stashed) != 1 || len(result.Kept) != 0 {
		t.Fatal(result, err)
	}

	if result.Stashed[0].Path != "group/repo with spaces" || result.Stashed[0].OID == "" {
		t.Fatal(result.Stashed[0])
	}

	if result.Stashed[0].OID != git(t, f.repo, "rev-parse", "stash") {
		t.Fatal(result.Stashed[0].OID)
	}

	if git(t, f.repo, "status", "--porcelain") != "" || git(t, f.repo, "stash", "list") == "" {
		t.Fatal("dirty tree was left in place")
	}
}

// TestStashLogsEveryRepository shows a line for a clean neighbor and a finished count.
func TestStashLogsEveryRepository(t *testing.T) {
	f := setup(t)
	clean := cloneRel(t, f, "group/clean")
	write(t, filepath.Join(f.repo, "file"), "dirty\n")
	file := stashFile(t, "group/repo with spaces", clean)

	var out bytes.Buffer

	ctx := logger.ToContext(t.Context(), logger.NewWithWriter(zapcore.InfoLevel, &out))

	options := &WorkspaceStashOptions{BaseDir: f.base}

	_, err := StashWorkspace(ctx, stashGit(f), file, options)
	if err != nil {
		t.Fatal(err)
	}

	text := out.String()
	loggedClean := stashLineLevel(text, "group/clean", "clean, phase=stash") == "INFO"
	loggedDirty := stashLineLevel(text, "group/repo with spaces", "stashed ") == "WARN"
	finished := strings.Contains(text, "finished") && strings.Contains(text, "done=2/2") &&
		strings.Contains(text, "left=0s") && strings.Contains(text, ", stashed=")

	if !loggedClean || !loggedDirty || !finished {
		t.Fatal(text)
	}
}

// TestStashKeepsTheBranchCommit checks that a CAP branch and its commit survive the stash.
func TestStashKeepsTheBranchCommit(t *testing.T) {
	f := setup(t)
	git(t, f.repo, "switch", "-c", "CAP-1263")
	write(t, filepath.Join(f.repo, "file"), "committed\n")
	git(t, f.repo, "add", "file")
	git(t, f.repo, "commit", "-m", "cap work")
	write(t, filepath.Join(f.repo, "file"), "still dirty\n")
	file := stashFile(t, "group/repo with spaces")

	options := &WorkspaceStashOptions{BaseDir: f.base}

	result, err := StashWorkspace(t.Context(), stashGit(f), file, options)
	if err != nil || len(result.Stashed) != 1 {
		t.Fatal(result, err)
	}

	if git(t, f.repo, "branch", "--show-current") != "CAP-1263" ||
		git(t, f.repo, "log", "-1", "--format=%s") != "cap work" ||
		git(t, f.repo, "status", "--porcelain") != "" {
		t.Fatal("branch commit was moved or the dirty edit stayed")
	}
}

// TestStashSecondRunDoesNotPushAgain leaves a newer edit in the worktree.
func TestStashSecondRunDoesNotPushAgain(t *testing.T) {
	f := setup(t)
	write(t, filepath.Join(f.repo, "file"), "first\n")
	file := stashFile(t, "group/repo with spaces")
	client := stashGit(f)
	options := &WorkspaceStashOptions{BaseDir: f.base}

	first, err := StashWorkspace(t.Context(), client, file, options)
	if err != nil || len(first.Stashed) != 1 {
		t.Fatal(first, err)
	}

	write(t, filepath.Join(f.repo, "file"), "second\n")

	second, err := StashWorkspace(t.Context(), client, file, options)
	if err != nil || len(second.Stashed) != 0 || len(second.Kept) != 1 {
		t.Fatal(second, err)
	}

	if second.Kept[0].OID != first.Stashed[0].OID || strings.Count(git(t, f.repo, "stash", "list"), "\n") != 0 {
		t.Fatal(second.Kept, git(t, f.repo, "stash", "list"))
	}

	body, err := os.ReadFile(filepath.Join(f.repo, "file"))
	if err != nil || string(body) != "second\n" {
		t.Fatal(string(body), err)
	}

	spec := oneProject(t, "group/repo with spaces", nil)
	report, err := runWorkspace(t, f, spec, ModeStatus)

	if ExitCodeForWorkspace(err) != 3 || report.Rows[0].ReasonCode != reasonDirty {
		t.Fatalf("%+v %v", report, err)
	}
}

// TestStashPushesWhenAnOlderMessageDiffers adds a stash beside a message this command does not own.
func TestStashPushesWhenAnOlderMessageDiffers(t *testing.T) {
	f := setup(t)
	write(t, filepath.Join(f.repo, "file"), "other\n")
	git(t, f.repo, "stash", "push", "-u", "-m", "wip")
	write(t, filepath.Join(f.repo, "file"), "ours\n")
	file := stashFile(t, "group/repo with spaces")

	options := &WorkspaceStashOptions{BaseDir: f.base}

	result, err := StashWorkspace(t.Context(), stashGit(f), file, options)
	if err != nil || len(result.Stashed) != 1 || len(result.Kept) != 0 {
		t.Fatal(result, err)
	}

	list := git(t, f.repo, "stash", "list")
	if strings.Count(list, "\n") != 1 || !strings.Contains(list, "release-align") || !strings.Contains(list, "wip") {
		t.Fatal(list)
	}
}

// TestStashSkipsCleanAndMissingAndContinuesAfterOneFailure visits every listed path.
func TestStashSkipsCleanAndMissingAndContinuesAfterOneFailure(t *testing.T) {
	f := setup(t)
	clean := cloneRel(t, f, "group/clean")
	broken := filepath.Join(f.base, "group", "broken")

	if err := os.MkdirAll(broken, 0o700); err != nil {
		t.Fatal(err)
	}

	write(t, filepath.Join(f.repo, "file"), "dirty\n")
	file := stashFile(t, "group/broken", "group/missing", clean, "group/repo with spaces")

	options := &WorkspaceStashOptions{BaseDir: f.base}

	result, err := StashWorkspace(t.Context(), stashGit(f), file, options)
	if err == nil || !strings.Contains(err.Error(), "group/broken") {
		t.Fatal(result, err)
	}

	if result.Clean != 1 || len(result.Missing) != 1 || result.Missing[0] != "group/missing" ||
		len(result.Stashed) != 1 || result.Stashed[0].Path != "group/repo with spaces" {
		t.Fatal(result)
	}
}

// TestStashRejectsAnEmptyRequest leaves Git alone when the arguments are unusable.
func TestStashRejectsAnEmptyRequest(t *testing.T) {
	f := setup(t)
	file := stashFile(t, "group/repo with spaces")

	options := &WorkspaceStashOptions{BaseDir: f.base}

	_, err := StashWorkspace(t.Context(), nil, file, options)
	if err == nil {
		t.Fatal("nil git accepted")
	}

	empty := &WorkspaceStashOptions{}

	_, err = StashWorkspace(t.Context(), stashGit(f), file, empty)
	if err == nil {
		t.Fatal("empty base accepted")
	}
}

func stashLineLevel(text, path, message string) string {
	for line := range strings.SplitSeq(text, "\n") {
		if !strings.Contains(line, path) || !strings.Contains(line, message) {
			continue
		}

		switch {
		case strings.Contains(line, " INFO "):
			return "INFO"
		case strings.Contains(line, " WARN "):
			return "WARN"
		}
	}

	return ""
}

func stashGit(f *fixture) *gitter.Client {
	return &gitter.Client{
		LocalTimeout: f.cfg.LocalTimeout,
		NoLazyFetch:  true,
	}
}

func stashFile(t *testing.T, paths ...string) string {
	t.Helper()

	projects := make([]*ProjectSpec, 0, len(paths))
	for i, path := range paths {
		project := &ProjectSpec{
			Name: "p" + string(rune('a'+i)),
			Path: path,
		}
		projects = append(projects, project)
	}

	spec := &WorkspaceSpec{
		SchemaVersion: 1,
		DefaultRevision: &RevisionSpec{
			Branch: "master",
		},
		Projects: projects,
	}

	return saveWorkspace(t, spec)
}
