package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

const githubKey = "github"

// GitHub preserves its original bytes so unknown settings survive a config round trip.
type GitHub struct{ raw json.RawMessage }

func (g *GitHub) UnmarshalJSON(data []byte) error {
	g.raw = append(json.RawMessage(nil), data...)
	return nil
}

func (g GitHub) MarshalJSON() ([]byte, error) {
	if len(g.raw) == 0 {
		return []byte("null"), nil
	}
	return g.raw, nil
}

// GitHubAPIKey returns the configured GitHub API key. This only reads the
// credential; it does not enable GitHub API requests by itself.
func (c Config) GitHubAPIKey() (string, error) {
	if c.GitHub == nil || isNull(c.GitHub.raw) {
		return "", nil
	}
	if !isObject(c.GitHub.raw) {
		return "", fmt.Errorf("config: %s: %w", githubKey, ErrNotAnObject)
	}
	var block map[string]json.RawMessage
	if err := json.Unmarshal(c.GitHub.raw, &block); err != nil {
		return "", fmt.Errorf("config: %s: %w", githubKey, err)
	}
	member, ok := block["api_key"]
	if !ok || isNull(member) {
		return "", nil
	}
	var apiKey string
	if err := json.Unmarshal(member, &apiKey); err != nil {
		return "", fmt.Errorf("config: %s: api_key: %w", githubKey, err)
	}
	return apiKey, nil
}

// GitHubSettings returns the credential, account name, and explicitly allowed
// repositories for task operations. An empty repository list grants no task
// access, even when a credential is present.
func (c Config) GitHubSettings() (apiKey, login string, repositories []string, err error) {
	if c.GitHub == nil || isNull(c.GitHub.raw) {
		return "", "", nil, nil
	}
	if !isObject(c.GitHub.raw) {
		return "", "", nil, fmt.Errorf("config: %s: %w", githubKey, ErrNotAnObject)
	}
	var block map[string]json.RawMessage
	if err = json.Unmarshal(c.GitHub.raw, &block); err != nil {
		return "", "", nil, fmt.Errorf("config: %s: %w", githubKey, err)
	}
	if raw, ok := block["api_key"]; ok && !isNull(raw) {
		if err = json.Unmarshal(raw, &apiKey); err != nil {
			return "", "", nil, fmt.Errorf("config: %s: api_key must be a string", githubKey)
		}
	}
	if raw, ok := block["login"]; ok && !isNull(raw) {
		if err = json.Unmarshal(raw, &login); err != nil {
			return "", "", nil, fmt.Errorf("config: %s: login must be a string", githubKey)
		}
	}
	if raw, ok := block["repositories"]; ok && !isNull(raw) {
		if err = json.Unmarshal(raw, &repositories); err != nil {
			return "", "", nil, fmt.Errorf("config: %s: repositories must be an array of owner/repo strings", githubKey)
		}
	}
	if apiKey != "" && (strings.TrimSpace(login) == "" || len(repositories) == 0) {
		return "", "", nil, fmt.Errorf("config: %s task access requires login and an explicit non-empty repositories allowlist", githubKey)
	}
	seen := make(map[string]struct{}, len(repositories))
	for _, repository := range repositories {
		if !validGitHubRepository(repository) {
			return "", "", nil, fmt.Errorf("config: %s: invalid repository %q; use owner/repo", githubKey, repository)
		}
		if _, exists := seen[repository]; exists {
			return "", "", nil, fmt.Errorf("config: %s: repository %q is listed more than once", githubKey, repository)
		}
		seen[repository] = struct{}{}
	}
	return apiKey, login, repositories, nil
}

func validGitHubRepository(repository string) bool {
	if len(repository) > 201 || strings.Count(repository, "/") != 1 || strings.TrimSpace(repository) != repository {
		return false
	}
	owner, name, _ := strings.Cut(repository, "/")
	return validGitHubSegment(owner) && validGitHubSegment(name)
}

func validGitHubSegment(segment string) bool {
	if segment == "" || segment == "." || segment == ".." {
		return false
	}
	for _, r := range segment {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func readGitHub(raw json.RawMessage) (*GitHub, error) {
	if isNull(raw) || len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	return &GitHub{raw: append(json.RawMessage(nil), raw...)}, nil
}
