package app

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/oshokin/release-align/internal/gitter"
	"github.com/oshokin/release-align/internal/logger"
)

const (
	// maxDirtyPaths is how many blocking paths an info line lists. Debug lists every path.
	maxDirtyPaths = 10
)

// safeCurrent reports why this repository must be left untouched.
// localKeep allows a dirty tree through. localReset allows a dirty tree and a diverged branch through.
// A detached HEAD and an unfinished merge or rebase are always a skip.
func (r *runner) safeCurrent(ctx context.Context, repo *repository) (string, error) {
	status, err := r.porcelain(ctx, repo)
	if err != nil {
		return "", err
	}

	if status != "" && r.cfg.Local == localSkip {
		return r.dirtyTreeReason(status, r.dirtyPathLimit(ctx)), nil
	}

	reason, err := r.unfinishedOperation(ctx, repo)
	if reason != "" || err != nil {
		return reason, err
	}

	branch, err := r.git.Local(ctx, repo.path, "symbolic-ref", "--quiet", "--short", "HEAD")
	if gitter.ExitCode(err) == 1 {
		return messageDetachedHead, nil
	}

	if err != nil {
		return "", err
	}

	if r.cfg.Local == localReset {
		return "", nil
	}

	upstream, err := r.git.Local(
		ctx,
		repo.path,
		"for-each-ref",
		"--format=%(upstream:remotename) %(upstream)",
		"refs/heads/"+branch,
	)
	if err != nil {
		return "", err
	}

	fields := strings.Fields(upstream)
	if len(fields) != 2 {
		return "current branch has no upstream", nil
	}

	if fields[0] != "origin" || !strings.HasPrefix(fields[1], "refs/remotes/origin/") {
		return "current branch tracks a remote other than origin", nil
	}

	exists, err := r.refExists(ctx, repo, fields[1])
	if err != nil {
		return "", err
	}

	if !exists {
		return "current upstream ref is missing", nil
	}

	ok, err := r.ancestor(ctx, repo, "HEAD", fields[1])
	if err != nil {
		return "", err
	}

	if !ok {
		return "current branch has unpushed commits or diverges from origin", nil
	}

	return "", nil
}

// unfinishedOperation reports a merge, rebase, cherry-pick, revert, or bisect still in progress.
func (r *runner) unfinishedOperation(ctx context.Context, repo *repository) (string, error) {
	names := gitOperationNames()

	for _, name := range names {
		path, err := r.git.Local(ctx, repo.path, "rev-parse", "--git-path", name)
		if err != nil {
			return "", err
		}

		if !filepath.IsAbs(path) {
			path = filepath.Join(repo.path, path)
		}

		exists, err := r.pathExists(path)
		if err != nil {
			return "", err
		}

		if exists {
			return "unfinished Git operation: " + name, nil
		}
	}

	return "", nil
}

// porcelain reads the paths that a checkout would have to preserve or refuse.
func (r *runner) porcelain(ctx context.Context, repo *repository) (string, error) {
	return r.git.Local(ctx, repo.path, "status", "--porcelain=v1", "-z", "--untracked-files=normal")
}

// pathExists reports whether path is present.
func (r *runner) pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}

	if os.IsNotExist(err) {
		return false, nil
	}

	return false, err
}

// dirtyPathLimit is maxDirtyPaths, or zero when debug logging is on.
func (r *runner) dirtyPathLimit(ctx context.Context) int {
	if logger.DebugEnabled(ctx) {
		return 0
	}

	return maxDirtyPaths
}

// dirtyTreeReason names the paths that block an update.
// A positive limit keeps the first paths and appends a count of the rest. Zero lists every path.
func (r *runner) dirtyTreeReason(status string, limit int) string {
	paths := r.porcelainPaths(status)
	if len(paths) == 0 {
		return messageDirtyTree
	}

	extra := 0
	if limit > 0 && len(paths) > limit {
		extra = len(paths) - limit
		paths = paths[:limit]
	}

	var b strings.Builder

	b.WriteString(messageDirtyTree)

	for _, path := range paths {
		b.WriteString("\n  ")
		b.WriteString(path)
	}

	if extra > 0 {
		b.WriteString("\n  ... and ")
		b.WriteString(strconv.Itoa(extra))
		b.WriteString(" more")
	}

	return b.String()
}

// porcelainPaths reads git status --porcelain=v1 -z.
// A rename or copy is "XY new\\0old\\0"; every other record is "XY path\\0".
func (r *runner) porcelainPaths(status string) []string {
	parts := strings.Split(status, "\x00")
	paths := make([]string, 0, len(parts))

	for i := 0; i < len(parts); i++ {
		entry := parts[i]
		if len(entry) < 4 {
			continue
		}

		code := entry[:2]
		path := entry[3:]

		next := ""
		if i+1 < len(parts) {
			next = parts[i+1]
		}

		renamed := code[0] == 'R' || code[0] == 'C' || code[1] == 'R' || code[1] == 'C'
		if renamed && next != "" {
			i++
			path = next + " -> " + path
		}

		paths = append(paths, path)
	}

	return paths
}
