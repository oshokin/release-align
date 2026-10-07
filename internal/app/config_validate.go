package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/oshokin/release-align/internal/logger"
)

// Validate checks numeric limits and expands a leading ~ in the base directory.
func (c *Config) Validate() error {
	if c.BaseDir == "" {
		return errBaseDirEmpty
	}

	if err := c.expandHomeWhenNeeded(); err != nil {
		return err
	}

	p, err := filepath.Abs(c.BaseDir)
	if err != nil {
		return err
	}

	c.BaseDir = p

	if c.Output == "" {
		c.Output = outputText
	}

	if c.Output != outputText && c.Output != outputJSON {
		return fmt.Errorf("%s: %w", c.Output, errWorkspaceOutput)
	}

	if c.Jobs < 1 || c.Jobs > 64 {
		return errJobsRange
	}

	if c.Attempts < 1 || c.Attempts > 10 {
		return errAttemptsRange
	}

	if c.ProbeTimeout <= 0 || c.FetchTimeout <= 0 || c.LocalTimeout <= 0 || c.RetryDelay < 0 {
		return errTimeoutRange
	}

	if c.Branch == "" || strings.HasPrefix(c.Branch, "-") {
		return errInvalidBranch
	}

	if _, ok := logger.ParseLogLevel(c.LogLevel); !ok {
		return fmt.Errorf("%s: %w", c.LogLevel, errInvalidLogLevel)
	}

	return nil
}

// ValidateWorkspace checks the inventory file and flags that apply only to sync or status.
// An explicit --branch override is applied by the caller.
func (c *Config) ValidateWorkspace(mode string) error {
	if c.Output != outputText && c.Output != outputJSON {
		return fmt.Errorf("%s: %w", c.Output, errWorkspaceOutput)
	}

	if c.WorkspaceFile == "" {
		return errWorkspaceRequired
	}

	if mode == ModeStatus && c.DryRun {
		return errWorkspaceStatusDryRun
	}

	return nil
}
