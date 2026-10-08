package app

import (
	"net/url"
	"strings"
)

const (
	// cloneProtocolSSH is the default clone transport.
	cloneProtocolSSH = "ssh"
	// cloneProtocolHTTPS selects the HTTP clone URL from the API.
	cloneProtocolHTTPS = "https"
	// remoteSchemeHTTP is accepted when matching an existing origin. It is not a clone protocol.
	remoteSchemeHTTP = "http"
)

// Validate checks the GitLab source without calling the network.
// Empty clone_protocol becomes ssh. Repeated groups are collapsed.
// Groups may be empty: init can store a host before any namespace is chosen.
func (s *GitLabSource) Validate() error {
	if s == nil {
		return errGitLabSource
	}

	if err := validateGitLabURL(s.URL); err != nil {
		return err
	}

	groups, err := normalizeGitLabGroups(s.Groups)
	if err != nil {
		return err
	}

	protocol := s.CloneProtocol
	if protocol == "" {
		protocol = cloneProtocolSSH
	}

	if protocol != cloneProtocolSSH && protocol != cloneProtocolHTTPS {
		return errGitLabProtocol
	}

	s.Groups = groups
	s.CloneProtocol = protocol

	return nil
}

// ValidateForAPI checks the source before a catalog read or a clone.
func (s *GitLabSource) ValidateForAPI() error {
	if err := s.Validate(); err != nil {
		return err
	}

	if len(s.Groups) == 0 {
		return errGitLabGroups
	}

	return nil
}

// validateGitLabURL accepts an https origin and an optional port.
func validateGitLabURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != cloneProtocolHTTPS || parsed.Host == "" || parsed.User != nil {
		return errGitLabURL
	}

	if parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return errGitLabURL
	}

	if parsed.Path != "" && parsed.Path != "/" {
		return errGitLabURL
	}

	return nil
}

// normalizeGitLabGroups keeps the first copy of each exact namespace path.
// An empty list is stored as-is. A blank or non-canonical entry is rejected.
func normalizeGitLabGroups(groups []string) ([]string, error) {
	if len(groups) == 0 {
		return []string{}, nil
	}

	seen := make(map[string]bool, len(groups))
	out := make([]string, 0, len(groups))

	for _, group := range groups {
		if !canonicalProjectPath(group) || strings.ContainsAny(group, "*?[]#") {
			return nil, errGitLabGroups
		}

		if seen[group] {
			continue
		}

		seen[group] = true
		out = append(out, group)
	}

	return out, nil
}
