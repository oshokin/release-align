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

	if c.WorkspaceFile == "" && (c.Depth < 1 || c.Depth > 32) {
		return errDepthRange
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

	switch strings.ToLower(strings.TrimSpace(c.Local)) {
	case localSkip, localKeep, localReset:
		c.Local = strings.ToLower(strings.TrimSpace(c.Local))
	default:
		return fmt.Errorf("%s: %w", c.Local, errInvalidLocal)
	}

	return nil
}

// ValidateWorkspace checks flags that apply only to the explicit inventory.
// Legacy discovery ignores them. An explicit --branch override is applied by the caller.
func (c *Config) ValidateWorkspace(mode string, depthChanged, versionsChanged bool) error {
	if c.Output != outputText && c.Output != outputJSON {
		return fmt.Errorf("%s: %w", c.Output, errWorkspaceOutput)
	}

	if c.WorkspaceFile == "" {
		return c.rejectWorkspaceOnlyFlags(mode)
	}

	if mode == ModeStatus && c.DryRun {
		return errWorkspaceStatusDryRun
	}

	if versionsChanged || c.VersionsFile != "" {
		return errWorkspaceVersions
	}

	if depthChanged {
		return errWorkspaceDepth
	}

	if c.Local != localSkip {
		return errWorkspaceLocal
	}

	return nil
}

// rejectWorkspaceOnlyFlags reports selection and JSON use without an inventory file.
func (c *Config) rejectWorkspaceOnlyFlags(mode string) error {
	if mode == ModeStatus {
		return errWorkspaceRequired
	}

	if len(c.Repositories) > 0 || len(c.Groups) > 0 {
		return errWorkspaceFilter
	}

	if c.Output == outputJSON {
		return errWorkspaceJSONNeedsFile
	}

	return nil
}
