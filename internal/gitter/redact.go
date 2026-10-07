package gitter

import (
	"net/url"
	"regexp"
	"strings"
)

var diagnosticURL = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://\S+`)

// RedactText removes userinfo and credential query values from Git text.
func RedactText(text string) string {
	return diagnosticURL.ReplaceAllStringFunc(text, redactOneURL)
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
