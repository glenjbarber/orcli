package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGitHubAPIKeyIsReadAndUnknownMembersSurvive(t *testing.T) {
	h := home(t)
	writeConfigAt(t, h, `{"api_key":"openrouter","github":{"api_key":"ghp_exampletoken123456789","future":"keep"}}`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, err := cfg.GitHubAPIKey()
	if err != nil {
		t.Fatalf("GitHubAPIKey: %v", err)
	}
	if got != "ghp_exampletoken123456789" {
		t.Fatalf("GitHubAPIKey = %q", got)
	}

	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"future":"keep"`) {
		t.Errorf("unknown GitHub member was lost: %s", raw)
	}
}

func TestGitHubAPIKeyMissingBlockOrMember(t *testing.T) {
	for _, body := range []string{
		`{"api_key":"openrouter"}`,
		`{"api_key":"openrouter","github":{}}`,
		`{"api_key":"openrouter","github":{"api_key":null}}`,
	} {
		t.Run(body, func(t *testing.T) {
			h := home(t)
			writeConfigAt(t, h, body)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			got, err := cfg.GitHubAPIKey()
			if err != nil || got != "" {
				t.Fatalf("GitHubAPIKey = %q, %v; want empty, nil", got, err)
			}
		})
	}
}

func TestGitHubAPIKeyRejectsWrongShape(t *testing.T) {
	for _, body := range []string{
		`{"api_key":"openrouter","github":[]}`,
		`{"api_key":"openrouter","github":{"api_key":42}}`,
	} {
		t.Run(body, func(t *testing.T) {
			h := home(t)
			writeConfigAt(t, h, body)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if _, err := cfg.GitHubAPIKey(); err == nil {
				t.Fatal("GitHubAPIKey accepted malformed configuration")
			}
		})
	}
}

func TestGitHubSettingsRequireAnExplicitRepositoryAllowlist(t *testing.T) {
	h := home(t)
	writeConfigAt(t, h, `{"api_key":"openrouter","github":{"api_key":"ghp_test","login":"glenjbarber","repositories":["glenjbarber/orcli"]}}`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	key, login, repositories, err := cfg.GitHubSettings()
	if err != nil {
		t.Fatal(err)
	}
	if key != "ghp_test" || login != "glenjbarber" || len(repositories) != 1 || repositories[0] != "glenjbarber/orcli" {
		t.Fatalf("GitHubSettings = (%q, %q, %#v), want configured account and repository", key, login, repositories)
	}
}

func TestGitHubSettingsRejectInvalidOrUnscopedRepositories(t *testing.T) {
	for _, settings := range []string{
		`{"api_key":"token","login":"glenjbarber"}`,
		`{"api_key":"token","login":"glenjbarber","repositories":["owner/repo/other"]}`,
		`{"api_key":"token","login":"glenjbarber","repositories":["../repo"]}`,
		`{"api_key":"token","login":"glenjbarber","repositories":["owner/repo","owner/repo"]}`,
	} {
		t.Run(settings, func(t *testing.T) {
			h := home(t)
			writeConfigAt(t, h, `{"api_key":"openrouter","github":`+settings+`}`)
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := cfg.GitHubSettings(); err == nil {
				t.Fatal("GitHubSettings accepted an invalid or unscoped repository list")
			}
		})
	}
}
