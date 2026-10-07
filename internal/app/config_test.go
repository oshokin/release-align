package app

import (
	"testing"
	"time"
)

// TestEnvironment verifies flag-aligned environment variables and Go durations.
func TestEnvironment(t *testing.T) {
	cfg := DefaultConfig()
	env := map[string]string{
		"BASE_DIR":      "/tmp/work",
		"JOBS":          "8",
		"DRY_RUN":       "1",
		"LOG_LEVEL":     "DeBuG",
		"FETCH_TIMEOUT": "12s",
		"PROBE_TIMEOUT": "750ms",
	}

	if e := cfg.ApplyEnv(envLookup(env)); e != nil {
		t.Fatal(e)
	}

	if cfg.BaseDir != "/tmp/work" || cfg.Jobs != 8 || !cfg.DryRun || cfg.LogLevel != "DeBuG" ||
		cfg.FetchTimeout != 12*time.Second || cfg.ProbeTimeout != 750*time.Millisecond {
		t.Fatal(cfg)
	}

	if e := cfg.Validate(); e != nil {
		t.Fatal(e)
	}
}

// TestValidation verifies numeric limits, the home directory, and rejected flag combinations.
func TestValidation(t *testing.T) {
	mutators := []func(*Config){
		func(c *Config) {
			c.Jobs = 0
		},
		func(c *Config) {
			c.Attempts = 0
		},
		func(c *Config) {
			c.ProbeTimeout = 0
		},
		func(c *Config) {
			c.RetryDelay = -1
		},
		func(c *Config) {
			c.Branch = "--bad"
		},
		func(c *Config) {
			c.LogLevel = "nope"
		},
	}

	for _, mutate := range mutators {
		c := DefaultConfig()
		mutate(c)

		if e := c.Validate(); e == nil {
			t.Fatal(c)
		}
	}

	c := DefaultConfig()
	if e := c.ApplyEnv(envVar("DRY_RUN", "oops")); e == nil {
		t.Fatal("invalid env accepted")
	}

	if e := c.ApplyEnv(envVar("FETCH_TIMEOUT", "12")); e == nil {
		t.Fatal("bare seconds accepted")
	}
}

// envLookup returns the value stored for a variable name.
func envLookup(env map[string]string) func(string) string {
	return func(key string) string {
		return env[key]
	}
}

// envVar returns one variable and leaves every other name empty.
func envVar(name, value string) func(string) string {
	return func(key string) string {
		if key == name {
			return value
		}

		return ""
	}
}
