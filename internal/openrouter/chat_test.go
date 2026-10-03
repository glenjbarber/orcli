package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// serving returns a Client pointed at a server that answers every request with
// the given status and body, and an accessor for the request it received.
//
// The request is reached through a function rather than returned as a value,
// because the server has not been called yet when this returns. Returning the
// request itself would hand back a nil that no amount of waiting could fill.
func serving(t *testing.T, status int, body string) (*Client, func() *http.Request) {
	t.Helper()

	var mu sync.Mutex
	var seen *http.Request

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The request is cloned before it is stored, because the handler reads
		// the body and a stored request would otherwise be drained by whatever
		// assertion looked at it afterwards.
		clone := r.Clone(context.Background())
		clone.Body = nil

		mu.Lock()
		seen = clone
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	c := New("sk-or-v1-test")
	c.baseURL = srv.URL

	return c, func() *http.Request {
		mu.Lock()
		defer mu.Unlock()
		return seen
	}
}

// gather collects the events a call produced, in order.
func gather(t *testing.T, c *Client, req Request) []Event {
	t.Helper()

	var got []Event
	if err := c.Chat(context.Background(), req, func(e Event) {
		got = append(got, e)
	}); err != nil {
		t.Fatalf("Chat returned %v, want nil: a failure reaches the reader as an event", err)
	}
	return got
}

// reply is the assembled text of the deltas.
func reply(events []Event) string {
	var b strings.Builder
	for _, e := range events {
		if e.Kind == EventDelta {
			b.WriteString(e.Text)
		}
	}
	return b.String()
}

// firstError returns the first failure reported, or nil.
func firstError(events []Event) error {
	for _, e := range events {
		if e.Kind == EventError {
			return e.Err
		}
	}
	return nil
}

// TestChatStreamsTheReply is the ordinary path, end to end through a server.
//
// Everything the stream parser does is already covered against a reader. This
// covers what only the request path can: that a real response body reaches the
// parser at all.
func TestChatStreamsTheReply(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"hello \"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"there\"}}]}\n\n" +
		"data: [DONE]\n\n"

	c, _ := serving(t, http.StatusOK, body)
	events := gather(t, c, Request{Model: "m", Stream: true})

	if got, want := reply(events), "hello there"; got != want {
		t.Errorf("reply is %q, want %q", got, want)
	}

	finish, ok := last(events, EventFinish)
	if !ok {
		t.Fatal("no finish event, want one")
	}
	if !finish.Finished {
		t.Error("Finished is false, want true: the stream reached its marker")
	}
	if err := firstError(events); err != nil {
		t.Errorf("a failure was reported for a well formed reply: %v", err)
	}
}

// TestChatSendsTheRequest checks what the request must carry.
//
// A client that posted to the wrong path, or without the credential, would fail
// against the real endpoint in a way no reply-side test would catch.
func TestChatSendsTheRequest(t *testing.T) {
	c, request := serving(t, http.StatusOK, "data: [DONE]\n\n")

	req := Request{
		Model:    "stealth/space-bunny-alpha",
		Messages: []Message{{Role: "user", Content: "a question"}},
		Stream:   true,
	}
	gather(t, c, req)

	seen := request()
	if seen == nil {
		t.Fatal("the server received no request")
	}
	if seen.Method != http.MethodPost {
		t.Errorf("method is %s, want POST", seen.Method)
	}
	if got, want := seen.URL.Path, "/chat/completions"; got != want {
		t.Errorf("path is %q, want %q", got, want)
	}
	if got, want := seen.Header.Get("Authorization"), "Bearer sk-or-v1-test"; got != want {
		t.Errorf("authorization is %q, want %q", got, want)
	}
	if got, want := seen.Header.Get("Content-Type"), "application/json"; got != want {
		t.Errorf("content type is %q, want %q", got, want)
	}
	if got, want := seen.Header.Get("Accept"), "text/event-stream"; got != want {
		t.Errorf("accept is %q, want %q", got, want)
	}
}

