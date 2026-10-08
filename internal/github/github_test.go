package github

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glenjbarber/orcli/internal/tools"
)

func TestListAssignedUsesOnlyAllowlistedRepositoriesAndFiltersPullRequests(t *testing.T) {
	const token = "github-test-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/repos/owner/allowed/issues" {
			t.Errorf("request = %s %s, want configured issue list", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("assignee") != "glenjbarber" || r.URL.Query().Get("state") != "open" {
			t.Errorf("query = %v, want configured assignee and open state", r.URL.Query())
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("GitHub request did not carry the configured bearer token")
		}
		_, _ = io.WriteString(w, `[{"number":1,"title":"task"},{"number":2,"title":"pull","pull_request":{"url":"https://example.test"}}]`)
	}))
	defer server.Close()
	client := New(token, "glenjbarber", []string{"owner/allowed"})
	client.Base = server.URL

	got := client.ToolSet()[0].Run(json.RawMessage(`{"operation":"list_assigned"}`))
	if got.Err != nil {
		t.Fatalf("list_assigned: %v", got.Err)
	}
	if strings.Contains(got.Content, token) || !strings.Contains(got.Content, `"number":1`) || strings.Contains(got.Content, `"number":2`) {
		t.Fatalf("unexpected or sensitive list result: %s", got.Content)
	}
}

func TestWritesStayInTheAllowlistAndHonorDenyMode(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/repos/owner/allowed/issues" || r.Method != http.MethodPost {
			t.Errorf("request = %s %s, want create in allowlisted repo", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["title"] != "new task" {
			t.Errorf("create body = %#v, error %v", body, err)
		}
		_, _ = io.WriteString(w, `{"number":7,"title":"new task"}`)
	}))
	defer server.Close()
	client := New("token", "glenjbarber", []string{"owner/allowed"})
	client.Base = server.URL
	tool := client.ToolSet()[0]

	outside := tool.Run(json.RawMessage(`{"operation":"create","repo":"owner/private","title":"new task"}`))
	if outside.Err == nil || calls != 0 {
		t.Fatalf("unlisted repo request = %+v; calls=%d", outside, calls)
	}

	client.Approval = tools.ApprovalDeny
	denied := tool.Run(json.RawMessage(`{"operation":"create","repo":"owner/allowed","title":"new task"}`))
	if denied.Err == nil || calls != 0 {
		t.Fatalf("deny mode request = %+v; calls=%d", denied, calls)
	}

	client.Approval = tools.ApprovalAllow
	created := tool.Run(json.RawMessage(`{"operation":"create","repo":"owner/allowed","title":"new task"}`))
	if created.Err != nil || !strings.Contains(created.Content, `"number":7`) || calls != 1 {
		t.Fatalf("create = %+v; calls=%d", created, calls)
	}
}

func TestWriteFailureReportsUnknownOutcomeWithoutRetry(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	client := New("token", "glenjbarber", []string{"owner/repo"})
	client.Base = server.URL
	got := client.ToolSet()[0].Run(json.RawMessage(`{"operation":"comment","repo":"owner/repo","number":3,"body":"note"}`))
	if got.Err == nil || !strings.Contains(got.Err.Error(), "outcome is unknown") || calls != 1 {
		t.Fatalf("comment result = %+v; calls=%d", got, calls)
	}
}

func TestInvalidToolArgumentsAndRepositoryConfigurationAreRefused(t *testing.T) {
	client := New("token", "glenjbarber", []string{"owner/repo"})
	tool := client.ToolSet()[0]
	for _, raw := range []string{`[]`, `{"operation":"get","repo":"owner/repo","number":0}`, `{"operation":"delete","repo":"owner/repo"}`, `{"operation":"get","repo":"owner/repo","number":1,"extra":true}`} {
		if got := tool.Run(json.RawMessage(raw)); got.Err == nil {
			t.Errorf("accepted invalid arguments %s", raw)
		}
	}
	client.Repositories = nil
	if got := tool.Run(json.RawMessage(`{"operation":"list_assigned"}`)); got.Err == nil {
		t.Fatal("task operation worked without an explicit repository allowlist")
	}
}
