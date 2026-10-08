package notion

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/glenjbarber/orcli/internal/tools"
)

// TaskTool returns task operations constrained to one configured data source
// and one configured Notion assignee.
func (c *Client) TaskTool(dataSourceID, assigneeProperty, assigneeID, statusProperty string) tools.Tool {
	return &taskTool{client: c, dataSourceID: dataSourceID, assigneeProperty: assigneeProperty, assigneeID: assigneeID, statusProperty: statusProperty}
}

type taskTool struct {
	client           *Client
	dataSourceID     string
	assigneeProperty string
	assigneeID       string
	statusProperty   string
}

func (*taskTool) Name() string              { return "notion_tasks" }
func (t *taskTool) SetApproval(mode string) { t.client.SetApproval(mode) }
func (*taskTool) Describe() tools.Schema {
	return tools.Schema{Type: "function", Function: tools.FunctionSpec{
		Name:        "notion_tasks",
		Description: "Read and manage tasks only in the configured Notion data source. Listing is filtered to the configured Notion assignee. Create, update, status changes, and comments are bounded to that source; no other Notion page or data source is used. Writes are never retried automatically; inspect the target when a write outcome is unknown.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"operation":    map[string]any{"type": "string", "enum": []string{"list_assigned", "get", "create", "update", "set_status", "list_comments", "comment"}},
				"task_id":      map[string]any{"type": "string", "description": "Notion task page ID"},
				"title":        map[string]any{"type": "string"},
				"description":  map[string]any{"type": "string"},
				"status":       map[string]any{"type": "string"},
				"properties":   map[string]any{"type": "object", "description": "Additional Notion task properties"},
				"comment":      map[string]any{"type": "string"},
				"start_cursor": map[string]any{"type": "string"},
				"page_size":    map[string]any{"type": "integer"},
			},
			"required":             []string{"operation"},
			"additionalProperties": false,
		},
	}}
}

type taskArgs struct {
	Operation   string                     `json:"operation"`
	TaskID      string                     `json:"task_id"`
	Title       string                     `json:"title"`
	Description string                     `json:"description"`
	Status      string                     `json:"status"`
	Properties  map[string]json.RawMessage `json:"properties"`
	Comment     string                     `json:"comment"`
	StartCursor string                     `json:"start_cursor"`
	PageSize    int                        `json:"page_size"`
}

