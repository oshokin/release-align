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

func TestFailFastExactlyThreeAttempts(t *testing.T) {
	f := setup(t)
	count := filepath.Join(t.TempDir(), "calls")
	sshScript(
		t,
		fmt.Sprintf(
			"echo x >> '%s'\necho 'ssh: Could not resolve hostname gitlab.stageoffice.ru' >&2\nexit 255\n",
			count,
		),
	)
	git(t, f.repo, "remote", "set-url", "origin", "git@gitlab.stageoffice.ru:group/repo.git")

	before := git(t, f.repo, "rev-parse", "HEAD")
	s, log, e := run(t, f, nil)

	if e == nil || s.Canceled != 1 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}

	b, err := os.ReadFile(count)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Count(string(b), "x") != 3 {
		t.Fatalf("calls: %s", b)
	}

	if git(t, f.repo, "rev-parse", "HEAD") != before {
		t.Fatal("changed on failed preflight")
	}

	if _, statErr := os.Stat(filepath.Join(f.base, ".release-align.lock")); !os.IsNotExist(statErr) {
		t.Fatal("lock not released")
	}
}

func TestPermissionFailureContinuesOtherRepositories(t *testing.T) {
	f := setup(t)
	second := filepath.Join(f.base, "group", "second")
	git(t, f.base, "clone", f.remote, second)

	count := filepath.Join(t.TempDir(), "calls")
	sshScript(t, fmt.Sprintf("echo x >> '%s'\necho 'Permission denied (publickey).' >&2\nexit 255\n", count))
	git(t, f.repo, "remote", "set-url", "origin", "git@gitlab.stageoffice.ru:group/repo.git")

	s, log, e := run(t, f, nil)
	if e == nil || s.Failed != 1 || s.Updated != 1 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}

	b, err := os.ReadFile(count)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Count(string(b), "x") != 1 {
		t.Fatal("retried authorization error")
	}
}

func TestOutageDuringFetchCancelsRemaining(t *testing.T) {
	f := setup(t)
	f.cfg.Jobs = 1
	count := filepath.Join(t.TempDir(), "calls")
	sshScript(
		t,
		fmt.Sprintf(
			"echo x >> '%s'\nif [ \"$(wc -l < '%s')\" -eq 1 ]; then exec git-upload-pack '%s'; fi\necho 'Connection refused' >&2\nexit 255\n",
			count,
			count,
			f.remote,
		),
	)

	for _, name := range []string{"second", "third"} {
		git(t, f.base, "clone", f.remote, filepath.Join(f.base, "group", name))
	}

	for _, name := range []string{filepath.Base(f.repo), "second", "third"} {
		git(
			t,
			filepath.Join(f.base, "group", name),
			"remote",
			"set-url",
			"origin",
			"git@gitlab.stageoffice.ru:group/"+name+".git",
		)
	}

	s, log, e := run(t, f, nil)
	if e == nil || s.Canceled != 3 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}

	b, err := os.ReadFile(count)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Count(string(b), "x") != 5 {
		t.Fatalf("expected preflight + fetch + 3 checks, got %s", b)
	}
}

func TestTransientPreflightRecoversOnThirdAttempt(t *testing.T) {
	f := setup(t)
	count := filepath.Join(t.TempDir(), "calls")
	sshScript(
		t,
		fmt.Sprintf(
			"echo x >> '%s'\nif [ \"$(wc -l < '%s')\" -lt 3 ]; then echo 'Connection refused' >&2; exit 255; fi\nexec git-upload-pack '%s'\n",
			count,
			count,
			f.remote,
		),
	)
	git(t, f.repo, "remote", "set-url", "origin", "git@gitlab.stageoffice.ru:group/repo.git")

	s, log, e := run(t, f, nil)
	if e != nil || s.Updated != 1 {
		t.Fatalf("%+v %v\n%s", s, e, log)
	}

	b, err := os.ReadFile(count)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Count(string(b), "x") != 4 {
		t.Fatalf("expected 3 probes + 1 fetch: %s", b)
	}
}

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
