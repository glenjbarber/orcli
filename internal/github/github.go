// Package github provides scoped GitHub issue and task operations.
package github

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/glenjbarber/orcli/internal/tools"
)

const (
	apiBase     = "https://api.github.com"
	apiVersion  = "2022-11-28"
	maxResponse = 4 << 20
)

// Client accesses issues only in Repositories, the explicit configuration
// allowlist. It does not use the ChatGPT GitHub connector.
type Client struct {
	Token        string
	Login        string
	Repositories []string
	HTTP         *http.Client
	Base         string
	Approval     string
}

// New returns a client whose task operations are limited to repositories.
func New(token, login string, repositories []string) *Client {
	repos := make([]string, 0, len(repositories))
	seen := make(map[string]struct{}, len(repositories))
	for _, repository := range repositories {
		if validRepository(repository) {
			if _, exists := seen[repository]; !exists {
				repos = append(repos, repository)
				seen[repository] = struct{}{}
			}
		}
	}
	return &Client{
		Token: token, Login: login, Repositories: repos, Base: apiBase,
		HTTP: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// ToolSet exposes task reads and writes inside the configured repositories.
func (c *Client) ToolSet() []tools.Tool { return []tools.Tool{&taskTool{client: c}} }

func (c *Client) SetApproval(mode string) { c.Approval = mode }

type taskTool struct{ client *Client }

func (*taskTool) Name() string              { return "github_tasks" }
func (t *taskTool) SetApproval(mode string) { t.client.SetApproval(mode) }
func (*taskTool) Describe() tools.Schema {
	return tools.Schema{Type: "function", Function: tools.FunctionSpec{
		Name:        "github_tasks",
		Description: "Read and manage GitHub issues as tasks. Listing searches only the repositories explicitly allowlisted in the configuration and only issues assigned to the configured GitHub login. Reads include issue details and comments. Writes can create issues, update issue fields/state, and add comments. A write whose result is unknown is never retried automatically; inspect the target before retrying.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"operation": map[string]any{"type": "string", "enum": []string{"list_assigned", "get", "list_comments", "create", "update", "comment"}},
				"repo":      map[string]any{"type": "string", "description": "An owner/repo entry from the configured allowlist"},
				"number":    map[string]any{"type": "integer", "description": "Issue number"},
				"state":     map[string]any{"type": "string", "enum": []string{"open", "closed", "all"}},
				"limit":     map[string]any{"type": "integer", "description": "Maximum total results for list_assigned (1-100, default 20)"},
				"title":     map[string]any{"type": "string"},
				"body":      map[string]any{"type": "string"},
				"labels":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"assignees": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
			"required":             []string{"operation"},
			"additionalProperties": false,
		},
	}}
}

type taskArgs struct {
	Operation  string    `json:"operation"`
	Repository string    `json:"repo"`
	Number     int       `json:"number"`
	State      string    `json:"state"`
	Limit      int       `json:"limit"`
	Title      *string   `json:"title"`
	Body       *string   `json:"body"`
	Labels     *[]string `json:"labels"`
	Assignees  *[]string `json:"assignees"`
}

func (t *taskTool) Run(raw json.RawMessage) tools.Result {
	c := t.client
	if c == nil || strings.TrimSpace(c.Token) == "" {
		return tools.Result{Err: errors.New("github: no API token; add github.api_key to the configuration file")}
	}
	if strings.TrimSpace(c.Login) == "" || len(c.Repositories) == 0 {
		return tools.Result{Err: errors.New("github: configure github.login and an explicit github.repositories allowlist before using task operations")}
	}
	var args taskArgs
	if err := decodeArgs(raw, &args); err != nil {
		return tools.Result{Err: err}
	}
	switch args.Operation {
	case "list_assigned":
		return t.listAssigned(args)
	case "get", "list_comments":
		if err := t.checkTarget(args); err != nil {
			return tools.Result{Err: err}
		}
		if args.Operation == "get" {
			return t.read(http.MethodGet, issuePath(args.Repository, args.Number), nil)
		}
		return t.read(http.MethodGet, issuePath(args.Repository, args.Number)+"/comments", nil)
	case "create":
		if t.client.Approval == tools.ApprovalDeny {
			return tools.Result{Err: errors.New("github: write call refused because approval mode is deny")}
		}
		if !t.allowedRepository(args.Repository) {
			return tools.Result{Err: errors.New("github: repository is not in the configured allowlist")}
		}
		if args.Title == nil || strings.TrimSpace(*args.Title) == "" {
			return tools.Result{Err: errors.New("github: create requires a non-empty title")}
		}
		payload := map[string]any{"title": *args.Title}
		if args.Body != nil {
			payload["body"] = *args.Body
		}
		if args.Labels != nil {
			payload["labels"] = *args.Labels
		}
		if args.Assignees != nil {
			payload["assignees"] = *args.Assignees
		}
		return t.write(http.MethodPost, issueCollectionPath(args.Repository), payload)
	case "update":
		if t.client.Approval == tools.ApprovalDeny {
			return tools.Result{Err: errors.New("github: write call refused because approval mode is deny")}
		}
		if err := t.checkTarget(args); err != nil {
			return tools.Result{Err: err}
		}
		payload := make(map[string]any)
		if args.Title != nil {
			payload["title"] = *args.Title
		}
		if args.Body != nil {
			payload["body"] = *args.Body
		}
		if args.State != "" {
			if args.State != "open" && args.State != "closed" {
				return tools.Result{Err: errors.New("github: update state must be open or closed")}
			}
			payload["state"] = args.State
		}
		if args.Labels != nil {
			payload["labels"] = *args.Labels
		}
		if args.Assignees != nil {
			payload["assignees"] = *args.Assignees
		}
		if len(payload) == 0 {
			return tools.Result{Err: errors.New("github: update requires at least one field")}
		}
		return t.write(http.MethodPatch, issuePath(args.Repository, args.Number), payload)
	case "comment":
		if t.client.Approval == tools.ApprovalDeny {
			return tools.Result{Err: errors.New("github: write call refused because approval mode is deny")}
		}
		if err := t.checkTarget(args); err != nil {
			return tools.Result{Err: err}
		}
		if args.Body == nil || strings.TrimSpace(*args.Body) == "" {
			return tools.Result{Err: errors.New("github: comment requires a non-empty body")}
		}
		return t.write(http.MethodPost, issuePath(args.Repository, args.Number)+"/comments", map[string]string{"body": *args.Body})
	default:
		return tools.Result{Err: fmt.Errorf("github: unsupported task operation %q", args.Operation)}
	}
}

func (t *taskTool) listAssigned(args taskArgs) tools.Result {
	state := args.State
	if state == "" {
		state = "open"
	}
	if state != "open" && state != "closed" && state != "all" {
		return tools.Result{Err: errors.New("github: list state must be open, closed, or all")}
	}
	limit := args.Limit
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 100 {
		return tools.Result{Err: errors.New("github: list limit must be between 1 and 100")}
	}
	items := make([]json.RawMessage, 0, limit)
	for _, repository := range t.client.Repositories {
		for page := 1; len(items) < limit; page++ {
			perPage := limit - len(items)
			if perPage > 100 {
				perPage = 100
			}
			query := url.Values{"assignee": {t.client.Login}, "state": {state}, "per_page": {strconv.Itoa(perPage)}, "page": {strconv.Itoa(page)}}
			result := t.request(http.MethodGet, issueCollectionPath(repository)+"?"+query.Encode(), nil)
			if result.Err != nil {
				return result
			}
			var pageItems []json.RawMessage
			if err := json.Unmarshal([]byte(result.Content), &pageItems); err != nil {
				return tools.Result{Err: errors.New("github: could not decode issue list response")}
			}
			for _, item := range pageItems {
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(item, &fields); err != nil {
					continue
				}
				if _, isPullRequest := fields["pull_request"]; isPullRequest {
					continue
				}
				wrapped, err := json.Marshal(map[string]any{"repository": repository, "issue": json.RawMessage(item)})
				if err != nil {
					return tools.Result{Err: errors.New("github: could not encode issue list response")}
				}
				items = append(items, wrapped)
				if len(items) == limit {
					break
				}
			}
			if len(pageItems) < perPage {
				break
			}
		}
		if len(items) == limit {
			break
		}
	}
	data, err := json.Marshal(map[string]any{"count": len(items), "items": items})
	if err != nil {
		return tools.Result{Err: errors.New("github: could not encode issue list response")}
	}
	return tools.Result{Content: strings.ReplaceAll(string(data), t.client.Token, "[redacted]")}
}

func (t *taskTool) checkTarget(args taskArgs) error {
	if !t.allowedRepository(args.Repository) {
		return errors.New("github: repository is not in the configured allowlist")
	}
	if args.Number < 1 {
		return errors.New("github: a positive issue number is required")
	}
	return nil
}

func (t *taskTool) allowedRepository(repository string) bool {
	for _, allowed := range t.client.Repositories {
		if repository == allowed {
			return true
		}
	}
	return false
}

func (t *taskTool) read(method, path string, payload any) tools.Result {
	data, err := t.do(method, path, payload, false)
	if err != nil {
		return tools.Result{Err: err}
	}
	return tools.Result{Content: strings.ReplaceAll(string(data), t.client.Token, "[redacted]")}
}

func (t *taskTool) write(method, path string, payload any) tools.Result {
	data, err := t.do(method, path, payload, true)
	if err != nil {
		return tools.Result{Err: err}
	}
	return tools.Result{Content: strings.ReplaceAll(string(data), t.client.Token, "[redacted]")}
}

func (t *taskTool) request(method, path string, payload any) tools.Result {
	data, err := t.do(method, path, payload, false)
	if err != nil {
		return tools.Result{Err: err}
	}
	return tools.Result{Content: strings.ReplaceAll(string(data), t.client.Token, "[redacted]")}
}

func (t *taskTool) do(method, path string, payload any, write bool) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, errors.New("github: could not encode request")
		}
		body = bytes.NewReader(data)
	}
	base := strings.TrimRight(t.client.Base, "/")
	req, err := http.NewRequest(method, base+path, body)
	if err != nil {
		return nil, errors.New("github: could not build request")
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+t.client.Token)
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
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
			return nil, errors.New("github: request failed; write outcome is unknown, inspect the target before retrying")
		}
		return nil, errors.New("github: request failed; check connectivity and the API token")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil || len(data) > maxResponse {
		if write {
			return nil, errors.New("github: response could not be read; write outcome is unknown, inspect the target before retrying")
		}
		return nil, errors.New("github: response could not be read or exceeded 4 MiB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := safeMessage(data, t.client.Token)
		if write && resp.StatusCode >= 500 {
			return nil, fmt.Errorf("github: API returned HTTP %d: %s; write outcome is unknown, inspect the target before retrying", resp.StatusCode, message)
		}
		return nil, fmt.Errorf("github: API returned HTTP %d: %s", resp.StatusCode, message)
	}
	return data, nil
}