func (t *taskTool) Run(raw json.RawMessage) tools.Result {
	if t.client == nil || strings.TrimSpace(t.client.Token) == "" {
		return tools.Result{Err: errors.New("notion: no integration token; add notion.api_key to the configuration file")}
	}
	if !validTaskNotionID(t.dataSourceID) || !validTaskNotionID(t.assigneeID) || strings.TrimSpace(t.assigneeProperty) == "" || strings.TrimSpace(t.statusProperty) == "" {
		return tools.Result{Err: errors.New("notion: configure task_data_source_id, task_assignee_property, task_assignee_id, and task_status_property")}
	}
	var args taskArgs
	if err := decodeTaskArgs(raw, &args); err != nil {
		return tools.Result{Err: err}
	}
	switch args.Operation {
	case "list_assigned":
		return t.listAssigned(args)
	case "get":
		page, err := t.getTask(args.TaskID)
		if err != nil {
			return tools.Result{Err: err}
		}
		children, err := t.request(http.MethodGet, "/blocks/"+url.PathEscape(args.TaskID)+"/children", nil, false)
		if err != nil {
			return tools.Result{Err: err}
		}
		return t.result(map[string]json.RawMessage{"page": page, "children": children})
	case "create":
		if denied := t.writeDenied(); denied != nil {
			return tools.Result{Err: denied}
		}
		if strings.TrimSpace(args.Title) == "" {
			return tools.Result{Err: errors.New("notion: task creation requires a title")}
		}
		return t.createTask(args)
	case "update":
		if denied := t.writeDenied(); denied != nil {
			return tools.Result{Err: denied}
		}
		if _, err := t.getTask(args.TaskID); err != nil {
			return tools.Result{Err: err}
		}
		properties, err := t.properties(args.Properties)
		if err != nil {
			return tools.Result{Err: err}
		}
		if args.Title != "" {
			schema, err := t.dataSourceSchema()
			if err != nil {
				return tools.Result{Err: err}
			}
			titleName := titleProperty(schema.Properties)
			if titleName == "" {
				return tools.Result{Err: errors.New("notion: configured data source has no title property")}
			}
			properties[titleName] = map[string]any{"title": []any{map[string]any{"type": "text", "text": map[string]any{"content": args.Title}}}}
		}
		if len(properties) == 0 {
			if args.Description != "" {
				return tools.Result{Err: errors.New("notion: page content cannot be replaced as a task property; use the comment operation for a note")}
			}
			return tools.Result{Err: errors.New("notion: update requires title or properties")}
		}
		payload := make(map[string]any)
		payload["properties"] = properties
		data, err := t.request(http.MethodPatch, "/pages/"+url.PathEscape(args.TaskID), payload, true)
		if err != nil {
			return tools.Result{Err: err}
		}
		return t.result(data)
	case "set_status":
		if denied := t.writeDenied(); denied != nil {
			return tools.Result{Err: denied}
		}
		if strings.TrimSpace(args.Status) == "" {
			return tools.Result{Err: errors.New("notion: status change requires a status value")}
		}
		if _, err := t.getTask(args.TaskID); err != nil {
			return tools.Result{Err: err}
		}
		schema, err := t.dataSourceSchema()
		if err != nil {
			return tools.Result{Err: err}
		}
		property, ok := schema.Properties[t.statusProperty]
		if !ok || (property.Type != "status" && property.Type != "select") {
			return tools.Result{Err: errors.New("notion: configured task_status_property is not a status or select property")}
		}
		value := map[string]any{"name": args.Status}
		payload := map[string]any{"properties": map[string]any{t.statusProperty: map[string]any{property.Type: value}}}
		data, err := t.request(http.MethodPatch, "/pages/"+url.PathEscape(args.TaskID), payload, true)
		if err != nil {
			return tools.Result{Err: err}
		}
		return t.result(data)
	case "list_comments":
		if _, err := t.getTask(args.TaskID); err != nil {
			return tools.Result{Err: err}
		}
		query := url.Values{"block_id": {args.TaskID}}
		if args.StartCursor != "" {
			query.Set("start_cursor", args.StartCursor)
		}
		if args.PageSize > 0 {
			if args.PageSize > 100 {
				return tools.Result{Err: errors.New("notion: page_size must not exceed 100")}
			}
			query.Set("page_size", fmt.Sprint(args.PageSize))
		}
		data, err := t.request(http.MethodGet, "/comments?"+query.Encode(), nil, false)
		if err != nil {
			return tools.Result{Err: err}
		}
		return t.result(data)
	case "comment":
		if denied := t.writeDenied(); denied != nil {
			return tools.Result{Err: denied}
		}
		if strings.TrimSpace(args.Comment) == "" {
			return tools.Result{Err: errors.New("notion: comment requires non-empty text")}
		}
		if _, err := t.getTask(args.TaskID); err != nil {
			return tools.Result{Err: err}
		}
		payload := map[string]any{"parent": map[string]string{"page_id": args.TaskID}, "rich_text": []any{map[string]any{"type": "text", "text": map[string]string{"content": args.Comment}}}}
		data, err := t.request(http.MethodPost, "/comments", payload, true)
		if err != nil {
			return tools.Result{Err: err}
		}
		return t.result(data)
	default:
		return tools.Result{Err: fmt.Errorf("notion: unsupported task operation %q", args.Operation)}
	}
}

func (t *taskTool) listAssigned(args taskArgs) tools.Result {
	if args.PageSize > 100 {
		return tools.Result{Err: errors.New("notion: page_size must not exceed 100")}
	}
	filter := map[string]any{"property": t.assigneeProperty, "people": map[string]string{"contains": t.assigneeID}}
	payload := map[string]any{"filter": filter}
	if args.StartCursor != "" {
		payload["start_cursor"] = args.StartCursor
	}
	if args.PageSize > 0 {
		payload["page_size"] = args.PageSize
	}
	data, err := t.request(http.MethodPost, "/data_sources/"+url.PathEscape(t.dataSourceID)+"/query", payload, false)
	if err != nil {
		return tools.Result{Err: err}
	}
	return t.result(data)
}

func (t *taskTool) createTask(args taskArgs) tools.Result {
	schema, err := t.dataSourceSchema()
	if err != nil {
		return tools.Result{Err: err}
	}
	titleName := titleProperty(schema.Properties)
	if titleName == "" {
		return tools.Result{Err: errors.New("notion: configured data source has no title property")}
	}
	assignee, ok := schema.Properties[t.assigneeProperty]
	if !ok || assignee.Type != "people" {
		return tools.Result{Err: errors.New("notion: configured task_assignee_property is not a people property")}
	}
	status, ok := schema.Properties[t.statusProperty]
	if !ok || (status.Type != "status" && status.Type != "select") {
		return tools.Result{Err: errors.New("notion: configured task_status_property is not a status or select property")}
	}
	properties, err := t.properties(args.Properties)
	if err != nil {
		return tools.Result{Err: err}
	}
	properties[titleName] = map[string]any{"title": []any{map[string]any{"type": "text", "text": map[string]any{"content": args.Title}}}}
	properties[t.assigneeProperty] = map[string]any{"people": []any{map[string]string{"id": t.assigneeID}}}
	if args.Status != "" {
		properties[t.statusProperty] = map[string]any{status.Type: map[string]string{"name": args.Status}}
	}
	payload := map[string]any{"parent": map[string]string{"data_source_id": t.dataSourceID}, "properties": properties}
	if args.Description != "" {
		payload["children"] = textBlocks(args.Description)
	}
	data, err := t.request(http.MethodPost, "/pages", payload, true)
	if err != nil {
		return tools.Result{Err: err}
	}
	return t.result(data)
}

