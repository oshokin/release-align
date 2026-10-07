package app

import (
	"net/url"
	"path/filepath"
	"strings"

	"github.com/oshokin/release-align/internal/retry"
)

// useProbeRetries attaches the origin-check retry engine when more than one attempt is allowed.
func (r *runner) useProbeRetries() error {
	if r.cfg.Attempts <= 1 {
		return nil
	}

	engineCfg := &retry.EngineConfig{
		MaxRetries:  uint64(r.cfg.Attempts - 1),
		DelayPolicy: retry.NewRandomRangePolicy(r.cfg.RetryDelay, r.cfg.RetryDelay),
		IsRetryable: r.retryableProbe,
	}

	engine, err := retry.NewEngine(engineCfg)
	if err != nil {
		return err
	}

	r.probeRetry = engine

	return nil
}

// endpoint reduces an origin URL to the host used for one preflight check.
func (r *runner) endpoint(remote string) string {
	if u, err := url.Parse(remote); err == nil && u.Host != "" {
		return u.Scheme + "://" + u.Host
	}

	if !filepath.IsAbs(remote) && strings.Contains(remote, ":") {
		return "ssh:" + strings.SplitN(remote, ":", 2)[0]
	}

	return "local:" + remote
}
