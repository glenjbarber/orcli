package config

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func readGitHub(raw json.RawMessage) (*GitHub, error) {
	if isNull(raw) || len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	return &GitHub{raw: append(json.RawMessage(nil), raw...)}, nil
}