func safeMessage(data []byte, token string) string {
	var response struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &response) != nil || response.Message == "" {
		return "request was rejected"
	}
	message := strings.ReplaceAll(response.Message, token, "[redacted]")
	if len(message) > 300 {
		message = message[:300]
	}
	return message
}

func decodeArgs(raw json.RawMessage, destination any) error {
	if len(raw) == 0 {
		return errors.New("github: arguments must be a JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return errors.New("github: arguments must be a valid object with supported fields")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("github: arguments must contain one JSON object")
	}
	if first := bytes.TrimSpace(raw); len(first) == 0 || first[0] != '{' {
		return errors.New("github: arguments must be a JSON object")
	}
	return nil
}

func issueCollectionPath(repository string) string { return "/repos/" + repository + "/issues" }
func issuePath(repository string, number int) string {
	return issueCollectionPath(repository) + "/" + strconv.Itoa(number)
}

func validRepository(repository string) bool {
	if len(repository) > 201 || strings.Count(repository, "/") != 1 || strings.TrimSpace(repository) != repository {
		return false
	}
	owner, name, _ := strings.Cut(repository, "/")
	return validSegment(owner) && validSegment(name)
}

func validSegment(segment string) bool {
	if segment == "" || segment == "." || segment == ".." {
		return false
	}
	for _, r := range segment {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}
