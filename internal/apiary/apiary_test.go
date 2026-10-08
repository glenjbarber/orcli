package apiary

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQueryUsesOnlyFixedGETRoutesAndRedactsToken(t *testing.T) {
	token := "apk_test_secret"
	paths := map[string]string{"status": "/v1/status", "health": "/v1/health", "vms": "/v1/vms", "vm": "/v1/vms/vm-1", "jails": "/v1/jails", "jail": "/v1/jails/jail-1", "networks": "/v1/networks"}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("missing Viewer bearer header")
		}
		_, _ = io.WriteString(w, `{"key":"`+token+`"}`)
	}))
	defer s.Close()
	c := New(s.URL, token)
	for query, id := range map[string]string{"status": "", "health": "", "vms": "", "vm": "vm-1", "jails": "", "jail": "jail-1", "networks": ""} {
		path := paths[query]
		c.HTTP = httpClientCheckingPath(t, s, path)
		args, _ := json.Marshal(map[string]string{"query": query, "id": id})
		got := c.ToolSet()[0].Run(args)
		if got.Err != nil {
			t.Fatalf("%s: %v", query, got.Err)
		}
		if strings.Contains(got.Content, token) || !strings.Contains(got.Content, "[redacted]") {
			t.Fatalf("%s leaked token: %s", query, got.Content)
		}
	}
}

func httpClientCheckingPath(t *testing.T, server *httptest.Server, want string) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != want {
			t.Errorf("path = %s, want %s", r.URL.Path, want)
		}
		return http.DefaultTransport.RoundTrip(r)
	})}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestQueryRejectsUnknownAndInvalidDetail(t *testing.T) {
	for _, raw := range []string{`{"query":"delete"}`, `{"query":"vm"}`, `{"query":"jail","id":"../vms"}`} {
		got := New("http://127.0.0.1:1", "apk_test").ToolSet()[0].Run(json.RawMessage(raw))
		if got.Err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestQueryLimitsResponseAndKeepsHTTPFailureBodyPrivate(t *testing.T) {
	for _, handler := range []http.HandlerFunc{
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(403)
			_, _ = io.WriteString(w, "sensitive response")
		},
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, strings.Repeat("x", maxResponse+1))
		},
	} {
		s := httptest.NewServer(handler)
		c := New(s.URL, "apk_secret")
		got := c.ToolSet()[0].Run(json.RawMessage(`{"query":"status"}`))
		s.Close()
		if got.Err == nil || strings.Contains(got.Err.Error(), "sensitive response") || strings.Contains(got.Err.Error(), "apk_secret") {
			t.Fatalf("unsafe result: %+v", got)
		}
	}
}
