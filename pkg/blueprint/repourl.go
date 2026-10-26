package blueprint

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

// parseRepoURL validates a Git repository URL and normalizes SSH URLs to HTTPS.
func parseRepoURL(repo string) (*url.URL, error) {
	u, err := url.Parse(repo)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "ssh") {
		return nil, fmt.Errorf("repoUrl must be an HTTPS or SSH repository URL starting with https:// or ssh://")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("repoUrl must be a repository URL without a query or fragment; specify the branch with branch")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	name := strings.TrimSuffix(path.Base(u.Path), ".git")
	if name == "" || name == "." || name == "/" {
		return nil, fmt.Errorf("repoUrl must include a repository name")
	}
	if u.Scheme == "ssh" {
		u.Scheme = "https"
		u.Host = u.Hostname()
		u.User = nil
	}
	return u, nil
}
