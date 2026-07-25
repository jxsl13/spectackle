package forge

import (
	"fmt"
	"net/url"
	"strings"
)

// ParseRemote extracts owner/repo from a git remote URL. It accepts the two
// forms `git remote get-url origin` actually produces: HTTPS
// (https://host/owner/repo, with or without a trailing .git) and the scp-
// like SSH form (git@host:owner/repo, with or without a trailing .git).
// Anything else is rejected rather than guessed at — a wrong owner/repo
// silently talks to the wrong repository's API.
func ParseRemote(remote string) (owner, repo string, err error) {
	trimmed := strings.TrimSuffix(strings.TrimSpace(remote), ".git")

	var path string
	switch {
	case strings.HasPrefix(trimmed, "https://") || strings.HasPrefix(trimmed, "http://"):
		u, perr := url.Parse(trimmed)
		if perr != nil {
			return "", "", fmt.Errorf("forge: parse remote %q: %w", remote, perr)
		}
		path = strings.Trim(u.Path, "/")
	case strings.HasPrefix(trimmed, "git@"):
		// scp-like syntax has no URL scheme to parse: host and path are
		// split on the first colon (git@host:owner/repo).
		idx := strings.Index(trimmed, ":")
		if idx < 0 {
			return "", "", fmt.Errorf("forge: unrecognized remote %q", remote)
		}
		path = strings.Trim(trimmed[idx+1:], "/")
	default:
		return "", "", fmt.Errorf("forge: unrecognized remote form %q", remote)
	}

	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("forge: cannot extract owner/repo from %q", remote)
	}
	return parts[0], parts[1], nil
}
