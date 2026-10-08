// Package notion provides model-callable tools backed by Notion's public REST API.
// It does not call the Codex-hosted Notion MCP connector: that connector is not
// available to the orcli process.
package notion

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/glenjbarber/orcli/internal/tools"
)

const apiBase = "https://api.notion.com/v1"
const apiVersion = "2025-09-03"

// Client calls the public Notion API using an integration token.
type Client struct {
	Token    string
	HTTP     *http.Client
	Base     string
	Approval string
}

// New returns a client using Notion's public API.
func New(token string) *Client {
	return &Client{Token: token, HTTP: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, Base: apiBase}
}

// SetApproval applies the current session's write policy to Notion calls.
func (c *Client) SetApproval(mode string) { c.Approval = mode }

// ToolSet returns the REST operations orcli currently exposes to the model.
func (c *Client) ToolSet() []tools.Tool {
	return []tools.Tool{
		&apiTool{client: c, name: "notion_search", method: http.MethodPost, path: "/search", description: "Search pages and databases by title or query. Supply query and optional filter, sort, and page_size.", schema: objectSchema(map[string]any{"query": stringSchema("Search text")}, "query")},
		&apiTool{client: c, name: "notion_fetch", method: http.MethodGet, pathArg: "id", path: "/pages/", description: "Fetch a Notion page by ID. Use notion_list_block_children to read its content blocks.", schema: objectSchema(map[string]any{"id": stringSchema("Page ID")}, "id")},
		&apiTool{client: c, name: "notion_list_block_children", method: http.MethodGet, pathArg: "block_id", path: "/blocks/", suffix: "/children", description: "List the content blocks under a page or block. Supports start_cursor and page_size.", schema: objectSchema(map[string]any{"block_id": stringSchema("Page or block ID"), "start_cursor": stringSchema("Pagination cursor"), "page_size": map[string]any{"type": "integer"}}, "block_id")},
		&apiTool{client: c, name: "notion_create_page", method: http.MethodPost, path: "/pages", bodyArg: true, description: "Create a page under a parent page or data source using Notion's page properties and children format.", schema: objectSchema(map[string]any{"parent": map[string]any{"type": "object"}, "properties": map[string]any{"type": "object"}, "children": map[string]any{"type": "array"}, "icon": map[string]any{"type": "object"}, "cover": map[string]any{"type": "object"}}, "parent", "properties")},
		&apiTool{client: c, name: "notion_update_page", method: http.MethodPatch, pathArg: "page_id", path: "/pages/", bodyArg: true, description: "Update a page's properties, icon, cover, or archived state.", schema: objectSchema(map[string]any{"page_id": stringSchema("Page ID"), "properties": map[string]any{"type": "object"}, "icon": map[string]any{"type": "object"}, "cover": map[string]any{"type": "object"}, "archived": map[string]any{"type": "boolean"}}, "page_id")},
		&apiTool{client: c, name: "notion_query_data_source", method: http.MethodPost, pathArg: "data_source_id", path: "/data_sources/", suffix: "/query", bodyArg: true, description: "Query a Notion data source with its filter, sorts, and pagination options.", schema: objectSchema(map[string]any{"data_source_id": stringSchema("Data source ID"), "filter": map[string]any{"type": "object"}, "sorts": map[string]any{"type": "array"}, "start_cursor": stringSchema("Pagination cursor"), "page_size": map[string]any{"type": "integer"}}, "data_source_id")},
		&apiTool{client: c, name: "notion_create_comment", method: http.MethodPost, path: "/comments", bodyArg: true, description: "Add a comment to a page or reply to an existing discussion.", schema: objectSchema(map[string]any{"parent": map[string]any{"type": "object"}, "rich_text": map[string]any{"type": "array"}, "discussion_id": stringSchema("Discussion ID")}, "parent", "rich_text")},
		&apiTool{client: c, name: "notion_get_comments", method: http.MethodGet, path: "/comments", queryArg: "block_id", description: "List comments on a page or block. Supports start_cursor and page_size.", schema: objectSchema(map[string]any{"block_id": stringSchema("Page or block ID"), "start_cursor": stringSchema("Pagination cursor"), "page_size": map[string]any{"type": "integer"}}, "block_id")},
		&publicAPITool{client: c},
	}
}

// Tool is one callable Notion operation.
type apiTool struct {
	client                                                     *Client
	name, method, path, pathArg, suffix, queryArg, description string
	bodyArg                                                    bool
	schema                                                     any
}

