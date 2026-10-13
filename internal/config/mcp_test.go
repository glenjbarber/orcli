package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPServersAreReadAndUnknownMembersSurvive(t *testing.T) {
	h := home(t)
	writeConfigAt(t, h, `{"api_key":"openrouter","mcp":{"servers":[{"name":"files","command":"npx","args":["-y","server-filesystem","/tmp"],"env":{"FOO":"bar"}}],"future":"keep"}}`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	servers, err := cfg.MCPServers()
	if err != nil {
		t.Fatalf("MCPServers: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("MCPServers = %#v, want one server", servers)
	}
	got := servers[0]
	if got.Name != "files" || got.Command != "npx" {
		t.Fatalf("MCPServers[0] = %#v", got)
	}
	if len(got.Args) != 3 || got.Args[0] != "-y" {
		t.Fatalf("MCPServers[0].Args = %#v", got.Args)
	}
	if len(got.Env) != 1 || got.Env[0] != "FOO=bar" {
		t.Fatalf("MCPServers[0].Env = %#v", got.Env)
	}

	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"future":"keep"`) {
		t.Errorf("unknown mcp member was lost: %s", raw)
	}
}

// TestMCPServersReadsARemoteServer covers a server configured with a url
// instead of a command - the Streamable HTTP shape this client now supports
// (issue #62).
func TestMCPServersReadsARemoteServer(t *testing.T) {
	h := home(t)
	writeConfigAt(t, h, `{"api_key":"openrouter","mcp":{"servers":[{"name":"watchgoose","url":"https://watchgoose.example.com/mcp","headers":{"Authorization":"Bearer secret-token"}}]}}`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	servers, err := cfg.MCPServers()
	if err != nil {
		t.Fatalf("MCPServers: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("MCPServers = %#v, want one server", servers)
	}
	got := servers[0]
	if got.Name != "watchgoose" || got.URL != "https://watchgoose.example.com/mcp" {
		t.Fatalf("MCPServers[0] = %#v", got)
	}
	if got.Command != "" {
		t.Errorf("a remote server carried a command: %q", got.Command)
	}
	if got.Headers["Authorization"] != "Bearer secret-token" {
		t.Errorf("MCPServers[0].Headers = %#v", got.Headers)
	}
}

// TestMCPServersRejectBothCommandAndURL and
// TestMCPServersRejectNeitherCommandNorURL guard the mutual-exclusion rule:
// a server is local or remote, never both and never neither.
func TestMCPServersRejectBothCommandAndURL(t *testing.T) {
	h := home(t)
	writeConfigAt(t, h, `{"api_key":"openrouter","mcp":{"servers":[{"name":"files","command":"npx","url":"https://example.com/mcp"}]}}`)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, err = cfg.MCPServers()
	if err == nil || !strings.Contains(err.Error(), "both a command and a url") {
		t.Fatalf("MCPServers() = %v, want a refusal naming both a command and a url", err)
	}
}

func TestMCPServersRejectNeitherCommandNorURL(t *testing.T) {
	h := home(t)
	writeConfigAt(t, h, `{"api_key":"openrouter","mcp":{"servers":[{"name":"files"}]}}`)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, err = cfg.MCPServers()
	if err == nil || !strings.Contains(err.Error(), "neither a command nor a url") {
		t.Fatalf("MCPServers() = %v, want a refusal naming neither", err)
	}
}

func TestMCPServersMissingBlockOrEmpty(t *testing.T) {
	for _, body := range []string{
		`{"api_key":"openrouter"}`,
		`{"api_key":"openrouter","mcp":{}}`,
		`{"api_key":"openrouter","mcp":{"servers":[]}}`,
		`{"api_key":"openrouter","mcp":null}`,
	} {
		t.Run(body, func(t *testing.T) {
			h := home(t)
			writeConfigAt(t, h, body)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			servers, err := cfg.MCPServers()
			if err != nil || len(servers) != 0 {
				t.Fatalf("MCPServers = %#v, %v; want none, nil", servers, err)
			}
		})
	}
}

func TestMCPServersRejectWrongShape(t *testing.T) {
	for _, body := range []string{
		`{"api_key":"openrouter","mcp":[]}`,
		`{"api_key":"openrouter","mcp":{"servers":[{"command":"npx"}]}}`,
		`{"api_key":"openrouter","mcp":{"servers":[{"name":"files"}]}}`,
		`{"api_key":"openrouter","mcp":{"servers":[{"name":"files","command":"npx"},{"name":"files","command":"npx"}]}}`,
	} {
		t.Run(body, func(t *testing.T) {
			h := home(t)
			writeConfigAt(t, h, body)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if _, err := cfg.MCPServers(); err == nil {
				t.Fatal("MCPServers accepted malformed configuration")
			}
		})
	}
}
