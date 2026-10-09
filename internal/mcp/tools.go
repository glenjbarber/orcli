package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/glenjbarber/orcli/internal/tools"
)

// The names the MCP tools are called by.
const (
	listResources = "mcp_list_resources"
	readResource  = "mcp_read_resource"
)

// serverArgs is the server name every MCP tool call carries.
type serverArgs struct {
	Server string `json:"server"`
	URI    string `json:"uri"`
}

// decodeServerArgs reads a call body and checks that it named a configured
// server. The uri is returned too, unchecked, since only readResourceTool
// needs it.
func decodeServerArgs(name string, raw json.RawMessage, servers func() []string) (serverArgs, tools.Result) {
	var a serverArgs
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return a, tools.Result{Err: fmt.Errorf("mcp: %s: the arguments are not a JSON object: %w", name, err)}
		}
	}
	if strings.TrimSpace(a.Server) == "" {
		list := servers()
		if len(list) == 0 {
			return a, tools.Result{Err: fmt.Errorf("mcp: %s: no server was given, and no MCP server is configured", name)}
		}
		return a, tools.Result{Err: fmt.Errorf("mcp: %s: no server was given; configured servers: %s", name, strings.Join(list, ", "))}
	}
	return a, tools.Result{}
}

// listResourcesTool lists the resources a configured MCP server offers.
type listResourcesTool struct{ m *Manager }

func (t *listResourcesTool) Name() string { return listResources }

func (t *listResourcesTool) Describe() tools.Schema {
	return tools.Schema{
		Type: "function",
		Function: tools.FunctionSpec{
			Name: listResources,
			Description: fmt.Sprintf(
				"list the resources an MCP server exposes, each as a URI, a name, and an "+
					"optional description. %s.", t.m.configuredServers(),
			),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"server": map[string]any{
						"type":        "string",
						"description": "the configured MCP server to list resources from",
					},
				},
				"required": []string{"server"},
			},
		},
	}
}

func (t *listResourcesTool) Run(raw json.RawMessage) tools.Result {
	a, bad := decodeServerArgs(listResources, raw, t.m.names)
	if bad.Err != nil {
		return bad
	}

	c, err := t.m.get(a.Server)
	if err != nil {
		return tools.Result{Err: err}
	}

	resources, err := c.listResources()
	if err != nil {
		t.m.drop(a.Server)
		return tools.Result{Err: err}
	}
	if len(resources) == 0 {
		return tools.Result{Content: fmt.Sprintf("%s exposes no resources", a.Server)}
	}

	var b strings.Builder
	for _, r := range resources {
		fmt.Fprintf(&b, "%s\t%s", r.URI, r.Name)
		if r.Description != "" {
			fmt.Fprintf(&b, "\t%s", r.Description)
		}
		b.WriteString("\n")
	}
	return tools.Result{Content: b.String()}
}

// readResourceTool reads one resource from a configured MCP server.
type readResourceTool struct{ m *Manager }

func (t *readResourceTool) Name() string { return readResource }

func (t *readResourceTool) Describe() tools.Schema {
	return tools.Schema{
		Type: "function",
		Function: tools.FunctionSpec{
			Name: readResource,
			Description: fmt.Sprintf(
				"read one resource from an MCP server by its URI, as returned by %s. %s.",
				listResources, t.m.configuredServers(),
			),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"server": map[string]any{
						"type":        "string",
						"description": "the configured MCP server to read from",
					},
					"uri": map[string]any{
						"type":        "string",
						"description": "the resource's URI",
					},
				},
				"required": []string{"server", "uri"},
			},
		},
	}
}

func (t *readResourceTool) Run(raw json.RawMessage) tools.Result {
	a, bad := decodeServerArgs(readResource, raw, t.m.names)
	if bad.Err != nil {
		return bad
	}
	if strings.TrimSpace(a.URI) == "" {
		return tools.Result{Err: fmt.Errorf("mcp: %s: no uri was given", readResource)}
	}

	c, err := t.m.get(a.Server)
	if err != nil {
		return tools.Result{Err: err}
	}

	content, err := c.readResource(a.URI)
	if err != nil {
		t.m.drop(a.Server)
		return tools.Result{Err: err}
	}
	return tools.Result{Content: content}
}
