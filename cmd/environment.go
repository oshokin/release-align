package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/oshokin/release-align/internal/app"
)

// environmentFlags maps RELEASE_ALIGN_* suffixes to Cobra flag names.
var environmentFlags = map[string]string{
	"BASE_DIR":      "base-dir",
	"JOBS":          "jobs",
	"ATTEMPTS":      "attempts",
	"DRY_RUN":       "dry-run",
	"LOG_LEVEL":     "log-level",
	"FETCH_TIMEOUT": "fetch-timeout",
	"PROBE_TIMEOUT": "probe-timeout",
	"LOCAL_TIMEOUT": "local-timeout",
	"RETRY_DELAY":   "retry-delay",
}

// applyCommandEnv reads settings after Cobra has parsed explicit flags.
// A flag that was present on the command line is not also read from the environment.
func applyCommandEnv(command *cobra.Command, cfg *app.Config) error {
	getenv := func(key string) string {
		if command.Flags().Changed(environmentFlags[key]) {
			return ""
		}

		return os.Getenv(key)
	}

	return cfg.ApplyEnv(getenv)
}
