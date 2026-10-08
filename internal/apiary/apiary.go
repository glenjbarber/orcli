// Package apiary exposes fixed, read-only queries against Apiary's REST shim.
package apiary

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/glenjbarber/orcli/internal/tools"
)

const maxResponse = 4 << 20

// Client can query Apiary's REST shim using an existing Viewer credential.
type Client struct {
	Base  string
	Token string
	HTTP  *http.Client
}

func New(base, token string) *Client {
	return &Client{Base: strings.TrimRight(base, "/"), Token: token, HTTP: &http.Client{Timeout: 20 * time.Second}}
}

// ToolSet only contains GET operations for Apiary's viewer endpoints.
func (c *Client) ToolSet() []tools.Tool {
	return []tools.Tool{&queryTool{client: c}}
}

type queryTool struct{ client *Client }

func (*queryTool) Name() string { return "apiary_query" }
func (*queryTool) Describe() tools.Schema {
	return tools.Schema{Type: "function", Function: tools.FunctionSpec{Name: "apiary_query", Description: "Read Apiary infrastructure through fixed Viewer-only GET queries. Query is status, health, vms, vm, jails, jail, or networks. Supply id only for vm or jail. No changes or permission escalation are available.", Parameters: map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string", "enum": []string{"status", "health", "vms", "vm", "jails", "jail", "networks"}}, "id": map[string]any{"type": "string", "description": "VM or jail ID for a detail lookup"}}, "required": []string{"query"}, "additionalProperties": false}}}
}

func (t *queryTool) Run(raw json.RawMessage) tools.Result {
	if t.client == nil || t.client.Base == "" || t.client.Token == "" {
		return tools.Result{Err: errors.New("apiary: configure apiary.base_url and apiary.viewer_token")}
	}
	var args struct {
		Query string `json:"query"`
		ID    string `json:"id"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return tools.Result{Err: errors.New("apiary: arguments must be a JSON object")}
	}
	paths := map[string]string{"status": "/v1/status", "health": "/v1/health", "vms": "/v1/vms", "jails": "/v1/jails", "networks": "/v1/networks"}
	switch args.Query {
	case "vm", "jail":
		if strings.TrimSpace(args.ID) == "" || len(args.ID) > 128 || strings.ContainsAny(args.ID, "/?#") {
			return tools.Result{Err: errors.New("apiary: a valid id is required for this query")}
		}
		resource := "vms"
		if args.Query == "jail" {
			resource = "jails"
		}
		paths[args.Query] = "/v1/" + resource + "/" + url.PathEscape(args.ID)
	}
	path, ok := paths[args.Query]
	if !ok {
		return tools.Result{Err: errors.New("apiary: unsupported query")}
	}
	req, err := http.NewRequest(http.MethodGet, t.client.Base+path, nil)
	if err != nil {
		return tools.Result{Err: errors.New("apiary: invalid base URL")}
	}
	req.Header.Set("Authorization", "Bearer "+t.client.Token)
	httpClient := t.client.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return tools.Result{Err: errors.New("apiary: request failed; check the REST endpoint and Viewer credential")}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil || len(body) > maxResponse {
		return tools.Result{Err: errors.New("apiary: response could not be read or exceeded 4 MiB")}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return tools.Result{Err: fmt.Errorf("apiary: REST API returned HTTP %d", resp.StatusCode)}
	}
	return tools.Result{Content: strings.ReplaceAll(string(body), t.client.Token, "[redacted]")}
}
