package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestReviewStatusDoesNotLazyFetch verifies that partial-clone object lookup stays offline.
func TestReviewStatusDoesNotLazyFetch(t *testing.T) {
	f, _, calls := partialClone(t)
	revision := &RevisionSpec{
		Commit: git(t, f.seed, "rev-parse", "HEAD"),
	}
	spec := oneProject(t, "group/repo with spaces", revision)
	report, err := runWorkspace(t, f, spec, ModeStatus)
	t.Logf("err=%v report=%+v", err, report)

	if _, statErr := os.Stat(calls); !os.IsNotExist(statErr) {
		t.Fatal("offline status contacted origin to lazily fetch a commit")
	}
}

// TestReviewDryRunDoesNotLazyFetch keeps a dry-run from fetching or changing local Git state.
func TestReviewDryRunDoesNotLazyFetch(t *testing.T) {
	f, oid, calls := partialClone(t)
	f.cfg.DryRun = true
	before := localGitSnapshot(t, f.repo)
	revisionSpec := &RevisionSpec{
		Commit: oid,
	}
	spec := oneProject(t, "group/repo with spaces", revisionSpec)
	_, err := runWorkspace(t, f, spec, ModeSync)
	t.Logf("err=%v", err)

	if _, statErr := os.Stat(calls); !os.IsNotExist(statErr) {
		t.Fatal("dry-run contacted origin to lazily fetch a commit")
	}

	if after := localGitSnapshot(t, f.repo); after != before {
		t.Fatal("dry-run changed HEAD, the index, or FETCH_HEAD")
	}
}

// TestReviewSyncStillFetchesPromisor checks that a normal sync may still contact origin.
func TestReviewSyncStillFetchesPromisor(t *testing.T) {
	f, oid, calls := partialClone(t)
	revisionSpec := &RevisionSpec{
		Commit: oid,
	}
	spec := oneProject(t, "group/repo with spaces", revisionSpec)
	_, err := runWorkspace(t, f, spec, ModeSync)
	t.Logf("err=%v", err)

	b, readErr := os.ReadFile(calls)
	if readErr != nil || len(b) == 0 {
		t.Fatal("sync did not fetch", readErr)
	}
}

// partialClone is a local clone whose missing release commit would require a promisor fetch.
func partialClone(t *testing.T) (*fixture, string, string) {
	t.Helper()

	f := setup(t)
	oid := branch(t, f, "Release-26.3.0")
	git(t, f.repo, "config", "remote.origin.promisor", "true")
	git(t, f.repo, "config", "remote.origin.partialclonefilter", "blob:none")
	git(t, f.repo, "remote", "set-url", "origin", "git@gitlab.example:group/repo.git")
	calls := filepath.Join(t.TempDir(), "calls")
	body := fmt.Sprintf("echo call >> '%s'\nexec git-upload-pack '%s'\n", calls, f.remote)
	sshScript(t, body)

	return f, oid, calls
}

// localGitSnapshot records HEAD, the index, and FETCH_HEAD so an offline command can be compared.
func localGitSnapshot(t *testing.T, repo string) string {
	t.Helper()

	head := git(t, repo, "rev-parse", "HEAD")

	index, err := os.ReadFile(filepath.Join(repo, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}

	fetch, err := os.ReadFile(filepath.Join(repo, ".git", "FETCH_HEAD"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	return head + "\n" + string(index) + "\n" + string(fetch)
}
