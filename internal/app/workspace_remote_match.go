package app

import (
	"net/url"
	"path/filepath"
	"strings"
)

// remoteKey is the host and namespace used to match a clone to a GitLab project.
type remoteKey struct {
	// host is the lowercased host, plus a non-default port when one was present.
	host string
	// path is the namespace path with its original case. A trailing .git is removed.
	path string
}

// parseRemote reduces a Git remote to a host and a namespace path.
// SSH user is ignored. Host case is ignored. The namespace case is kept.
func parseRemote(raw string) (*remoteKey, bool) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, "/")

	if raw == "" || strings.HasPrefix(raw, "file:") || filepath.IsAbs(raw) {
		return nil, false
	}

	if strings.Contains(raw, "://") {
		return parseRemoteURL(raw)
	}

	return parseSCPRemote(raw)
}

// parseRemoteURL reads ssh and http(s) URLs. A non-default port stays in the host.
func parseRemoteURL(raw string) (*remoteKey, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return nil, false
	}

	scheme := strings.ToLower(parsed.Scheme)

	if scheme != cloneProtocolSSH && scheme != cloneProtocolHTTPS && scheme != remoteSchemeHTTP {
		return nil, false
	}

	host := strings.ToLower(parsed.Hostname())
	if port := parsed.Port(); port != "" && !defaultRemotePort(scheme, port) {
		host = host + ":" + port
	}

	repoPath := strings.Trim(parsed.Path, "/")
	repoPath = strings.TrimSuffix(repoPath, ".git")

	if repoPath == "" || strings.Contains(repoPath, pathDotDot) {
		return nil, false
	}

	key := &remoteKey{
		host: host,
		path: repoPath,
	}

	return key, true
}

// parseSCPRemote reads git@host:namespace/project.git.
func parseSCPRemote(raw string) (*remoteKey, bool) {
	host, repoPath, ok := strings.Cut(raw, ":")
	if !ok || repoPath == "" || strings.Contains(repoPath, "://") {
		return nil, false
	}

	if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:]
	}

	repoPath = strings.TrimSuffix(strings.Trim(repoPath, "/"), ".git")

	if host == "" || repoPath == "" || strings.Contains(repoPath, pathDotDot) {
		return nil, false
	}

	key := &remoteKey{
		host: strings.ToLower(host),
		path: repoPath,
	}

	return key, true
}

// remoteID is the map identity of a parsed remote. Nil has no identity.
func remoteID(key *remoteKey) string {
	if key == nil {
		return ""
	}

	return key.host + "\x00" + key.path
}

// defaultRemotePort reports the usual port for a scheme.
func defaultRemotePort(scheme, port string) bool {
	switch scheme {
	case cloneProtocolHTTPS:
		return port == "443"
	case remoteSchemeHTTP:
		return port == "80"
	case cloneProtocolSSH:
		return port == "22"
	default:
		return false
	}
}

// productionCloneURL reports a URL that production clone may use for protocol.
func productionCloneURL(raw, protocol string) bool {
	if _, ok := parseRemote(raw); !ok {
		return false
	}

	if !strings.Contains(raw, "://") {
		return protocol == cloneProtocolSSH
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}

	scheme := strings.ToLower(parsed.Scheme)
	if protocol == cloneProtocolHTTPS {
		return scheme == cloneProtocolHTTPS
	}

	return scheme == cloneProtocolSSH
}

// localCloneURL reports a filesystem path allowed only for tests.
func localCloneURL(raw string) bool {
	return filepath.IsAbs(raw) || strings.HasPrefix(strings.TrimSpace(raw), "file://")
}

// cleanLocal returns a cleaned absolute path, or an empty string.
func cleanLocal(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "file://")

	if !filepath.IsAbs(raw) {
		return ""
	}

	return filepath.Clean(raw)
}

// gitlabScope reports whether path belongs to one configured GitLab group.
func gitlabScope(projectPath string, groups []string) bool {
	for _, group := range groups {
		if projectPath == group || strings.HasPrefix(projectPath, group+"/") {
			return true
		}
	}

	return false
}
