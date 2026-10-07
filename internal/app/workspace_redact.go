package app

import (
	"context"
	"errors"
	"strings"

	"github.com/oshokin/release-align/internal/gitter"
)

// redactGitText shares diagnostic redaction with the Git subprocess boundary.
func redactGitText(text string) string { return gitter.RedactText(text) }

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
