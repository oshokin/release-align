package app

import (
	"fmt"
	"strconv"
	"time"
)

// ApplyEnv retains the useful settings of the original shell script.
func (c *Config) ApplyEnv(getenv func(string) string) error {
	c.applyStringEnv(getenv)

	if err := c.applyIntEnv(getenv); err != nil {
		return err
	}

	if s := getenv("LOG_LEVEL"); s != "" {
		c.LogLevel = s
	}

	if s := getenv("LOCAL"); s != "" {
		c.Local = s
	}

	if err := c.applyBoolEnv(getenv); err != nil {
		return err
	}

	return c.applyDurationEnv(getenv)
}

// applyStringEnv copies non-empty path and branch settings.
func (c *Config) applyStringEnv(getenv func(string) string) {
	if s := getenv("BASE_DIR"); s != "" {
		c.BaseDir = s
	}

	if s := getenv("RELEASE_BRANCH"); s != "" {
		c.Branch = s
	}

	if s := getenv("VERSIONS_FILE"); s != "" {
		c.VersionsFile = s
	}
}

// applyIntEnv copies numeric limits. An empty variable leaves the flag value.
func (c *Config) applyIntEnv(getenv func(string) string) error {
	depth, ok, err := c.envInt(getenv, "MAX_DEPTH")
	if err != nil {
		return err
	}

	if ok {
		c.Depth = depth
	}

	jobs, ok, err := c.envInt(getenv, "JOBS")
	if err != nil {
		return err
	}

	if ok {
		c.Jobs = jobs
	}

	attempts, ok, err := c.envInt(getenv, "ATTEMPTS")
	if err != nil {
		return err
	}

	if ok {
		c.Attempts = attempts
	}

	return nil
}

// envInt parses one integer variable. ok is false when the variable is empty.
func (*Config) envInt(getenv func(string) string, key string) (int, bool, error) {
	s := getenv(key)
	if s == "" {
		return 0, false, nil
	}

	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false, fmt.Errorf("%s: %w", key, err)
	}

	return n, true, nil
}

// applyBoolEnv copies DRY_RUN when the variable is set.
func (c *Config) applyBoolEnv(getenv func(string) string) error {
	key := "DRY_RUN"

	s := getenv(key)
	if s == "" {
		return nil
	}

	b, err := strconv.ParseBool(s)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}

	c.DryRun = b

	return nil
}

// applyDurationEnv copies timeouts. A bare number is seconds, via ParseDuration.
func (c *Config) applyDurationEnv(getenv func(string) string) error {
	fetch, ok, err := c.envDuration(getenv, "FETCH_TIMEOUT")
	if err != nil {
		return err
	}

	if ok {
		c.FetchTimeout = fetch
	}

	probe, ok, err := c.envDuration(getenv, "LSREMOTE_TIMEOUT")
	if err != nil {
		return err
	}

	if ok {
		c.ProbeTimeout = probe
	}

	checkout, ok, err := c.envDuration(getenv, "CHECKOUT_TIMEOUT")
	if err != nil {
		return err
	}

	if ok {
		c.LocalTimeout = checkout
	}

	delay, ok, err := c.envDuration(getenv, "RETRY_DELAY")
	if err != nil {
		return err
	}

	if ok {
		c.RetryDelay = delay
	}

	return nil
}

// envDuration parses one duration variable. ok is false when the variable is empty.
func (c *Config) envDuration(getenv func(string) string, key string) (time.Duration, bool, error) {
	s := getenv(key)
	if s == "" {
		return 0, false, nil
	}

	d, err := c.ParseDuration(s)
	if err != nil {
		return 0, false, fmt.Errorf("%s: %w", key, err)
	}

	return d, true, nil
}
