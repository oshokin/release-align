package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Build metadata is injected through -ldflags. It belongs to the CLI, not app data.
var (
	// Version is the release version. The default is dev.
	Version = "dev"
	// Commit is the source revision. The default is unknown.
	Commit = "unknown"
	// BuildTime is the build timestamp. The default is unknown.
	BuildTime = "unknown"
)

// fullVersion formats the version line printed by the version command.
func fullVersion() string {
	return fmt.Sprintf("release-align %s (commit %s, built %s)", Version, Commit, BuildTime)
}

// newVersionCommand builds the version subcommand.
func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version and build metadata",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), fullVersion())
			return err
		},
	}
}
