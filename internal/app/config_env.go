package app

import (
	"fmt"
	"strconv"
	"time"
)

// ApplyEnv copies settings whose names match the flags.
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

// applyDurationEnv copies timeouts. A value must include a Go unit.
func (c *Config) applyDurationEnv(getenv func(string) string) error {
	fetch, ok, err := c.envDuration(getenv, "FETCH_TIMEOUT")
	if err != nil {
		return err
	}

	if ok {
		c.FetchTimeout = fetch
	}

	probe, ok, err := c.envDuration(getenv, "PROBE_TIMEOUT")
	if err != nil {
		return err
	}

	if ok {
		c.ProbeTimeout = probe
	}

	local, ok, err := c.envDuration(getenv, "LOCAL_TIMEOUT")
	if err != nil {
		return err
	}

	if ok {
		c.LocalTimeout = local
	}

	delay, ok, err := c.envDuration(getenv, "RETRY_DELAY")
	if err != nil {
		return err
	}

	if ok {
		c.RetryDelay = delay
	}

	clone, ok, err := c.envDuration(getenv, "CLONE_TIMEOUT")
	if err != nil {
		return err
	}

	if ok {
		c.CloneTimeout = clone
	}

	archive, ok, err := c.envDuration(getenv, "ARCHIVE_TIMEOUT")
	if err != nil {
		return err
	}

	if ok {
		c.ArchiveTimeout = archive
	}

	return nil
}

// envDuration parses one duration variable. ok is false when the variable is empty.
func (*Config) envDuration(getenv func(string) string, key string) (time.Duration, bool, error) {
	s := getenv(key)
	if s == "" {
		return 0, false, nil
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, false, fmt.Errorf("%s: %w", key, err)
	}

	return d, true, nil
}
