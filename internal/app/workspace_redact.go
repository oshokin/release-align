package app

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"

	"github.com/oshokin/release-align/internal/gitter"
)

var workspaceURL = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://\S+`)

// redactGitText removes userinfo and credential query values from Git text.
func redactGitText(text string) string {
	return workspaceURL.ReplaceAllStringFunc(text, redactOneURL)
}

// redactOneURL strips userinfo and secret query parameters from one URL.
func redactOneURL(raw string) string {
	trimmed := strings.TrimRight(raw, ".,);]")
	suffix := raw[len(trimmed):]

	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return "[redacted-url]" + suffix
	}

	if parsed.User != nil {
		parsed.User = url.User("redacted")
	}

	query := parsed.Query()
	for key := range query {
		if secretQueryKey(key) {
			query.Set(key, "redacted")
		}
	}

	parsed.RawQuery = query.Encode()

	return parsed.String() + suffix
}

// secretQueryKey reports query keys that can carry a credential.
func secretQueryKey(key string) bool {
	switch strings.ToLower(key) {
	case "token", "access_token", "private_token", "password", "oauth_token", "secret":
		return true
	default:
		return false
	}
}

// workspaceGitReason maps a Git error onto a reason code and a redacted message.
func (r *runner) workspaceGitReason(err error) (string, string) {
	if err == nil {
		return "", ""
	}

	if errors.Is(err, context.Canceled) {
		return reasonCanceled, messageRunStopped
	}

	message := redactGitText(err.Error())
	lower := strings.ToLower(err.Error())

	switch {
	case gitter.NetworkError(err) || strings.Contains(lower, "context deadline exceeded") || strings.Contains(lower, "origin unreachable"):
		return reasonNetwork, message
	case r.authGitError(lower):
		return reasonAuth, message + "; check access with: git ls-remote origin HEAD"
	default:
		return reasonGitFailed, message
	}
}

// authGitError reports authentication and permission failures.
func (*runner) authGitError(lower string) bool {
	markers := []string{
		"authentication failed",
		"permission denied (publickey)",
		"http basic: access denied",
		"invalid username or token",
		"could not read username",
	}

	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}

	return false
}
