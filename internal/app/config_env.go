package app

import (
	"fmt"
	"strconv"
	"time"
)

// envIntValue is one integer taken from the environment.
type envIntValue struct {
	// value is the parsed number.
	value int
	// present reports that the variable was set.
	present bool
}

// envDurationValue is one duration taken from the environment.
type envDurationValue struct {
	// value is the parsed duration.
	value time.Duration
	// present reports that the variable was set.
	present bool
}

// envBoolValue is one boolean taken from the environment.
type envBoolValue struct {
	// value is the parsed flag.
	value bool
	// present reports that the variable was set.
	present bool
}

const (
	// EnvPrefix marks release-align settings in the process environment.
	// GITLAB_TOKEN stays unprefixed: it is a container credential, not a program setting.
	EnvPrefix = "RELEASE_ALIGN_"
	// envFetchTimeout is the suffix for one fetch.
	envFetchTimeout = "FETCH_TIMEOUT"
	// envCatalogTimeout is the suffix for one GitLab page.
	envCatalogTimeout = "CATALOG_TIMEOUT"
	// envCatalogBudget is the suffix for one GitLab group listing.
	envCatalogBudget = "CATALOG_BUDGET"
	// envProbeTimeout is the suffix for one reachability probe.
	envProbeTimeout = "PROBE_TIMEOUT"
	// envLocalTimeout is the suffix for one local Git command.
	envLocalTimeout = "LOCAL_TIMEOUT"
	// envRetryDelay is the suffix for the pause between probes.
	envRetryDelay = "RETRY_DELAY"
	// envCloneTimeout is the suffix for one clone.
	envCloneTimeout = "CLONE_TIMEOUT"
	// envArchiveTimeout is the suffix for one archive.
	envArchiveTimeout = "ARCHIVE_TIMEOUT"
)

// Env returns the process variable for one release-align setting.
func Env(name string) string {
	return EnvPrefix + name
}

// ApplyEnv copies settings whose names match the flags.
// name is the suffix. The process variable is RELEASE_ALIGN_ plus that suffix.
func (c *Config) ApplyEnv(getenv func(string) string) error {
	c.applyStringEnv(getenv)

	if err := c.applyIntEnv(getenv); err != nil {
		return err
	}

	if s := getenv("LOG_LEVEL"); s != "" {
		c.LogLevel = s
	}

	if err := c.applyBoolEnv(getenv); err != nil {
		return err
	}

	return c.applyDurationEnv(getenv)
}

// applyStringEnv copies a non-empty base directory.
func (c *Config) applyStringEnv(getenv func(string) string) {
	if s := getenv("BASE_DIR"); s != "" {
		c.BaseDir = s
	}
}

// applyIntEnv copies numeric limits. An empty variable leaves the flag value.
func (c *Config) applyIntEnv(getenv func(string) string) error {
	jobs, err := c.envInt(getenv, "JOBS")
	if err != nil {
		return err
	}

	if jobs.present {
		c.Jobs = jobs.value
	}

	attempts, err := c.envInt(getenv, "ATTEMPTS")
	if err != nil {
		return err
	}

	if attempts.present {
		c.Attempts = attempts.value
	}

	return nil
}

// envInt parses one integer variable. ok is false when the variable is empty.
func (*Config) envInt(getenv func(string) string, key string) (*envIntValue, error) {
	parsed := new(envIntValue)

	s := getenv(key)
	if s == "" {
		return parsed, nil
	}

	n, err := strconv.Atoi(s)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", Env(key), err)
	}

	parsed.value = n
	parsed.present = true

	return parsed, nil
}

// applyBoolEnv copies boolean settings when their variables are set.
func (c *Config) applyBoolEnv(getenv func(string) string) error {
	dry, err := c.envBool(getenv, "DRY_RUN")
	if err != nil {
		return err
	}

	if dry.present {
		c.DryRun = dry.value
	}

	ignored, err := c.envBool(getenv, "IGNORE_ERRORS")
	if err != nil {
		return err
	}

	if ignored.present {
		c.IgnoreErrors = ignored.value
	}

	return nil
}

// envBool parses one boolean variable. present is false when the variable is empty.
func (*Config) envBool(getenv func(string) string, key string) (*envBoolValue, error) {
	parsed := new(envBoolValue)

	s := getenv(key)
	if s == "" {
		return parsed, nil
	}

	value, err := strconv.ParseBool(s)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", Env(key), err)
	}

	parsed.value = value
	parsed.present = true

	return parsed, nil
}

// applyDurationEnv copies timeouts. A value must include a Go unit.
func (c *Config) applyDurationEnv(getenv func(string) string) error {
	keys := []string{
		envFetchTimeout,
		envCatalogTimeout,
		envCatalogBudget,
		envProbeTimeout,
		envLocalTimeout,
		envRetryDelay,
		envCloneTimeout,
		envArchiveTimeout,
	}

	for _, key := range keys {
		if err := c.applyOneDuration(getenv, key); err != nil {
			return err
		}
	}

	return nil
}

// applyOneDuration copies one timeout when its variable is set.
func (c *Config) applyOneDuration(getenv func(string) string, key string) error {
	parsed, err := c.envDuration(getenv, key)
	if err != nil {
		return err
	}

	if !parsed.present {
		return nil
	}

	switch key {
	case envFetchTimeout:
		c.FetchTimeout = parsed.value
	case envCatalogTimeout:
		c.CatalogTimeout = parsed.value
	case envCatalogBudget:
		c.CatalogBudget = parsed.value
	case envProbeTimeout:
		c.ProbeTimeout = parsed.value
	case envLocalTimeout:
		c.LocalTimeout = parsed.value
	case envRetryDelay:
		c.RetryDelay = parsed.value
	case envCloneTimeout:
		c.CloneTimeout = parsed.value
	case envArchiveTimeout:
		c.ArchiveTimeout = parsed.value
	}

	return nil
}

// envDuration parses one duration variable. ok is false when the variable is empty.
func (*Config) envDuration(getenv func(string) string, key string) (*envDurationValue, error) {
	parsed := new(envDurationValue)

	s := getenv(key)
	if s == "" {
		return parsed, nil
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", Env(key), err)
	}

	parsed.value = d
	parsed.present = true

	return parsed, nil
}
