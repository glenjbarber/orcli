package notion

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/glenjbarber/orcli/internal/tools"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestToolSetIncludesPublicRESTRouter(t *testing.T) {
	client := New("token")
	names := make(map[string]bool)
	for _, tool := range client.ToolSet() {
		names[tool.Name()] = true
	}
	for _, name := range []string{"notion_search", "notion_fetch", "notion_list_block_children", "notion_api"} {
		if !names[name] {
			t.Errorf("toolset is missing %q", name)
		}
	}
}

func TestPublicAPIRouterBuildsAuthenticatedRequestAndRedactsToken(t *testing.T) {
	const token = "secret-notion-token-value"
	client := New(token)
	client.Base = "https://notion.test/v1"
	client.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got, want := req.Method, http.MethodPost; got != want {
			t.Errorf("method = %q, want %q", got, want)
		}
		if got, want := req.URL.Path, "/v1/pages"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if got, want := req.Header.Get("Authorization"), "Bearer "+token; got != want {
			t.Errorf("Authorization = %q, want configured bearer token", got)
		}
		if got, want := req.Header.Get("Notion-Version"), apiVersion; got != want {
			t.Errorf("Notion-Version = %q, want %q", got, want)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id":"page-id","token":"` + token + `"}`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}
	tool := &publicAPITool{client: client}
	result := tool.Run([]byte(`{"operation":"create_page","body":{"parent":{"page_id":"parent"}}}`))
	if result.Err != nil {
		t.Fatalf("Run: %v", result.Err)
	}
	if strings.Contains(result.Content, token) {
		t.Errorf("response leaked the configured token: %s", result.Content)
	}
	if !strings.Contains(result.Content, "[redacted]") {
		t.Errorf("response did not mark the token as redacted: %s", result.Content)
	}
}

func TestPublicAPIRouterDenyModeRefusesWritesBeforeRequest(t *testing.T) {
	called := false
	client := New("token")
	client.Approval = tools.ApprovalDeny
	client.HTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, nil
	})}
	result := (&publicAPITool{client: client}).Run([]byte(`{"operation":"create_page"}`))
	if result.Err == nil {
		t.Fatal("write was not refused in deny mode")
	}
	if called {
		t.Fatal("a denied write reached the transport")
	}
}

func TestPublicAPIRouterRejectsUnknownOperation(t *testing.T) {
	result := (&publicAPITool{client: New("token")}).Run([]byte(`{"operation":"send_email"}`))
	if result.Err == nil {
		t.Fatal("unknown operation was accepted")
	}
	if !strings.Contains(result.Err.Error(), "unsupported public API operation") {
		t.Errorf("error = %q, want unsupported operation", result.Err)
	}
}
