package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeHTTPServer is a minimal Streamable HTTP MCP server: it answers
// initialize, resources/list and resources/read, assigns a session ID on
// initialize, and records every request it saw so a test can assert on
// headers and session continuity.
type fakeHTTPServer struct {
	mu       sync.Mutex
	requests []*http.Request
	sseMode  bool

	sessionID    string
	deleteCalled int32
}

func newFakeHTTPServer(sseMode bool) *fakeHTTPServer {
	return &fakeHTTPServer{sseMode: sseMode, sessionID: "session-abc-123"}
}

func (f *fakeHTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	// Headers are read now, before the body is consumed, and the request
	// itself is cloned-by-reference for later inspection - fine here since
	// nothing in this test server mutates it afterward.
	f.requests = append(f.requests, r)
	f.mu.Unlock()

	if r.Method == http.MethodDelete {
		atomic.AddInt32(&f.deleteCalled, 1)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	var req rpcRequest
	body, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// A notification (no reply expected) is identified here the same way a
	// real server would: no "id" member at all. rpcRequest.ID is int64 and
	// decodes to 0 either way, so the raw body is checked instead.
	if !strings.Contains(string(body), `"id"`) {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	var result any
	switch req.Method {
	case "initialize":
		w.Header().Set(mcpSessionHeader, f.sessionID)
		result = map[string]any{"protocolVersion": protocolVersion}
	case "resources/list":
		result = listResourcesResult{Resources: []Resource{{URI: "mem://a", Name: "a"}}}
	case "resources/read":
		result = readResourceResult{Contents: []resourceContent{{URI: "mem://a", Text: "hello from remote"}}}
	default:
		http.Error(w, "unknown method", http.StatusNotFound)
		return
	}

	resultBytes, _ := json.Marshal(result)
	resp := rpcResponse{JSONRPC: "2.0", ID: json.RawMessage(fmt.Sprintf("%d", req.ID)), Result: resultBytes}
	respBytes, _ := json.Marshal(resp)

	if f.sseMode {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", respBytes)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(respBytes)
}

func (f *fakeHTTPServer) lastRequest() *http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

func TestHTTPTransport_JSONResponseMode(t *testing.T) {
	fake := newFakeHTTPServer(false)
	ts := httptest.NewServer(fake)
	defer ts.Close()

	tr := newHTTPTransport(ts.URL, map[string]string{"Authorization": "Bearer test-token"})
	raw, err := tr.call("resources/list", map[string]any{})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var result listResourcesResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Resources) != 1 || result.Resources[0].URI != "mem://a" {
		t.Fatalf("result = %+v", result)
	}
	if got := fake.lastRequest().Header.Get("Authorization"); got != "Bearer test-token" {
		t.Errorf("Authorization header = %q", got)
	}
}

func TestHTTPTransport_SSEResponseMode(t *testing.T) {
	fake := newFakeHTTPServer(true)
	ts := httptest.NewServer(fake)
	defer ts.Close()

	tr := newHTTPTransport(ts.URL, nil)
	raw, err := tr.call("resources/read", map[string]any{"uri": "mem://a"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var result readResourceResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Contents) != 1 || result.Contents[0].Text != "hello from remote" {
		t.Fatalf("result = %+v", result)
	}
}

func TestHTTPTransport_SessionIDPropagatesAfterInitialize(t *testing.T) {
	fake := newFakeHTTPServer(false)
	ts := httptest.NewServer(fake)
	defer ts.Close()

	tr := newHTTPTransport(ts.URL, nil)
	if _, err := tr.call("initialize", map[string]any{}); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if tr.session() != "session-abc-123" {
		t.Fatalf("session() = %q, want the server-assigned id", tr.session())
	}
	if _, err := tr.call("resources/list", map[string]any{}); err != nil {
		t.Fatalf("resources/list: %v", err)
	}
	if got := fake.lastRequest().Header.Get(mcpSessionHeader); got != "session-abc-123" {
		t.Errorf("the second request did not carry the session id, got %q", got)
	}
}

func TestHTTPTransport_NotifyAcceptsA202WithNoBody(t *testing.T) {
	fake := newFakeHTTPServer(false)
	ts := httptest.NewServer(fake)
	defer ts.Close()

	tr := newHTTPTransport(ts.URL, nil)
	if err := tr.notify("notifications/initialized", map[string]any{}); err != nil {
		t.Fatalf("notify: %v", err)
	}
}

func TestHTTPTransport_NonSuccessStatusIsReported(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer ts.Close()

	tr := newHTTPTransport(ts.URL, nil)
	_, err := tr.call("resources/list", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("call() = %v, want an error naming the 403 status", err)
	}
}

func TestHTTPTransport_RPCErrorObjectIsReported(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &req)
		resp := rpcResponse{JSONRPC: "2.0", ID: json.RawMessage(fmt.Sprintf("%d", req.ID)), Error: &rpcError{Code: -32601, Message: "method not found"}}
		respBytes, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.Write(respBytes)
	}))
	defer ts.Close()

	tr := newHTTPTransport(ts.URL, nil)
	_, err := tr.call("resources/list", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "method not found") {
		t.Fatalf("call() = %v, want the JSON-RPC error message", err)
	}
}

func TestHTTPTransport_CloseSendsDeleteOnlyWhenASessionExists(t *testing.T) {
	fake := newFakeHTTPServer(false)
	ts := httptest.NewServer(fake)
	defer ts.Close()

	tr := newHTTPTransport(ts.URL, nil)
	if err := tr.Close(); err != nil {
		t.Fatalf("Close() with no session established: %v", err)
	}
	if atomic.LoadInt32(&fake.deleteCalled) != 0 {
		t.Error("Close sent DELETE despite no session ever being assigned")
	}

	if _, err := tr.call("initialize", map[string]any{}); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("Close() after a session was assigned: %v", err)
	}
	if atomic.LoadInt32(&fake.deleteCalled) != 1 {
		t.Errorf("deleteCalled = %d, want 1", fake.deleteCalled)
	}
}

// TestRemoteServerEndToEnd proves a Server naming a URL works through the
// Manager exactly the way a local one does: initialize, list, and read, all
// through client's own transport-agnostic protocol methods.
func TestRemoteServerEndToEnd(t *testing.T) {
	fake := newFakeHTTPServer(false)
	ts := httptest.NewServer(fake)
	defer ts.Close()

	m := NewManager([]Server{{Name: "remote", URL: ts.URL}})
	defer m.Close()

	c, err := m.get("remote")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resources, err := c.listResources()
	if err != nil {
		t.Fatalf("listResources: %v", err)
	}
	if len(resources) != 1 || resources[0].URI != "mem://a" {
		t.Fatalf("listResources() = %+v", resources)
	}
	text, err := c.readResource("mem://a")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}
	if text != "hello from remote" {
		t.Fatalf("readResource() = %q", text)
	}
}

func TestDecodeSSEResponse_MultiLineData(t *testing.T) {
	raw := "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\n" +
		"data: \"result\":{}}\n\n"
	resp, err := decodeSSEResponse(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("decodeSSEResponse: %v", err)
	}
	if resp.JSONRPC != "2.0" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestDecodeSSEResponse_NoDataFieldIsAnError(t *testing.T) {
	_, err := decodeSSEResponse(strings.NewReader("event: ping\n\n"))
	if err == nil {
		t.Fatal("expected an error for a stream with no data field")
	}
}
