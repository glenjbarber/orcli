package groq

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/glenjbarber/orcli/internal/openrouter"
)

// serving returns a Client pointed at a server that answers every request
// with the given status and body, and an accessor for the request it
// received. It mirrors internal/openrouter's own serving helper, white-box
// pointing baseURL at an httptest.Server the same way.
func serving(t *testing.T, status int, body string) (*Client, func() *http.Request) {
	t.Helper()

	var mu sync.Mutex
	var seen *http.Request

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	c := New("gsk-test")
	c.baseURL = srv.URL

	return c, func() *http.Request {
		mu.Lock()
		defer mu.Unlock()
		return seen
	}
}

// gather collects the events a call produced, in order.
func gather(t *testing.T, c *Client, req openrouter.Request) []openrouter.Event {
	t.Helper()

	var got []openrouter.Event
	if err := c.Chat(context.Background(), req, func(e openrouter.Event) {
		got = append(got, e)
	}); err != nil {
		t.Fatalf("Chat returned %v, want nil: a failure reaches the reader as an event", err)
	}
	return got
}

// reply is the assembled text of the deltas.
func reply(events []openrouter.Event) string {
	var b strings.Builder
	for _, e := range events {
		if e.Kind == openrouter.EventDelta {
			b.WriteString(e.Text)
		}
	}
	return b.String()
}

// firstError returns the first failure reported, or nil.
func firstError(events []openrouter.Event) error {
	for _, e := range events {
		if e.Kind == openrouter.EventError {
			return e.Err
		}
	}
	return nil
}

// last returns the last event of the given kind, and whether one was found.
func last(events []openrouter.Event, kind openrouter.EventKind) (openrouter.Event, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind == kind {
			return events[i], true
		}
	}
	return openrouter.Event{}, false
}

// TestChatStreamsTheReply is the ordinary path, end to end through a server.
func TestChatStreamsTheReply(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"hello \"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"there\"}}]}\n\n" +
		"data: [DONE]\n\n"

	c, _ := serving(t, http.StatusOK, body)
	events := gather(t, c, openrouter.Request{Model: "m", Stream: true})

	if got, want := reply(events), "hello there"; got != want {
		t.Errorf("reply is %q, want %q", got, want)
	}

	finish, ok := last(events, openrouter.EventFinish)
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

// TestChatSendsTheRequest checks what the request must carry: the right
// path, the right method, and the bearer credential, against Groq's own
// OpenAI-compatible host rather than OpenRouter's.
func TestChatSendsTheRequest(t *testing.T) {
	c, request := serving(t, http.StatusOK, "data: [DONE]\n\n")

	req := openrouter.Request{
		Model:    "a-groq-model",
		Messages: []openrouter.Message{{Role: "user", Content: "a question"}},
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
	if auth := seen.Header.Get("Authorization"); !strings.Contains(auth, "gsk-test") {
		t.Errorf("the request did not carry the credential: %q", auth)
	}
}

// TestChatWithNoKeyIsRefusedLocally covers the same local check
// [openrouter.Client.Chat] makes: a request with no credential is caught
// before it is sent, since the backend cannot tell a missing key from an
// invalid one.
func TestChatWithNoKeyIsRefusedLocally(t *testing.T) {
	c := New("")
	err := c.Chat(context.Background(), openrouter.Request{}, func(openrouter.Event) {})
	if err != ErrNoAPIKey {
		t.Errorf("Chat with no key returned %v, want ErrNoAPIKey", err)
	}
}

// TestChatRequiresAnEventCallback covers the same local check
// [openrouter.Client.Chat] makes for a nil callback.
func TestChatRequiresAnEventCallback(t *testing.T) {
	c := New("gsk-test")
	err := c.Chat(context.Background(), openrouter.Request{}, nil)
	if err == nil {
		t.Fatal("Chat with a nil callback returned nil, want an error")
	}
}

// TestChatReportsAToolCall covers tool call assembly across chunks, the same
// case internal/openrouter's own stream tests cover for its own assembler,
// proving the reused openrouter.ToolCall shape round-trips through this
// client's own copy of the assembler.
func TestChatReportsAToolCall(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\"," +
		"\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":" +
		"{\"arguments\":\"{\\\"city\\\":\\\"nyc\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n"

	c, _ := serving(t, http.StatusOK, body)
	events := gather(t, c, openrouter.Request{Stream: true})

	var calls []openrouter.ToolCall
	for _, e := range events {
		if e.Kind == openrouter.EventTool && e.ToolCall != nil {
			calls = append(calls, *e.ToolCall)
		}
	}
	if len(calls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(calls))
	}
	if calls[0].ID != "call_1" || calls[0].Function.Name != "get_weather" {
		t.Errorf("tool call decoded as %+v", calls[0])
	}
	if calls[0].Function.Arguments != `{"city":"nyc"}` {
		t.Errorf("arguments are %q, want the joined fragments", calls[0].Function.Arguments)
	}
}

// TestChatReportsAnEndpointRefusal covers a non-200 response: the caller is
// shown what the endpoint said rather than a decode failure over an error
// body.
func TestChatReportsAnEndpointRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":{"message":"invalid api key"}}`)
	}))
	t.Cleanup(srv.Close)

	c := New("gsk-test")
	c.baseURL = srv.URL

	events := gather(t, c, openrouter.Request{Stream: true})
	err := firstError(events)
	if err == nil {
		t.Fatal("a 401 was reported as a success")
	}
	if !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("the error is %q, want it to quote the endpoint", err)
	}
}

// TestChatCutShortIsNotReportedAsFinished covers the one outcome this
// package exists to avoid presenting as a complete reply: a stream that ends
// without its [DONE] terminator.
func TestChatCutShortIsNotReportedAsFinished(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"

	c, _ := serving(t, http.StatusOK, body)
	events := gather(t, c, openrouter.Request{Stream: true})

	if got, want := reply(events), "partial"; got != want {
		t.Errorf("reply is %q, want %q: the text before the cut must be kept", got, want)
	}

	finish, ok := last(events, openrouter.EventFinish)
	if !ok {
		t.Fatal("no finish event, want one")
	}
	if finish.Finished {
		t.Error("Finished is true, want false: the stream never reached its marker")
	}
	if firstError(events) == nil {
		t.Error("no error reported for a stream that ended without its terminator")
	}
}
