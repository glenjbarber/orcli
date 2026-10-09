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

// MCPServer is one Model Context Protocol server the reader has configured,
// launched as a subprocess and spoken to over its standard input and output.
//
// Only this shape - a local command, launched over stdio - is read today. A
// remote server reached over HTTP or SSE is not a configuration this client
// understands yet.
type MCPServer struct {
	// Name is how the model names this server when it calls a tool.
	Name string

	// Command is the program to launch, resolved by the search path.
	Command string

	// Args are the arguments the program is started with.
	Args []string

	// Env is the subprocess's environment, as KEY=VALUE pairs, in addition to
	// PATH. It is never read from this process's own environment: a server
	// that needs a credential gets it because the reader wrote it here, the
	// same way every other credential in this file is handled.
	Env []string
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
		if name == "" {
			return nil, fmt.Errorf("config: %s: a server with no name was given", mcpKey)
		}
		if command == "" {
			return nil, fmt.Errorf("config: %s: server %q has no command", mcpKey, name)
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("config: %s: server %q is listed more than once", mcpKey, name)
		}
		seen[name] = struct{}{}

		keys := make([]string, 0, len(s.Env))
		for k := range s.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		env := make([]string, 0, len(keys))
		for _, k := range keys {
			env = append(env, k+"="+s.Env[k])
		}

		servers = append(servers, MCPServer{
			Name:    name,
			Command: command,
			Args:    append([]string(nil), s.Args...),
			Env:     env,
		})
	}
	return servers, nil
}

func readMCP(raw json.RawMessage) (*MCP, error) {
	if isNull(raw) || len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	return &MCP{raw: append(json.RawMessage(nil), raw...)}, nil
}
