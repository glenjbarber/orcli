package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const mcpKey = "mcp"

// MCP keeps its original bytes so a future member survives a configuration round trip.
type MCP struct{ raw json.RawMessage }

func (m *MCP) UnmarshalJSON(data []byte) error {
	m.raw = append(json.RawMessage(nil), data...)
	return nil
}

func (m MCP) MarshalJSON() ([]byte, error) {
	if len(m.raw) == 0 {
		return []byte("null"), nil
	}
	return m.raw, nil
}

// MCPServer is one Model Context Protocol server the reader has configured -
// either a local command launched as a subprocess and spoken to over its
// standard input and output, or a remote server reached over HTTP using the
// Streamable HTTP transport (the one the specification currently
// recommends; the older HTTP+SSE transport is not supported). The two are
// mutually exclusive: MCPServers refuses a server naming both a command and
// a URL, or neither.
type MCPServer struct {
	// Name is how the model names this server when it calls a tool.
	Name string

	// Command is the program to launch, resolved by the search path.
	// Empty for a remote server.
	Command string

	// Args are the arguments the program is started with. Local only.
	Args []string

	// Env is the subprocess's environment, as KEY=VALUE pairs, in addition to
	// PATH. It is never read from this process's own environment: a server
	// that needs a credential gets it because the reader wrote it here, the
	// same way every other credential in this file is handled. Local only.
	Env []string

	// URL is the remote server's own Streamable HTTP endpoint. Empty for a
	// local server.
	URL string

	// Headers are sent with every request to a remote server - this
	// transport's own equivalent of Env, for a bearer token or any other
	// credential the server itself needs, written here rather than read
	// from this process's own environment. Remote only.
	Headers map[string]string
}

// MCPServers returns the MCP servers configured under the mcp block.
//
// A server with no name, no command, or a name repeated by another server in
// the same block is a fault reported by name rather than a server silently
// dropped: a reader who misconfigured one server should not find out by the
// model never having it, three sessions later.
func (c Config) MCPServers() ([]MCPServer, error) {
	if c.MCP == nil || isNull(c.MCP.raw) {
		return nil, nil
	}
	if !isObject(c.MCP.raw) {
		return nil, fmt.Errorf("config: %s: %w", mcpKey, ErrNotAnObject)
	}

	var block struct {
		Servers []struct {
			Name    string            `json:"name"`
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(c.MCP.raw, &block); err != nil {
		return nil, fmt.Errorf("config: %s: %w", mcpKey, err)
	}

	seen := make(map[string]struct{}, len(block.Servers))
	servers := make([]MCPServer, 0, len(block.Servers))
	for _, s := range block.Servers {
		name := strings.TrimSpace(s.Name)
		command := strings.TrimSpace(s.Command)
		url := strings.TrimSpace(s.URL)
		if name == "" {
			return nil, fmt.Errorf("config: %s: a server with no name was given", mcpKey)
		}
		if command == "" && url == "" {
			return nil, fmt.Errorf("config: %s: server %q has neither a command nor a url", mcpKey, name)
		}
		if command != "" && url != "" {
			return nil, fmt.Errorf("config: %s: server %q names both a command and a url; a server is local (command) or remote (url), never both", mcpKey, name)
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("config: %s: server %q is listed more than once", mcpKey, name)
		}
		seen[name] = struct{}{}

		servers = append(servers, MCPServer{
			Name:    name,
			Command: command,
			Args:    append([]string(nil), s.Args...),
			Env:     sortedKeyValues(s.Env),
			URL:     url,
			Headers: s.Headers,
		})
	}
	return servers, nil
}

// sortedKeyValues renders a map as KEY=VALUE strings, sorted by key, so the
// order is stable across reads of the same file rather than following Go's
// own randomized map iteration.
func sortedKeyValues(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+m[k])
	}
	return out
}

func readMCP(raw json.RawMessage) (*MCP, error) {
	if isNull(raw) || len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	return &MCP{raw: append(json.RawMessage(nil), raw...)}, nil
}
