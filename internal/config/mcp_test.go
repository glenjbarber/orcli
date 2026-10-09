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
