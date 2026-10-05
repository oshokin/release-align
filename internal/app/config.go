package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/oshokin/release-align/internal/logger"
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

// ApplyEnv retains the useful settings of the original shell script.
func (c *Config) ApplyEnv(getenv func(string) string) error {
	for key, dst := range map[string]*string{"BASE_DIR": &c.BaseDir, "RELEASE_BRANCH": &c.Branch, "VERSIONS_FILE": &c.VersionsFile} {
		if s := getenv(key); s != "" {
			*dst = s
		}
	}

	for key, dst := range map[string]*int{"MAX_DEPTH": &c.Depth, "JOBS": &c.Jobs, "ATTEMPTS": &c.Attempts} {
		s := getenv(key)
		if s == "" {
			continue
		}

		n, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}

		*dst = n
	}

	if s := getenv("LOG_LEVEL"); s != "" {
		c.LogLevel = s
	}

	if s := getenv("LOCAL"); s != "" {
		c.Local = s
	}

	for key, dst := range map[string]*bool{"DRY_RUN": &c.DryRun} {
		s := getenv(key)
		if s == "" {
			continue
		}

		b, err := strconv.ParseBool(s)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}

		*dst = b
	}

	for key, dst := range map[string]*time.Duration{"FETCH_TIMEOUT": &c.FetchTimeout, "LSREMOTE_TIMEOUT": &c.ProbeTimeout, "CHECKOUT_TIMEOUT": &c.LocalTimeout, "RETRY_DELAY": &c.RetryDelay} {
		s := getenv(key)
		if s == "" {
			continue
		}

		d, err := c.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}

		*dst = d
	}

	return nil
}

// Validate checks numeric limits and expands a leading ~ in the base directory.
func (c *Config) Validate() error {
	if c.BaseDir == "" {
		return errBaseDirEmpty
	}

	if c.BaseDir == "~" || strings.HasPrefix(c.BaseDir, "~/") {
		if err := c.expandHome(); err != nil {
			return err
		}
	}

	p, err := filepath.Abs(c.BaseDir)
	if err != nil {
		return err
	}

	c.BaseDir = p
	if c.Depth < 1 || c.Depth > 32 {
		return errDepthRange
	}

	if c.Jobs < 1 || c.Jobs > 64 {
		return errJobsRange
	}

	if c.Attempts < 1 || c.Attempts > 10 {
		return errAttemptsRange
	}

	if c.ProbeTimeout <= 0 || c.FetchTimeout <= 0 || c.LocalTimeout <= 0 || c.RetryDelay < 0 {
		return errTimeoutRange
	}

	if c.Branch == "" || strings.HasPrefix(c.Branch, "-") {
		return errInvalidBranch
	}

	if _, ok := logger.ParseLogLevel(c.LogLevel); !ok {
		return fmt.Errorf("%s: %w", c.LogLevel, errInvalidLogLevel)
	}

	switch strings.ToLower(strings.TrimSpace(c.Local)) {
	case localSkip, localKeep, localReset:
		c.Local = strings.ToLower(strings.TrimSpace(c.Local))
	default:
		return fmt.Errorf("%s: %w", c.Local, errInvalidLocal)
	}

	return nil
}

// ParseDuration accepts a Go duration or a bare number of seconds.
func (c *Config) ParseDuration(s string) (time.Duration, error) {
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		s += "s"
	}

	return time.ParseDuration(s)
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
	}
}
