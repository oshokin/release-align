package app

import (
	"testing"
	"time"
)

func TestLegacyEnvironment(t *testing.T) {
	cfg := DefaultConfig()
	env := map[string]string{
		"BASE_DIR":         "/tmp/work",
		"RELEASE_BRANCH":   "release",
		"MAX_DEPTH":        "3",
		"JOBS":             "8",
		"DRY_RUN":          "1",
		"LOCAL":            "ReSeT",
		"LOG_LEVEL":        "DeBuG",
		"FETCH_TIMEOUT":    "12",
		"LSREMOTE_TIMEOUT": "750ms",
	}

	if e := cfg.ApplyEnv(func(s string) string {
		return env[s]
	}); e != nil {
		t.Fatal(e)
	}

	if cfg.Depth != 3 || cfg.Jobs != 8 || !cfg.DryRun || cfg.Local != "ReSeT" ||
		cfg.LogLevel != "DeBuG" || cfg.FetchTimeout != 12*time.Second || cfg.ProbeTimeout != 750*time.Millisecond {
		t.Fatal(cfg)
	}

	if e := cfg.Validate(); e != nil {
		t.Fatal(e)
	}

	if cfg.Local != localReset {
		t.Fatal(cfg.Local)
	}
}

func TestValidation(t *testing.T) {
	for _, mutate := range []func(*Config){
		func(c *Config) {
			c.Jobs = 0
		},
		func(c *Config) {
			c.Depth = 0
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
		func(c *Config) {
			c.Local = "sideways"
		},
	} {
		c := DefaultConfig()
		mutate(c)

		if e := c.Validate(); e == nil {
			t.Fatal(c)
		}
	}

	c := DefaultConfig()
	if e := c.ApplyEnv(func(s string) string {
		if s == "DRY_RUN" {
			return "oops"
		}

		return ""
	}); e == nil {
		t.Fatal("invalid env accepted")
	}
}