// TestChatSendsTheConversation checks that the request body carries what the
// endpoint expects, since a conversation that silently lost a turn would be
// reported by a model as a question it was never asked.
func TestChatSendsTheConversation(t *testing.T) {
	type sent struct {
		Model    string    `json:"model"`
		Stream   bool      `json:"stream"`
		Messages []Message `json:"messages"`
	}

	var body sent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	c := New("key")
	c.baseURL = srv.URL

	req := Request{
		Model:  "m",
		Stream: true,
		Messages: []Message{
			{Role: "system", Content: "instructions"},
			{Role: "user", Content: "the question"},
		},
	}
	gather(t, c, req)

	if body.Model != "m" {
		t.Errorf("model is %q, want %q", body.Model, "m")
	}
	if !body.Stream {
		t.Error("stream is false, want true: a streamed reply is what this client reads")
	}
	if len(body.Messages) != 2 {
		t.Fatalf("sent %d messages, want 2", len(body.Messages))
	}
	if body.Messages[0].Role != "system" || body.Messages[1].Role != "user" {
		t.Errorf("messages arrived as %q and %q, want system then user",
			body.Messages[0].Role, body.Messages[1].Role)
	}
	if body.Messages[1].Content != "the question" {
		t.Errorf("the question arrived as %q", body.Messages[1].Content)
	}
}

// TestChatKeepsTheStreamUnreadOnSuccess covers the rule that decides whether the
// reply can be read at all.
//
// The body on a successful response is the stream. A client that read it to
// check for an error would consume it, and the parser would see an empty body
// and report a cut stream for a reply that arrived whole.
func TestChatKeepsTheStreamUnreadOnSuccess(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"intact\"}}]}\n\n" +
		"data: [DONE]\n\n"

	c, _ := serving(t, http.StatusOK, body)
	events := gather(t, c, Request{Stream: true})

	if got, want := reply(events), "intact"; got != want {
		t.Errorf("reply is %q, want %q: the body must reach the parser unconsumed", got, want)
	}
}

// TestChatReportsAnErrorStatus covers the path where the endpoint refuses.
//
// The body is read and quoted on this path, and it is quoted because a reader
// shown nothing but a status has been told what happened but not why.
func TestChatReportsAnErrorStatus(t *testing.T) {
	body := `{"error":{"message":"no such model","code":404}}`
	c, _ := serving(t, http.StatusNotFound, body)

	events := gather(t, c, Request{Model: "nope", Stream: true})

	err := firstError(events)
	if err == nil {
		t.Fatal("no failure was reported for a 404, want one")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("failure is %q, want it to carry the status", err)
	}
	if !strings.Contains(err.Error(), "no such model") {
		t.Errorf("failure is %q, want it to quote the body", err)
	}
}

// TestChatBoundsTheErrorBody covers the limit on what is read from an error.
//
// A body from an endpoint is not a thing to be trusted about its length, and
// what is read is shown to a reader and ends up in terminal scrollback.
func TestChatBoundsTheErrorBody(t *testing.T) {
	c, _ := serving(t, http.StatusInternalServerError, strings.Repeat("x", 4096))

	err := firstError(gather(t, c, Request{Stream: true}))
	if err == nil {
		t.Fatal("no failure was reported, want one")
	}
	if len(err.Error()) > 600 {
		t.Errorf("failure is %d bytes, want it bounded near the 512 byte limit",
			len(err.Error()))
	}
}