func (t *taskTool) getTask(taskID string) (json.RawMessage, error) {
	if !validTaskNotionID(taskID) {
		return nil, errors.New("notion: task_id must be a page ID")
	}
	data, err := t.request(http.MethodGet, "/pages/"+url.PathEscape(taskID), nil, false)
	if err != nil {
		return nil, err
	}
	var page struct {
		Parent map[string]json.RawMessage `json:"parent"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, errors.New("notion: could not verify task page parent")
	}
	var sourceID string
	for _, key := range []string{"data_source_id", "database_id"} {
		if raw := page.Parent[key]; len(raw) > 0 {
			_ = json.Unmarshal(raw, &sourceID)
			if sourceID != "" {
				break
			}
		}
	}
	if sourceID != t.dataSourceID {
		return nil, errors.New("notion: task page is outside the configured task data source")
	}
	return data, nil
}

type dataSource struct {
	Properties map[string]struct {
		Type string `json:"type"`
	} `json:"properties"`
}

func (t *taskTool) dataSourceSchema() (dataSource, error) {
	data, err := t.request(http.MethodGet, "/data_sources/"+url.PathEscape(t.dataSourceID), nil, false)
	if err != nil {
		return dataSource{}, err
	}
	var schema dataSource
	if err := json.Unmarshal(data, &schema); err != nil || len(schema.Properties) == 0 {
		return dataSource{}, errors.New("notion: could not read configured task data source properties")
	}
	return schema, nil
}

func titleProperty(properties map[string]struct {
	Type string `json:"type"`
}) string {
	for name, property := range properties {
		if property.Type == "title" {
			return name
		}
	}
	return ""
}

func (t *taskTool) properties(raw map[string]json.RawMessage) (map[string]any, error) {
	properties := make(map[string]any, len(raw)+3)
	for name, value := range raw {
		if strings.TrimSpace(name) == "" || !json.Valid(value) {
			return nil, errors.New("notion: properties must contain valid JSON values with non-empty names")
		}
		var decoded any
		if err := json.Unmarshal(value, &decoded); err != nil {
			return nil, errors.New("notion: properties must contain valid JSON values")
		}
		properties[name] = decoded
	}
	return properties, nil
}

func (t *taskTool) request(method, path string, payload any, write bool) (json.RawMessage, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, errors.New("notion: could not encode task request")
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, strings.TrimRight(t.client.Base, "/")+path, body)
	if err != nil {
		return nil, errors.New("notion: could not build task request")
	}
	req.Header.Set("Authorization", "Bearer "+t.client.Token)
	req.Header.Set("Notion-Version", apiVersion)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	httpClient := t.client.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		if write {
			return nil, errors.New("notion: request failed; write outcome is unknown, inspect the task before retrying")
		}
		return nil, errors.New("notion: request failed; check connectivity and the integration token")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20+1))
	if err != nil || len(data) > 4<<20 {
		if write {
			return nil, errors.New("notion: response could not be read; write outcome is unknown, inspect the task before retrying")
		}
		return nil, errors.New("notion: response could not be read or exceeded 4 MiB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if write && resp.StatusCode >= 500 {
			return nil, fmt.Errorf("notion: API returned HTTP %d; write outcome is unknown, inspect the task before retrying", resp.StatusCode)
		}
		return nil, fmt.Errorf("notion: API returned HTTP %d: %s", resp.StatusCode, safeErrorBody(data, t.client.Token))
	}
	return json.RawMessage(strings.ReplaceAll(string(data), t.client.Token, "[redacted]")), nil
}

func (t *taskTool) writeDenied() error {
	if t.client.Approval == tools.ApprovalDeny {
		return errors.New("notion: write call refused because approval mode is deny")
	}
	return nil
}

func (t *taskTool) result(value any) tools.Result {
	data, err := json.Marshal(value)
	if err != nil {
		return tools.Result{Err: errors.New("notion: could not encode task result")}
	}
	return tools.Result{Content: strings.ReplaceAll(string(data), t.client.Token, "[redacted]")}
}

func textBlocks(description string) []any {
	return []any{map[string]any{"object": "block", "type": "paragraph", "paragraph": map[string]any{"rich_text": []any{map[string]any{"type": "text", "text": map[string]string{"content": description}}}}}}
}

func decodeTaskArgs(raw json.RawMessage, destination any) error {
	if len(raw) == 0 {
		return errors.New("notion: arguments must be a JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return errors.New("notion: arguments must be a valid object with supported fields")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("notion: arguments must contain one JSON object")
	}
	if first := bytes.TrimSpace(raw); len(first) == 0 || first[0] != '{' {
		return errors.New("notion: arguments must be a JSON object")
	}
	return nil
}

func validTaskNotionID(value string) bool {
	compact := strings.ReplaceAll(value, "-", "")
	if len(compact) != 32 {
		return false
	}
	for _, r := range compact {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}
