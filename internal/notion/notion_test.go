package notion

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

const (
	testTaskDataSource = "11111111-1111-4111-8111-111111111111"
	testTaskAssignee   = "22222222-2222-4222-8222-222222222222"
	testTaskPage       = "33333333-3333-4333-8333-333333333333"
)

func taskClient(handler http.HandlerFunc) *Client {
	client := New("notion-test-token")
	client.Base = "https://notion.test/v1"
	client.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler(recorder, req)
		return recorder.Result(), nil
	})}
	return client
}

func TestTaskListIsBoundToConfiguredSourceAndAssignee(t *testing.T) {
	client := taskClient(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost || req.URL.Path != "/v1/data_sources/"+testTaskDataSource+"/query" {
			t.Errorf("request = %s %s", req.Method, req.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		filter := body["filter"].(map[string]any)
		people := filter["people"].(map[string]any)
		if filter["property"] != "Owner" || people["contains"] != testTaskAssignee {
			t.Errorf("filter = %#v", filter)
		}
		_, _ = io.WriteString(w, `{"results":[]}`)
	})
	tool := client.TaskTool(testTaskDataSource, "Owner", testTaskAssignee, "Status")
	if got := tool.Run([]byte(`{"operation":"list_assigned"}`)); got.Err != nil {
		t.Fatalf("list assigned: %v", got.Err)
	}
}

func TestTaskReadsRefusePagesOutsideConfiguredSource(t *testing.T) {
	calls := 0
	client := taskClient(func(w http.ResponseWriter, req *http.Request) {
		calls++
		_, _ = io.WriteString(w, `{"id":"`+testTaskPage+`","parent":{"data_source_id":"44444444-4444-4444-8444-444444444444"}}`)
	})
	tool := client.TaskTool(testTaskDataSource, "Owner", testTaskAssignee, "Status")
	got := tool.Run([]byte(`{"operation":"get","task_id":"` + testTaskPage + `"}`))
	if got.Err == nil || !strings.Contains(got.Err.Error(), "outside the configured task data source") || calls != 1 {
		t.Fatalf("get = %+v; calls=%d", got, calls)
	}
}

func TestTaskCreateUsesConfiguredParentAndAssignee(t *testing.T) {
	client := taskClient(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/v1/data_sources/" + testTaskDataSource:
			_, _ = io.WriteString(w, `{"properties":{"Name":{"type":"title"},"Owner":{"type":"people"},"Status":{"type":"status"}}}`)
		case "/v1/pages":
			var body map[string]any
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			parent := body["parent"].(map[string]any)
			properties := body["properties"].(map[string]any)
			owner := properties["Owner"].(map[string]any)["people"].([]any)[0].(map[string]any)
			if parent["data_source_id"] != testTaskDataSource || owner["id"] != testTaskAssignee {
				t.Errorf("parent=%v owner=%v", parent, owner)
			}
			_, _ = io.WriteString(w, `{"id":"created"}`)
		default:
			t.Errorf("unexpected request path %s", req.URL.Path)
		}
	})
	tool := client.TaskTool(testTaskDataSource, "Owner", testTaskAssignee, "Status")
	if got := tool.Run([]byte(`{"operation":"create","title":"A task"}`)); got.Err != nil {
		t.Fatalf("create: %v", got.Err)
	}
}

func TestTaskCommentHonorsDenyModeAndUnknownOutcome(t *testing.T) {
	calls := 0
	client := taskClient(func(w http.ResponseWriter, req *http.Request) {
		calls++
		if req.URL.Path == "/v1/pages/"+testTaskPage {
			_, _ = io.WriteString(w, `{"parent":{"data_source_id":"`+testTaskDataSource+`"}}`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	})
	tool := client.TaskTool(testTaskDataSource, "Owner", testTaskAssignee, "Status")
	client.Approval = tools.ApprovalDeny
	denied := tool.Run([]byte(`{"operation":"comment","task_id":"` + testTaskPage + `","comment":"progress"}`))
	if denied.Err == nil || calls != 0 {
		t.Fatalf("deny result=%+v; calls=%d", denied, calls)
	}
	client.Approval = tools.ApprovalAllow
	got := tool.Run([]byte(`{"operation":"comment","task_id":"` + testTaskPage + `","comment":"progress"}`))
	if got.Err == nil || !strings.Contains(got.Err.Error(), "outcome is unknown") || calls != 2 {
		t.Fatalf("comment result=%+v; calls=%d", got, calls)
	}
}
