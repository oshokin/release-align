package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oshokin/release-align/internal/gitter"
)

// TestWorkspaceRealGitResolvesExactly verifies that Git resolves the requested branch, tag, or commit and nothing else.
func TestWorkspaceRealGitResolvesExactly(t *testing.T) {
	f := setup(t)
	oid := branch(t, f, "Release-26.3.0")
	git(t, f.repo, "fetch", "origin")
	g := &gitter.Client{
		LocalTimeout: 5 * time.Second,
	}
	ctx := context.Background()
	release := &RevisionSpec{
		Branch: "Release-26.3.0",
	}
	target, err := ResolveRevision(ctx, g, f.repo, release)

	if err != nil || target.OID != oid {
		t.Fatal(target, err)
	}

	absent := &RevisionSpec{
		Branch: "absent",
	}
	_, err = ResolveRevision(ctx, g, f.repo, absent)

	if !errors.Is(err, ErrWorkspaceTargetMissing) {
		t.Fatal("fallback instead of missing", err)
	}

	git(t, f.repo, "tag", "-a", "v-test", "-m", "test", oid)
	annotated := &RevisionSpec{
		Tag: "v-test",
	}

	tag, err := ResolveRevision(ctx, g, f.repo, annotated)
	if err != nil || tag.OID != oid {
		t.Fatal("annotated tag not peeled", tag, err)
	}

	exact := &RevisionSpec{
		Commit: oid,
	}

	pinned, err := ResolveRevision(ctx, g, f.repo, exact)
	if err != nil || pinned.OID != oid {
		t.Fatal(pinned, err)
	}

	git(t, f.repo, "switch", "--track", "origin/Release-26.3.0")
	state, err := ObserveWorkspaceState(ctx, g, f.repo)
	if err != nil || !state.Verified || state.Dirty || state.Head != oid {
		t.Fatal(state, err)
	}

	write(t, filepath.Join(f.repo, "untracked"), "local")
	state, err = ObserveWorkspaceState(ctx, g, f.repo)
	if err != nil || !state.Dirty {
		t.Fatal("dirty state hidden", err)
	}

	git(t, f.repo, "checkout", "--detach", oid)
	state, err = ObserveWorkspaceState(ctx, g, f.repo)
	if err != nil || state.Branch != "" {
		t.Fatal("detached observation failed", err)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()

	_, err = ResolveRevision(canceled, g, f.repo, release)

	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation swallowed", err)
	}
}

// TestWorkspaceDirectoryBoundaries verifies path escapes, missing clones, and directory symlinks.
func TestWorkspaceDirectoryBoundaries(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "a", "repo with spaces"), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := ResolveProjectDirectory(base, "a/repo with spaces"); err != nil {
		t.Fatal(err)
	}

	if _, err := ResolveProjectDirectory(base, "a/missing"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing clone not detected", err)
	}

	if err := os.Symlink(t.TempDir(), filepath.Join(base, "outside")); err != nil {
		t.Skip("directory symlinks unavailable")
	}

	if _, err := ResolveProjectDirectory(base, "outside"); err == nil {
		t.Fatal("symlink accepted")
	}
}
