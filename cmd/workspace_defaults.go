package cmd

import (
	"errors"
	"os"

	"github.com/spf13/cobra"

	"github.com/oshokin/release-align/internal/app"
)

// baseDirChoice is one lookup of the directory that contains the clones.
type baseDirChoice struct {
	// command is the invocation whose --base-dir flag was parsed.
	command *cobra.Command
	// workspace is the YAML file that may store release-align.base-dir.
	// Empty means this command does not read a file, as with workspace init.
	workspace string
	// current is the flag value, including an explicit empty string.
	current string
	// recorded is a base-dir the caller already loaded from the workspace.
	recorded string
	// recordedSet means recorded is authoritative and the file is not read again.
	recordedSet bool
}

var (
	// errBaseDirChoice means the lookup was called without a command.
	errBaseDirChoice = errors.New("base directory choice is missing")
	// errBaseDirMissing means the flag, RELEASE_ALIGN_BASE_DIR, and the workspace file are all empty.
	errBaseDirMissing = errors.New(
		"base directory requires --base-dir, " + app.EnvPrefix + "BASE_DIR, or release-align.base-dir",
	)
)

// savedBaseDir returns the directory for this run.
// An explicit flag wins, then BASE_DIR, then release-align.base-dir in the workspace file.
func savedBaseDir(choice *baseDirChoice) (string, error) {
	if choice == nil || choice.command == nil {
		return "", errBaseDirChoice
	}

	if choice.command.Flags().Changed("base-dir") {
		if choice.current == "" {
			return "", errBaseDirMissing
		}

		return choice.current, nil
	}

	if dir := os.Getenv(app.Env("BASE_DIR")); dir != "" {
		return dir, nil
	}

	recorded, err := choice.recordedBase()
	if err != nil {
		return "", err
	}

	if recorded == "" {
		return "", errBaseDirMissing
	}

	return recorded, nil
}

// recordedBase returns the workspace base-dir, loading the file when the caller has not.
func (c *baseDirChoice) recordedBase() (string, error) {
	if c.recordedSet {
		return c.recorded, nil
	}

	if c.workspace == "" {
		return "", nil
	}

	spec, err := app.LoadWorkspace(c.workspace)
	if err != nil {
		return "", err
	}

	return spec.BaseDir, nil
}
