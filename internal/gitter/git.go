// Package gitter runs the installed Git executable, retaining SSH and credential configuration.
package gitter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// Client runs the installed git executable.
type Client struct {
	// LocalTimeout limits Git commands that do not talk to a remote.
	LocalTimeout time.Duration
	// NoLazyFetch sets GIT_NO_LAZY_FETCH=1 for this client's processes.
	// A Git build that does not understand the variable may still contact a promisor remote.
	NoLazyFetch bool
}

// CommandError is a failed git invocation, including its arguments and combined output.
type CommandError struct {
	// Args are the Git arguments that were run.
	Args []string
	// Output is the original combined stdout and stderr, kept for classification.
	// Print Error, which removes URL credentials. Do not print Output directly.
	Output string
	// Err is the process error.
	Err error
}

// Run never invokes a shell. Timeout includes Git and its subprocess tree.
func (c *Client) Run(ctx context.Context, dir string, timeout time.Duration, args ...string) (string, error) {
	if timeout == 0 {
		timeout = c.LocalTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)

	defer cancel()

	const configFlag = "-c"

	//nolint:gosec // G204: git is invoked directly; arguments are Git subcommands, not a shell.
	cmd := exec.CommandContext(
		ctx,
		"git",
		slices.Concat(
			[]string{configFlag, "credential.interactive=false", configFlag, "submodule.recurse=false"},
			args,
		)...)
	cmd.Dir = dir

	cmd.Env = c.commandEnv()
	cmd.WaitDelay = 250 * time.Millisecond
	c.configureProcess(cmd)

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	if ctx.Err() != nil {
		err = ctx.Err()
	}

	if err != nil {
		return "", &CommandError{
			Args:   args,
			Output: strings.TrimSpace(stderr.String() + stdout.String()),
			Err:    err,
		}
	}

	// Trim only the trailing newline. Porcelain v1 uses a leading space (" M file").
	return strings.TrimRight(stdout.String(), "\r\n"), nil
}

// Local runs git with the client's local timeout.
func (c *Client) Local(ctx context.Context, dir string, args ...string) (string, error) {
	return c.Run(ctx, dir, 0, args...)
}

// Probe checks that origin answers ls-remote.
func (c *Client) Probe(ctx context.Context, dir string, timeout time.Duration) error {
	_, err := c.Run(ctx, dir, timeout, "ls-remote", "--quiet", "origin", "HEAD")
	return err
}

// ProbeURL checks that one remote URL answers ls-remote. dir may be empty.
func (c *Client) ProbeURL(ctx context.Context, remote string, timeout time.Duration) error {
	_, err := c.Run(ctx, "", timeout, "ls-remote", "--quiet", "--", remote, "HEAD")
	return err
}

// Clone copies remote into dir with a normal checkout of the remote default branch.
// dir's parent must exist. Submodules are not cloned.
func (c *Client) Clone(ctx context.Context, remote, dir string, timeout time.Duration) error {
	_, err := c.Run(ctx, "", timeout, "clone", "--no-recurse-submodules", "--", remote, dir)
	return err
}

// Fetch updates origin branches and tags without pruning local-only tags.
func (c *Client) Fetch(ctx context.Context, dir string, timeout time.Duration) error {
	// Explicit branch refspec; --tags does not put local-only tags under --prune.
	// Override pruneTags even when enabled in global/remote configuration.
	_, err := c.Run(
		ctx,
		dir,
		timeout,
		"-c",
		"fetch.pruneTags=false",
		"-c",
		"remote.origin.pruneTags=false",
		"fetch",
		"--refmap=",
		"--atomic",
		"--prune",
		"--no-prune-tags",
		"--tags",
		"--no-recurse-submodules",
		"--quiet",
		"origin",
		"+refs/heads/*:refs/remotes/origin/*",
	)

	return err
}

// Archive writes one revision to a ZIP file. It does not fetch or check out that revision.
func (c *Client) Archive(ctx context.Context, dir, prefix, output, oid string, timeout time.Duration) error {
	_, err := c.Run(
		ctx,
		dir,
		timeout,
		"archive",
		"--format=zip",
		"--prefix="+prefix,
		"--output="+output,
		oid,
	)

	return err
}

// Error formats the failed git command with URL credentials removed.
func (e *CommandError) Error() string {
	return RedactText(fmt.Sprintf("git %s: %v: %s", strings.Join(e.Args, " "), e.Err, e.Output))
}

// Unwrap returns the error from the git process.
func (e *CommandError) Unwrap() error {
	return e.Err
}

// ExitCode returns the process status from an exec error, or -1.
func ExitCode(err error) int {
	if e, ok := errors.AsType[*exec.ExitError](err); ok {
		return e.ExitCode()
	}

	return -1
}

// NetworkError reports a failure to reach the remote.
func NetworkError(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	if err == nil {
		return false
	}

	s := strings.ToLower(err.Error())

	markers := []string{
		"could not resolve",
		"name or service not known",
		"temporary failure in name resolution",
		"connection timed out",
		"operation timed out",
		"connection refused",
		"failed to connect",
		"network is unreachable",
		"no route to host",
		"connection reset",
		"connection closed",
		"remote end hung up",
		"early eof",
		"unexpected disconnect",
		"empty reply",
		"http 500",
		"http 502",
		"http 503",
		"http 504",
		"error: 500",
		"error: 502",
		"error: 503",
		"error: 504",
		"tls connection was non-properly terminated",
		"ssl_connect",
	}

	for _, v := range markers {
		if strings.Contains(s, v) {
			return true
		}
	}

	return false
}

// commandEnv isolates repository selection and applies noninteractive Git settings.
func (c *Client) commandEnv() []string {
	var env []string

	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "GIT_DIR",
			"GIT_WORK_TREE",
			"GIT_INDEX_FILE",
			"GIT_COMMON_DIR",
			"GIT_OBJECT_DIRECTORY",
			"GIT_ALTERNATE_OBJECT_DIRECTORIES",
			"GIT_NAMESPACE":
			continue
		}

		env = append(env, entry)
	}

	ssh := os.Getenv("GIT_SSH_COMMAND")
	if ssh == "" && os.Getenv("GIT_SSH") == "" {
		ssh = "ssh -oBatchMode=yes -oNumberOfPasswordPrompts=0 -oStrictHostKeyChecking=accept-new -oConnectTimeout=5 -oConnectionAttempts=1"
	}

	env = append(
		env,
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=Never",
		"GIT_ASKPASS=false",
		"SSH_ASKPASS=false",
		"SSH_ASKPASS_REQUIRE=never",
		"GIT_OPTIONAL_LOCKS=0",
		"LC_ALL=C",
		"LANG=C",
		"GIT_PAGER=cat",
	)
	if ssh != "" {
		env = append(env, "GIT_SSH_COMMAND="+ssh)
	}

	if c.NoLazyFetch {
		env = append(env, "GIT_NO_LAZY_FETCH=1")
	}

	return env
}
