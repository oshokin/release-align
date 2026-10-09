package app

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config is immutable after CLI parsing. Durations use Go units.
type Config struct {
	// BaseDir is the root that contains the clones named by the workspace file.
	BaseDir string
	// Branch replaces defaults.revision when the flag is present on the command line.
	Branch string
	// Jobs is the number of repositories updated at once.
	Jobs int
	// Attempts is the total number of origin probe tries, not extra retries.
	Attempts int
	// DryRun plans updates without fetching or writing.
	DryRun bool
	// IgnoreErrors checks out every repository that can move.
	// A blocked repository stays as it is and does not stop the others.
	IgnoreErrors bool
	// LogLevel is a zap level name. Case does not matter.
	LogLevel string
	// ProbeTimeout limits one origin reachability check.
	ProbeTimeout time.Duration
	// RetryDelay is the pause between probe attempts.
	RetryDelay time.Duration
	// FetchTimeout limits one git fetch.
	FetchTimeout time.Duration
	// CatalogTimeout limits one GitLab projects page.
	CatalogTimeout time.Duration
	// CatalogBudget limits one GitLab group listing.
	CatalogBudget time.Duration
	// LocalTimeout limits git commands that do not talk to a remote.
	LocalTimeout time.Duration
	// CloneTimeout limits one git clone.
	CloneTimeout time.Duration
	// ArchiveTimeout limits one repository export, including its ZIP copy.
	ArchiveTimeout time.Duration
	// timeoutLocks records values set by an explicit flag or environment variable.
	timeoutLocks *TimeoutLocks
	// WorkspaceFile is the project inventory. It is required.
	WorkspaceFile string
	// Repositories selects workspace paths. Empty selects every project, unless Groups is set.
	Repositories []string
	// Groups selects workspace groups. Combined with Repositories as a union.
	Groups []string
	// Output is text or json.
	Output string
	// Remote compares the workspace with GitLab after the local operation.
	Remote bool
	// progress receives lines that must stay off JSON stdout.
	progress io.Writer
	// remoteHooks replaces the token and HTTP client in tests.
	remoteHooks *remoteHooks
	// beforeCheckout runs once after a clean plan and before the first switch.
	// It is nil outside tests.
	beforeCheckout func()
	// afterCheckout runs after one switch and before that repository is read back.
	// It is nil outside tests.
	afterCheckout func(string)
}

const (
	// gitNoOverwriteIgnore keeps ignored files from being replaced by checkout.
	gitNoOverwriteIgnore = "--no-overwrite-ignore"
)

// SetProgress records where inventory progress is written. Nil discards it.
func (c *Config) SetProgress(w io.Writer) {
	if c == nil {
		return
	}

	c.progress = w
}

// JSON reports whether stdout must contain only the workspace document.
func (c *Config) JSON() bool {
	return c != nil && c.Output == outputJSON
}

// DefaultConfig returns the built-in timeouts and output format.
// The base directory comes from the flag, RELEASE_ALIGN_BASE_DIR, or the workspace file.
func DefaultConfig() *Config {
	return &Config{
		Branch:         "master",
		Jobs:           4,
		Attempts:       3,
		LogLevel:       "info",
		ProbeTimeout:   5 * time.Second,
		RetryDelay:     time.Second,
		FetchTimeout:   60 * time.Second,
		CatalogTimeout: 30 * time.Second,
		CatalogBudget:  3 * time.Minute,
		LocalTimeout:   40 * time.Second,
		CloneTimeout:   15 * time.Minute,
		ArchiveTimeout: 15 * time.Minute,
		Output:         outputText,
	}
}

// expandHomeWhenNeeded expands a leading ~ in the base directory.
func (c *Config) expandHomeWhenNeeded() error {
	if c.BaseDir != "~" && !strings.HasPrefix(c.BaseDir, "~/") {
		return nil
	}

	return c.expandHome()
}

// expandHome replaces a leading ~ with the current user's home directory.
func (c *Config) expandHome() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	if c.BaseDir == "~" {
		c.BaseDir = home

		return nil
	}

	c.BaseDir = filepath.Join(home, strings.TrimPrefix(c.BaseDir, "~/"))

	return nil
}