func (t *apiTool) Name() string            { return t.name }
func (t *apiTool) SetApproval(mode string) { t.client.SetApproval(mode) }
func (t *apiTool) Describe() tools.Schema {
	return tools.Schema{Type: "function", Function: tools.FunctionSpec{Name: t.name, Description: t.description, Parameters: t.schema}}
}

func (t *apiTool) Run(raw json.RawMessage) tools.Result {
	if t.client == nil || strings.TrimSpace(t.client.Token) == "" {
		return tools.Result{Err: errors.New("notion: no integration token; add notion.api_key to the configuration file")}
	}
	if t.method != http.MethodGet && t.client.Approval == tools.ApprovalDeny {
		return tools.Result{Err: errors.New("notion: write call refused because approval mode is deny")}
	}
	var args map[string]json.RawMessage
	if len(raw) == 0 {
		args = map[string]json.RawMessage{}
	} else if err := json.Unmarshal(raw, &args); err != nil || args == nil {
		return tools.Result{Err: errors.New("notion: arguments must be a JSON object")}
	}
	path := t.path
	if t.pathArg != "" {
		var id string
		if err := json.Unmarshal(args[t.pathArg], &id); err != nil || strings.TrimSpace(id) == "" {
			return tools.Result{Err: fmt.Errorf("notion: %s is required", t.pathArg)}
		}
		path += url.PathEscape(id) + t.suffix
		delete(args, t.pathArg)
	}
	var body io.Reader
	if t.bodyArg {
		data, err := json.Marshal(args)
		if err != nil {
			return tools.Result{Err: fmt.Errorf("notion: encode request: %w", err)}
		}
		body = bytes.NewReader(data)
	}
	endpoint := strings.TrimRight(t.client.Base, "/") + path
	if t.method == http.MethodGet {
		query, _ := json.Marshal(args)
		var values map[string]json.RawMessage
		_ = json.Unmarshal(query, &values)
		params := url.Values{}
		for key, value := range values {
			var s string
			if json.Unmarshal(value, &s) == nil {
				params.Set(key, s)
			} else {
				params.Set(key, string(value))
			}
		}
		if encoded := params.Encode(); encoded != "" {
			endpoint += "?" + encoded
		}
	}
	req, err := http.NewRequest(t.method, endpoint, body)
	if err != nil {
		return tools.Result{Err: errors.New("notion: could not build request")}
	}
	req.Header.Set("Authorization", "Bearer "+t.client.Token)
	req.Header.Set("Notion-Version", apiVersion)
	if t.bodyArg {
		req.Header.Set("Content-Type", "application/json")
	}
	httpClient := t.client.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return tools.Result{Err: errors.New("notion: request failed; check connectivity and the integration token")}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return tools.Result{Err: errors.New("notion: could not read response")}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return tools.Result{Err: fmt.Errorf("notion: API returned HTTP %d: %s", resp.StatusCode, safeErrorBody(data, t.client.Token))}
	}
	return tools.Result{Content: strings.ReplaceAll(string(data), t.client.Token, "[redacted]")}
}

func safeErrorBody(data []byte, token string) string {
	var v map[string]any
	if json.Unmarshal(data, &v) != nil {
		return "request was rejected"
	}
	if s, ok := v["message"].(string); ok {
		return strings.ReplaceAll(s, token, "[redacted]")
	}
	return "request was rejected"
}

