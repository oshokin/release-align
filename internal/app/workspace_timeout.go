package app

import (
	"fmt"
	"time"
)

// TimeoutLocks records durations already chosen by a flag or an environment variable.
type TimeoutLocks struct {
	// Probe is set when probe timeout must not come from the workspace file.
	Probe bool
	// Fetch is set when fetch timeout must not come from the workspace file.
	Fetch bool
	// Local is set when local timeout must not come from the workspace file.
	Local bool
	// Clone is set when clone timeout must not come from the workspace file.
	Clone bool
	// Archive is set when archive timeout must not come from the workspace file.
	Archive bool
}

// WorkspaceTimeouts contains optional duration overrides from the workspace.
type WorkspaceTimeouts struct {
	// Probe limits a reachability attempt.
	Probe *string `json:"probe,omitempty"`
	// Fetch limits a fetch or the complete GitLab inventory request.
	Fetch *string `json:"fetch,omitempty"`
	// Local limits one short local Git command.
	Local *string `json:"local,omitempty"`
	// Clone limits one repository clone.
	Clone *string `json:"clone,omitempty"`
	// Archive limits one repository export, including its ZIP entry copy.
	Archive *string `json:"archive,omitempty"`
}

type timeoutField struct {
	// name is the JSON field name.
	name string
	// value is the raw duration. Nil means the field was omitted.
	value *string
}

// SetTimeoutLocks stores which durations a command already resolved.
func (c *Config) SetTimeoutLocks(locks *TimeoutLocks) {
	if c == nil {
		return
	}

	c.timeoutLocks = locks
}

// ApplySpecTimeouts copies unlocked workspace overrides onto cfg.
func ApplySpecTimeouts(cfg *Config, spec *WorkspaceSpec) error {
	if cfg == nil || spec == nil || spec.Timeouts == nil {
		return nil
	}

	locks := cfg.timeoutLocks
	if locks == nil {
		locks = new(TimeoutLocks)
	}

	probe, err := unlockedDuration(locks.Probe, "probe", spec.Timeouts.Probe, cfg.ProbeTimeout)
	if err != nil {
		return err
	}

	fetch, err := unlockedDuration(locks.Fetch, "fetch", spec.Timeouts.Fetch, cfg.FetchTimeout)
	if err != nil {
		return err
	}

	local, err := unlockedDuration(locks.Local, "local", spec.Timeouts.Local, cfg.LocalTimeout)
	if err != nil {
		return err
	}

	clone, err := unlockedDuration(locks.Clone, "clone", spec.Timeouts.Clone, cfg.CloneTimeout)
	if err != nil {
		return err
	}

	archive, err := unlockedDuration(locks.Archive, "archive", spec.Timeouts.Archive, cfg.ArchiveTimeout)
	if err != nil {
		return err
	}

	cfg.ProbeTimeout = probe
	cfg.FetchTimeout = fetch
	cfg.LocalTimeout = local
	cfg.CloneTimeout = clone
	cfg.ArchiveTimeout = archive

	return nil
}

// applyCloneTimeouts copies unlocked workspace overrides onto clone options.
func applyCloneTimeouts(opts *WorkspaceCloneOptions, spec *WorkspaceSpec) error {
	if opts == nil || spec == nil || spec.Timeouts == nil {
		return nil
	}

	locks := opts.timeoutLocks
	if locks == nil {
		locks = new(TimeoutLocks)
	}

	probe, err := unlockedDuration(locks.Probe, "probe", spec.Timeouts.Probe, opts.ProbeTimeout)
	if err != nil {
		return err
	}

	fetch, err := unlockedDuration(locks.Fetch, "fetch", spec.Timeouts.Fetch, opts.FetchTimeout)
	if err != nil {
		return err
	}

	local, err := unlockedDuration(locks.Local, "local", spec.Timeouts.Local, opts.LocalTimeout)
	if err != nil {
		return err
	}

	clone, err := unlockedDuration(locks.Clone, "clone", spec.Timeouts.Clone, opts.CloneTimeout)
	if err != nil {
		return err
	}

	opts.ProbeTimeout = probe
	opts.FetchTimeout = fetch
	opts.LocalTimeout = local
	opts.CloneTimeout = clone

	return nil
}

// applyArchiveTimeouts copies unlocked workspace overrides onto archive options.
func applyArchiveTimeouts(opts *WorkspaceArchiveOptions, spec *WorkspaceSpec) error {
	if opts == nil || spec == nil || spec.Timeouts == nil {
		return nil
	}

	locks := opts.timeoutLocks
	if locks == nil {
		locks = new(TimeoutLocks)
	}

	local, err := unlockedDuration(locks.Local, "local", spec.Timeouts.Local, opts.LocalTimeout)
	if err != nil {
		return err
	}

	archive, err := unlockedDuration(locks.Archive, "archive", spec.Timeouts.Archive, opts.ArchiveTimeout)
	if err != nil {
		return err
	}

	opts.LocalTimeout = local
	opts.ArchiveTimeout = archive

	return nil
}

// Validate rejects an empty, zero, negative, or unparsable override.
func (t *WorkspaceTimeouts) Validate() error {
	if t == nil {
		return nil
	}

	fields := []*timeoutField{
		{name: "probe", value: t.Probe},
		{name: "fetch", value: t.Fetch},
		{name: "local", value: t.Local},
		{name: "clone", value: t.Clone},
		{name: "archive", value: t.Archive},
	}

	for _, field := range fields {
		if _, err := field.duration(); err != nil {
			return err
		}
	}

	return nil
}

// clone returns an independent timeout block.
func (t *WorkspaceTimeouts) clone() *WorkspaceTimeouts {
	if t == nil {
		return nil
	}

	return &WorkspaceTimeouts{
		Probe:   cloneTimeoutString(t.Probe),
		Fetch:   cloneTimeoutString(t.Fetch),
		Local:   cloneTimeoutString(t.Local),
		Clone:   cloneTimeoutString(t.Clone),
		Archive: cloneTimeoutString(t.Archive),
	}
}

// duration parses one override. A nil value is inherited.
func (f *timeoutField) duration() (time.Duration, error) {
	if f == nil || f.value == nil {
		return 0, nil
	}

	parsed, err := time.ParseDuration(*f.value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%w: %s", errWorkspaceTimeout, f.name)
	}

	return parsed, nil
}

// unlockedDuration keeps current when the field is locked or omitted.
func unlockedDuration(locked bool, name string, raw *string, current time.Duration) (time.Duration, error) {
	if locked || raw == nil {
		return current, nil
	}

	field := &timeoutField{
		name:  name,
		value: raw,
	}

	return field.duration()
}

// cloneTimeoutString copies one optional duration string.
func cloneTimeoutString(raw *string) *string {
	if raw == nil {
		return nil
	}

	value := *raw

	return &value
}
