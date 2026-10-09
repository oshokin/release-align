package gitter

import (
	"net/url"
	"regexp"
	"strings"
)

// diagnosticURL finds URLs in Git text so userinfo can be removed.
var diagnosticURL = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://\S+`)

// RedactText removes userinfo and credential query values from Git text.
// Newlines become one line so a later stderr line stays on the same log record.
func RedactText(text string) string {
	return diagnosticURL.ReplaceAllStringFunc(oneLine(text), redactOneURL)
}

// oneLine joins wrapped Git text. Empty lines are dropped.
func oneLine(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	parts := strings.Split(text, "\n")
	kept := make([]string, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		kept = append(kept, part)
	}

	return strings.Join(kept, "; ")
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