func stringSchema(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}
func objectSchema(properties map[string]any, required ...string) any {
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

// publicOperations is the documented JSON REST API surface supported by notion_api.
// Connector-only orchestration and upload transport calls are outside this set.
var publicOperations = map[string]struct {
	method, path, idSuffix string
	body                   bool
}{
	"search":        {http.MethodPost, "/search", "", true},
	"retrieve_page": {http.MethodGet, "/pages/", "", false}, "create_page": {http.MethodPost, "/pages", "", true}, "update_page": {http.MethodPatch, "/pages/", "", true},
	"retrieve_block": {http.MethodGet, "/blocks/", "", false}, "update_block": {http.MethodPatch, "/blocks/", "", true}, "delete_block": {http.MethodDelete, "/blocks/", "", false},
	"get_block_children": {http.MethodGet, "/blocks/", "/children", false}, "append_block_children": {http.MethodPatch, "/blocks/", "/children", true},
	"retrieve_database": {http.MethodGet, "/databases/", "", false}, "create_database": {http.MethodPost, "/databases", "", true}, "update_database": {http.MethodPatch, "/databases/", "", true},
	"retrieve_data_source": {http.MethodGet, "/data_sources/", "", false},
	"create_data_source":   {http.MethodPost, "/data_sources", "", true}, "update_data_source": {http.MethodPatch, "/data_sources/", "", true}, "query_data_source": {http.MethodPost, "/data_sources/", "/query", true},
	"create_comment": {http.MethodPost, "/comments", "", true}, "list_comments": {http.MethodGet, "/comments", "", false},
	"list_users": {http.MethodGet, "/users", "", false}, "retrieve_user": {http.MethodGet, "/users/", "", false},
	"create_file_upload": {http.MethodPost, "/file_uploads", "", true}, "retrieve_file_upload": {http.MethodGet, "/file_uploads/", "", false},
	"complete_file_upload": {http.MethodPost, "/file_uploads/", "/complete", true}, "list_file_upload_parts": {http.MethodGet, "/file_uploads/", "/parts", false},
}

type publicAPITool struct{ client *Client }

func (t *publicAPITool) Name() string            { return "notion_api" }
func (t *publicAPITool) SetApproval(mode string) { t.client.SetApproval(mode) }
func (t *publicAPITool) Describe() tools.Schema {
	operations := make([]string, 0, len(publicOperations))
	for name := range publicOperations {
		operations = append(operations, name)
	}
	sort.Strings(operations)
	return tools.Schema{Type: "function", Function: tools.FunctionSpec{Name: t.Name(), Description: "Call a documented Notion public REST API operation. Choose operation and provide optional id, query parameters, and JSON body. File data transfer is not supported by this JSON interface.", Parameters: map[string]any{"type": "object", "properties": map[string]any{"operation": map[string]any{"type": "string", "enum": operations}, "id": stringSchema("Object ID required by this operation"), "query": map[string]any{"type": "object"}, "body": map[string]any{"type": "object"}}, "required": []string{"operation"}, "additionalProperties": false}}}
}
func (t *publicAPITool) Run(raw json.RawMessage) tools.Result {
	if t.client == nil || strings.TrimSpace(t.client.Token) == "" {
		return tools.Result{Err: errors.New("notion: no integration token; add notion.api_key to the configuration file")}
	}
	var args struct {
		Operation string          `json:"operation"`
		ID        string          `json:"id"`
		Query     map[string]any  `json:"query"`
		Body      json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return tools.Result{Err: errors.New("notion: arguments must be a JSON object")}
	}
	op, ok := publicOperations[args.Operation]
	if !ok {
		return tools.Result{Err: fmt.Errorf("notion: unsupported public API operation %q", args.Operation)}
	}
	if op.method != http.MethodGet && t.client.Approval == tools.ApprovalDeny {
		return tools.Result{Err: errors.New("notion: write call refused because approval mode is deny")}
	}
	if strings.Contains(op.path, "/") && strings.HasSuffix(op.path, "/") && strings.TrimSpace(args.ID) == "" {
		return tools.Result{Err: errors.New("notion: this operation requires id")}
	}
	path := op.path
	if strings.HasSuffix(path, "/") {
		path += url.PathEscape(args.ID)
	}
	path += op.idSuffix
	endpoint := strings.TrimRight(t.client.Base, "/") + path
	if len(args.Query) > 0 {
		values := url.Values{}
		for k, v := range args.Query {
			values.Set(k, fmt.Sprint(v))
		}
		endpoint += "?" + values.Encode()
	}
	var body io.Reader
	if op.body {
		if len(args.Body) == 0 {
			args.Body = json.RawMessage(`{}`)
		}
		if !json.Valid(args.Body) {
			return tools.Result{Err: errors.New("notion: body must be a JSON value")}
		}
		body = bytes.NewReader(args.Body)
	}
	req, err := http.NewRequest(op.method, endpoint, body)
	if err != nil {
		return tools.Result{Err: errors.New("notion: could not build request")}
	}
	req.Header.Set("Authorization", "Bearer "+t.client.Token)
	req.Header.Set("Notion-Version", apiVersion)
	if op.body {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := t.client.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return tools.Result{Err: errors.New("notion: request failed; check connectivity and the integration token")}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return tools.Result{Err: errors.New("notion: could not read response")}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return tools.Result{Err: fmt.Errorf("notion: API returned HTTP %d: %s", resp.StatusCode, safeErrorBody(data, t.client.Token))}
	}
	return tools.Result{Content: strings.ReplaceAll(string(data), t.client.Token, "[redacted]")}
}