// TestChatRedactsTheCredentialInAnErrorBody is the reason every diagnostic goes
// through Filter.
//
// The credential travels in a header, so it is in no body this client builds,
// but a server or a proxy can quote it back, and this failure is shown in a
// pane and scrolls into the terminal. The figure redacted is the one the client
// holds, since that is the only figure Filter can recognize.
func TestChatRedactsTheCredentialInAnErrorBody(t *testing.T) {
	// This is the credential the client under test carries, so it is the one
	// the server below is made to quote back.
	const key = "sk-or-v1-test"
	c, _ := serving(t, http.StatusUnauthorized, `{"error":"bad key `+key+`"}`)

	err := firstError(gather(t, c, Request{Stream: true}))
	if err == nil {
		t.Fatal("no failure was reported, want one")
	}
	if strings.Contains(err.Error(), key) {
		t.Errorf("the failure quotes the credential: %q", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Errorf("failure is %q, want the credential marked as redacted", err)
	}
}

// TestChatReportsAnEmptyErrorBody covers a status with nothing to quote.
//
// The status alone is the report, and a failure carrying only a status is better
// than a silent one.
func TestChatReportsAnEmptyErrorBody(t *testing.T) {
	c, _ := serving(t, http.StatusTooManyRequests, "")

	err := firstError(gather(t, c, Request{Stream: true}))
	if err == nil {
		t.Fatal("no failure was reported, want one")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("failure is %q, want it to carry the status", err)
	}
}

// TestChatReportsACutStreamThroughTheServer covers the property this package is
// built around, end to end.
//
// A connection that closes without the marker is what a cut stream looks like
// from here, and the reply that did arrive is kept rather than thrown away with
// the failure.
func TestChatReportsACutStreamThroughTheServer(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"half an \"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"answer\"}}]}\n\n"

	c, _ := serving(t, http.StatusOK, body)
	events := gather(t, c, Request{Stream: true})

	if got, want := reply(events), "half an answer"; got != want {
		t.Errorf("reply is %q, want %q: text before a cut is kept", got, want)
	}
	finish, ok := last(events, EventFinish)
	if !ok {
		t.Fatal("no finish event, want one")
	}
	if finish.Finished {
		t.Error("Finished is true for a stream with no marker, want false")
	}
	if firstError(events) == nil {
		t.Error("no failure was reported for a cut stream, want one")
	}
}

// TestChatReportsToolCallsThroughTheServer covers the tool path end to end.
//
// The fragments arrive over the wire as the endpoint frames them, and the
// assembled call is what the interface would hand to a tool.
func TestChatReportsToolCallsThroughTheServer(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_a\"," +
		"\"type\":\"function\",\"function\":{\"name\":\"read_file\"," +
		"\"arguments\":\"{\\\"path\\\":\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0," +
		"\"function\":{\"arguments\":\"\\\"x\\\"}\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n"

	c, _ := serving(t, http.StatusOK, body)
	events := gather(t, c, Request{Stream: true})

	var calls []ToolCall
	for _, e := range events {
		if e.Kind == EventTool {
			calls = append(calls, *e.ToolCall)
		}
	}
	if len(calls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(calls))
	}
	if got, want := calls[0].Function.Name, "read_file"; got != want {
		t.Errorf("name is %q, want %q", got, want)
	}
	if got, want := calls[0].Function.Arguments, `{"path":"x"}`; got != want {
		t.Errorf("arguments are %q, want %q", got, want)
	}
	finish, ok := last(events, EventFinish)
	if !ok {
		t.Fatal("no finish event, want one")
	}
	if got, want := finish.Reason, "tool_calls"; got != want {
		t.Errorf("finish reason is %q, want %q", got, want)
	}
}

// TestChatReportsUsageThroughTheServer covers the accounting arriving with the
// last payload of a turn.
func TestChatReportsUsageThroughTheServer(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]," +
		"\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":4," +
		"\"total_tokens\":14,\"cost\":0.0007}}\n\n" +
		"data: [DONE]\n\n"

	c, _ := serving(t, http.StatusOK, body)
	events := gather(t, c, Request{Stream: true})

	usage, ok := find(events, EventUsage)
	if !ok {
		t.Fatal("no usage event, want one")
	}
	if got, want := *usage.Usage.PromptTokens, 10; got != want {
		t.Errorf("prompt count is %d, want %d", got, want)
	}
	if got, want := *usage.Usage.TotalTokens, 14; got != want {
		t.Errorf("total count is %d, want %d", got, want)
	}
	if got, want := *usage.Usage.Cost, 0.0007; got != want {
		t.Errorf("cost is %v, want %v", got, want)
	}
}

// TestChatKeepsTextWhenTheServerRefusesMidStream covers a failure arriving after
// the reply began.
//
// The endpoint can accept a request and then fail while streaming it, and the
// words already on the wire are usually more useful than the failure alone.
func TestChatKeepsTextWhenTheServerRefusesMidStream(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"arrived\"}}]}\n\n" +
		"data: {not a payload}\n\n"

	c, _ := serving(t, http.StatusOK, body)
	events := gather(t, c, Request{Stream: true})

	if got, want := reply(events), "arrived"; got != want {
		t.Errorf("reply is %q, want %q", got, want)
	}
	if firstError(events) == nil {
		t.Error("no failure was reported for an unreadable payload, want one")
	}
}

