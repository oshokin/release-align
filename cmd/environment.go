package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/oshokin/release-align/internal/app"
)

// environmentFlags maps setting names to Cobra flags.
// The process variable is RELEASE_ALIGN_ plus the map key. Cobra does not read the environment.
var environmentFlags = map[string]string{
	"BASE_DIR":        "base-dir",
	"JOBS":            "jobs",
	"ATTEMPTS":        "attempts",
	"DRY_RUN":         "dry-run",
	"LOG_LEVEL":       "log-level",
	"FETCH_TIMEOUT":   "fetch-timeout",
	"PROBE_TIMEOUT":   "probe-timeout",
	"LOCAL_TIMEOUT":   "local-timeout",
	"RETRY_DELAY":     "retry-delay",
	"CLONE_TIMEOUT":   "clone-timeout",
	"ARCHIVE_TIMEOUT": "archive-timeout",
}

// applyCommandEnv reads settings after Cobra has parsed explicit flags.
// A flag that was present on the command line is not also read from the environment.
func applyCommandEnv(command *cobra.Command, cfg *app.Config) error {
	getenv := func(key string) string {
		flag := environmentFlags[key]
		if command.Flags().Lookup(flag) == nil || command.Flags().Changed(flag) {
			return ""
		}

		return os.Getenv(app.Env(key))
	}

	return cfg.ApplyEnv(getenv)
}

// timeoutLocks reports durations this command already took from a flag or the environment.
func timeoutLocks(command *cobra.Command) *app.TimeoutLocks {
	return &app.TimeoutLocks{
		Probe:   durationLocked(command, "probe-timeout", "PROBE_TIMEOUT"),
		Fetch:   durationLocked(command, "fetch-timeout", "FETCH_TIMEOUT"),
		Local:   durationLocked(command, "local-timeout", "LOCAL_TIMEOUT"),
		Clone:   durationLocked(command, "clone-timeout", "CLONE_TIMEOUT"),
		Archive: durationLocked(command, "archive-timeout", "ARCHIVE_TIMEOUT"),
	}
}

// durationLocked reports that a flag or its RELEASE_ALIGN_ variable already chose the value.
func durationLocked(command *cobra.Command, flag, name string) bool {
	if command.Flags().Lookup(flag) == nil {
		return false
	}

	if command.Flags().Changed(flag) {
		return true
	}

	return os.Getenv(app.Env(name)) != ""
}
