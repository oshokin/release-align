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

const (
	// EnvPrefix marks release-align settings in the process environment.
	// GITLAB_TOKEN stays unprefixed: it is a container credential, not a program setting.
	EnvPrefix = "RELEASE_ALIGN_"
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
	parsed := &envIntValue{}

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

// applyBoolEnv copies DRY_RUN when the variable is set.
func (c *Config) applyBoolEnv(getenv func(string) string) error {
	key := "DRY_RUN"

	s := getenv(key)
	if s == "" {
		return nil
	}

	b, err := strconv.ParseBool(s)
	if err != nil {
		return fmt.Errorf("%s: %w", Env(key), err)
	}

	c.DryRun = b

	return nil
}

// applyDurationEnv copies timeouts. A value must include a Go unit.
func (c *Config) applyDurationEnv(getenv func(string) string) error {
	fetch, err := c.envDuration(getenv, "FETCH_TIMEOUT")
	if err != nil {
		return err
	}

	if fetch.present {
		c.FetchTimeout = fetch.value
	}

	probe, err := c.envDuration(getenv, "PROBE_TIMEOUT")
	if err != nil {
		return err
	}

	if probe.present {
		c.ProbeTimeout = probe.value
	}

	local, err := c.envDuration(getenv, "LOCAL_TIMEOUT")
	if err != nil {
		return err
	}

	if local.present {
		c.LocalTimeout = local.value
	}

	delay, err := c.envDuration(getenv, "RETRY_DELAY")
	if err != nil {
		return err
	}

	if delay.present {
		c.RetryDelay = delay.value
	}

	clone, err := c.envDuration(getenv, "CLONE_TIMEOUT")
	if err != nil {
		return err
	}

	if clone.present {
		c.CloneTimeout = clone.value
	}

	archive, err := c.envDuration(getenv, "ARCHIVE_TIMEOUT")
	if err != nil {
		return err
	}

	if archive.present {
		c.ArchiveTimeout = archive.value
	}

	return nil
}

// envDuration parses one duration variable. ok is false when the variable is empty.
func (*Config) envDuration(getenv func(string) string, key string) (*envDurationValue, error) {
	parsed := &envDurationValue{}

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