// TestChatStopsOnACancelledContext covers the reader stopping a turn.
//
// A turn runs on its own context so that stopping the model stops the request
// and not the session. A cancelled context must therefore end the request.
func TestChatStopsOnACancelledContext(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"started\"}}]}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-release
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})

	c := New("key")
	c.baseURL = srv.URL

	ctx, cancel := context.WithCancel(context.Background())

	var events []Event
	var mu sync.Mutex
	done := make(chan error, 1)

	go func() {
		done <- c.Chat(ctx, Request{Stream: true}, func(e Event) {
			mu.Lock()
			events = append(events, e)
			mu.Unlock()
		})
	}()

	// The cancellation is given a moment to land after the server has answered,
	// so the test exercises a request that began rather than one refused at the
	// door.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("Chat returned %v, want nil or context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Chat did not return after the context was cancelled")
	}

	mu.Lock()
	defer mu.Unlock()
	if got, want := reply(events), "started"; got != want {
		t.Errorf("reply is %q, want %q: the text that arrived is kept", got, want)
	}
}

// TestChatSendsToolsWhenOffered checks that a tool offered reaches the endpoint
// in the shape it expects.
//
// The interface decides whether to offer a tool, and this is where the offering
// becomes a request.
func TestChatSendsToolsWhenOffered(t *testing.T) {
	type sent struct {
		Tools []struct {
			Type     string `json:"type"`
			Function struct {
				Name        string         `json:"name"`
				Description string         `json:"description"`
				Parameters  map[string]any `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
	}

	var body sent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	c := New("key")
	c.baseURL = srv.URL

	tool := Tool{
		Type: "function",
		Function: ToolFunction{
			Name:        "read_file",
			Description: "read a file",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"path": map[string]any{"type": "string"}},
			},
		},
	}
	gather(t, c, Request{Model: "m", Stream: true, Tools: []Tool{tool}})

	if len(body.Tools) != 1 {
		t.Fatalf("sent %d tools, want 1", len(body.Tools))
	}
	if got, want := body.Tools[0].Function.Name, "read_file"; got != want {
		t.Errorf("tool name is %q, want %q", got, want)
	}
	if got, want := body.Tools[0].Type, "function"; got != want {
		t.Errorf("tool type is %q, want %q", got, want)
	}
	if body.Tools[0].Function.Parameters["type"] != "object" {
		t.Errorf("tool parameters were not sent: %v", body.Tools[0].Function.Parameters)
	}
}

// TestChatOmitsToolsWhenNoneAreOffered covers that a request without tools does
// not carry an empty list.
//
// The endpoint treats the field as an offering, and an empty offering is not
// the same as no offering.
func TestChatOmitsToolsWhenNoneAreOffered(t *testing.T) {
	var raw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	c := New("key")
	c.baseURL = srv.URL
	gather(t, c, Request{Model: "m", Stream: true})

	if strings.Contains(string(raw), `"tools"`) {
		t.Errorf("a request without tools carried the field: %s", raw)
	}
}

// TestChatReplaysAToolTurn checks that the shape the endpoint expects for a
// round that called a tool is what this client sends.
//
// A round that called a tool is replayed as an assistant turn carrying the call
// and a tool-role turn carrying its answer. Sending either half is a request
// the endpoint cannot answer.
func TestChatReplaysAToolTurn(t *testing.T) {
	type sent struct {
		Messages []Message `json:"messages"`
	}

	var body sent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	c := New("key")
	c.baseURL = srv.URL

	req := Request{
		Model:  "m",
		Stream: true,
		Messages: []Message{
			{Role: "user", Content: "read x"},
			{
				Role:    "assistant",
				Content: "",
				ToolCalls: []ToolCall{{
					ID:       "call_a",
					Type:     "function",
					Index:    0,
					Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"x"}`},
				}},
			},
			{Role: "tool", Content: "the contents", Name: "read_file"},
		},
	}
	gather(t, c, req)

	if len(body.Messages) != 3 {
		t.Fatalf("sent %d messages, want 3", len(body.Messages))
	}
	if len(body.Messages[1].ToolCalls) != 1 {
		t.Errorf("the assistant turn carried %d tool calls, want 1",
			len(body.Messages[1].ToolCalls))
	}
	if got, want := body.Messages[1].ToolCalls[0].Function.Arguments, `{"path":"x"}`; got != want {
		t.Errorf("arguments were sent as %q, want %q", got, want)
	}
	if got, want := body.Messages[2].Role, "tool"; got != want {
		t.Errorf("the answer arrived as role %q, want %q", got, want)
	}
	if got, want := body.Messages[2].Content, "the contents"; got != want {
		t.Errorf("the answer arrived as %q, want %q", got, want)
	}
}
