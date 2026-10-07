package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config is immutable after CLI parsing. Durations accept Go units or legacy seconds.
type Config struct {
	// BaseDir is the root directory walked for Git repositories.
	BaseDir string
	// Branch is the preferred origin branch.
	Branch string
	// VersionsFile overrides the built-in service version table when set.
	VersionsFile string
	// Depth is how many directory levels below BaseDir are searched.
	Depth int
	// Jobs is the number of repositories updated at once.
	Jobs int
	// Attempts is the total number of origin probe tries, not extra retries.
	Attempts int
	// DryRun plans updates without fetching or writing.
	DryRun bool
	// Local chooses skip, keep, or reset for local commits and edits.
	Local string
	// LogLevel is a zap level name. Case does not matter.
	LogLevel string
	// ProbeTimeout limits one origin reachability check.
	ProbeTimeout time.Duration
	// RetryDelay is the pause between probe attempts.
	RetryDelay time.Duration
	// FetchTimeout limits one git fetch.
	FetchTimeout time.Duration
	// LocalTimeout limits git commands that do not talk to a remote.
	LocalTimeout time.Duration
	// WorkspaceFile is an explicit project inventory. Empty keeps legacy discovery.
	WorkspaceFile string
	// Repositories selects workspace paths. Empty selects every project, unless Groups is set.
	Repositories []string
	// Groups selects workspace groups. Combined with Repositories as a union.
	Groups []string
	// Output is text or json.
	Output string
	// beforeCheckout runs once after a clean plan and before the first switch.
	// It is nil outside tests.
	beforeCheckout func()
	// afterCheckout runs after one switch and before that repository is read back.
	// It is nil outside tests.
	afterCheckout func(string)
}

const (
	// localSkip leaves a dirty tree or a diverged branch untouched.
	localSkip = "skip"
	// localKeep carries uncommitted edits onto the updated branch.
	localKeep = "keep"
	// localReset discards local commits and uncommitted edits.
	localReset = "reset"
	// gitNoOverwriteIgnore keeps ignored files from being replaced by checkout.
	gitNoOverwriteIgnore = "--no-overwrite-ignore"
)

// JSON reports whether stdout must contain only the workspace document.
func (c *Config) JSON() bool {
	return c != nil && c.Output == outputJSON
}

// ParseDuration accepts a Go duration or a bare number of seconds.
func (c *Config) ParseDuration(s string) (time.Duration, error) {
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		s += "s"
	}

	return time.ParseDuration(s)
}

// DefaultConfig returns the built-in paths, timeouts, and local policy.
func DefaultConfig() *Config {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	return &Config{
		BaseDir: filepath.Join(
			home,
			"go",
			"src",
			"gitlab.stageoffice.ru",
		),
		Branch:       "master",
		Depth:        2,
		Jobs:         4,
		Attempts:     3,
		Local:        localSkip,
		LogLevel:     "info",
		ProbeTimeout: 5 * time.Second,
		RetryDelay:   time.Second,
		FetchTimeout: 60 * time.Second,
		LocalTimeout: 40 * time.Second,
		Output:       outputText,
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
