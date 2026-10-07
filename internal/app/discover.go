package app

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// discover lists Git repositories under base, down to depth.
func discover(ctx context.Context, base string, depth int) ([]string, error) {
	info, err := os.Stat(base)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		return nil, errBaseDirNotDirectory
	}

	var repos []string

	err = filepath.WalkDir(base, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}

		found, visitErr := visitRepository(base, path, d, depth)
		if found != "" {
			repos = append(repos, found)
		}

		return visitErr
	})

	sort.Strings(repos)

	return repos, err
}

// visitRepository records a repository at the requested depth and stops at another work tree.
func visitRepository(base, path string, d fs.DirEntry, depth int) (string, error) {
	if !d.IsDir() || path == base {
		return "", nil
	}

	if strings.HasPrefix(d.Name(), ".") {
		return "", filepath.SkipDir
	}

	rel, err := filepath.Rel(base, path)
	if err != nil {
		return "", err
	}

	level := len(strings.Split(rel, string(filepath.Separator)))
	_, gitErr := os.Stat(filepath.Join(path, ".git"))

	if gitErr != nil && !os.IsNotExist(gitErr) {
		return "", gitErr
	}

	found := ""
	if level == depth && gitErr == nil {
		found = path
	}

	// The depth limit and any Git work tree both end the walk.
	if level == depth || gitErr == nil {
		return found, filepath.SkipDir
	}

	return found, nil
}

// One invocation per base-dir; Git still owns its normal per-operation locks.
func lockBase(base string) (func(), error) {
	path := filepath.Join(base, ".release-align.lock")
	if err := os.Mkdir(path, 0o700); err != nil {
		return nil, fmt.Errorf("cannot acquire %s (another run or a stale lock): %w", path, err)
	}

	if err := os.WriteFile(
		filepath.Join(path, "owner"),
		fmt.Appendf(nil, "pid=%d\n", os.Getpid()),
		0o600,
	); err != nil {
		_ = os.Remove(path)
		return nil, err
	}

	return func() {
		_ = os.Remove(filepath.Join(path, "owner"))
		_ = os.Remove(path)
	}, nil
}
