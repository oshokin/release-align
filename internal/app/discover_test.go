package app

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/oshokin/release-align/internal/logger"
)

func TestParallelRepositories(t *testing.T) {
	f := setup(t)
	f.cfg.Jobs = 8

	for i := range 11 {
		git(t, f.base, "clone", f.remote, filepath.Join(f.base, "group", fmt.Sprintf("repo-%02d", i)))
	}

	branch(t, f, "release")

	f.cfg.Branch = "release"
	s, log, e := run(t, f, nil)

	if e != nil || s.Updated != 12 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}
}

func TestDiscoveryDepthAndWorktree(t *testing.T) {
	f := setup(t)
	other := filepath.Join(f.base, "group", "worktree")
	git(t, f.repo, "worktree", "add", "-b", "worktree", other, "origin/master")

	paths, e := discover(context.Background(), f.base, 2)
	if e != nil || len(paths) != 2 {
		t.Fatal(paths, e)
	}

	paths, e = discover(context.Background(), f.base, 1)
	if e != nil || len(paths) != 0 {
		t.Fatal(paths, e)
	}

	s, log, e := run(t, f, nil)
	if e == nil || s.Updated != 1 || s.Failed != 1 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}

	if git(t, other, "branch", "--show-current") != "worktree" {
		t.Fatal("worktree branch changed")
	}
}

func TestBaseLock(t *testing.T) {
	f := setup(t)

	unlock, e := lockBase(f.base)
	if e != nil {
		t.Fatal(e)
	}
	defer unlock()

	s, _, e := run(t, f, nil)
	if e == nil || s.Updated != 0 {
		t.Fatal("concurrent run accepted", s, e)
	}
}

func TestCanceledContext(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var logs bytes.Buffer

	_, e := Run(logger.ToContext(ctx, logger.NewWithWriter(nil, &logs)), f.cfg, nil)
	if e == nil {
		t.Fatal("cancellation ignored")
	}
}
