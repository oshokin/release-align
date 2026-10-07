package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/oshokin/release-align/internal/logger"
)

// timeoutProbe blocks until the per-attempt deadline, the same contract as a hung ls-remote.
type timeoutProbe struct {
	// calls is how many origin checks ran.
	calls int
}

// errUnexpectedGit means the timeout fake was asked to run a Git command it does not model.
var errUnexpectedGit = errors.New("unexpected git command")

// TestCancellationInterruptsRetryDelay verifies that cancel stops a retry while it is waiting.
func TestCancellationInterruptsRetryDelay(t *testing.T) {
	f := setup(t)
	f.cfg.RetryDelay = time.Second

	sshScript(t, "echo 'Connection refused' >&2\nexit 255\n")
	git(t, f.repo, "remote", "set-url", "origin", "git@gitlab.stageoffice.ru:group/repo.git")

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)

		defer cancel()

		start := time.Now()

		var logs bytes.Buffer

		s, e := Run(logger.ToContext(ctx, logger.NewWithWriter(nil, &logs)), f.cfg, nil)

		if e == nil || s.Updated != 0 || time.Since(start) > time.Second {
			t.Fatal(s, e, time.Since(start))
		}
	})
}

// TestFailFastOneAttemptDoesNotEnableUnlimitedRetries verifies that one attempt does not turn retries into an infinite loop.
func TestFailFastOneAttemptDoesNotEnableUnlimitedRetries(t *testing.T) {
	f := setup(t)
	f.cfg.Attempts = 1
	count := filepath.Join(t.TempDir(), "calls")
	sshScript(t, fmt.Sprintf("echo x >> '%s'\necho 'Connection refused' >&2\nexit 255\n", count))
	git(t, f.repo, "remote", "set-url", "origin", "git@gitlab.stageoffice.ru:group/repo.git")
	// A separate parent deadline prevents a retry-budget regression hanging the test.
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	var logs bytes.Buffer

	s, e := Run(logger.ToContext(ctx, logger.NewWithWriter(nil, &logs)), f.cfg, nil)

	if e == nil || ctx.Err() != nil || s.Canceled != 1 {
		t.Fatalf("%+v %v\n%s", s, e, logs.String())
	}

	b, err := os.ReadFile(count)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Count(string(b), "x") != 1 {
		t.Fatalf("expected exactly one probe: %s", b)
	}
}

// TestIndividualProbeTimeoutIsRetried verifies that a timed-out probe is tried again.
func TestIndividualProbeTimeoutIsRetried(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Attempts = 3
		cfg.ProbeTimeout = time.Second
		cfg.RetryDelay = time.Second

		git := new(timeoutProbe)
		r := &runner{cfg: cfg, git: git}

		if err := r.useProbeRetries(); err != nil {
			t.Fatal(err)
		}

		ctx := logger.ToContext(t.Context(), logger.NewWithWriter(nil, new(bytes.Buffer)))
		repo := &repository{relative: "group/repo", endpoint: "ssh:gitlab.example"}
		summary := new(Summary)
		_, err := r.preflight(ctx, []*repository{repo}, summary)

		if err == nil || ctx.Err() != nil || summary.Canceled != 1 || git.calls != 3 {
			t.Fatalf("calls=%d canceled=%d ctx=%v err=%v", git.calls, summary.Canceled, ctx.Err(), err)
		}
	})
}

// Probe waits out the attempt deadline and reports it.
func (p *timeoutProbe) Probe(ctx context.Context, _ string, timeout time.Duration) error {
	p.calls++

	timed, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	<-timed.Done()

	return timed.Err()
}

// Local reports that this probe fake has no local Git commands.
func (p *timeoutProbe) Local(context.Context, string, ...string) (string, error) {
	return "", errUnexpectedGit
}

// Fetch reports that this probe fake has no fetch command.
func (p *timeoutProbe) Fetch(context.Context, string, time.Duration) error {
	return errUnexpectedGit
}
