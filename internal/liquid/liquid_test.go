package liquid

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/glenjbarber/orcli/internal/openrouter"
)

func testClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	s := httptest.NewServer(handler)
	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	return newClient("synthetic-secret", u, s.Client()), s
}

func TestChatMapsDocumentedRequestAndStreamsToolCalls(t *testing.T) {
	c, s := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer synthetic-secret" {
			t.Errorf("authorization %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if _, ok := body["attribution_id"]; ok {
			t.Error("OpenRouter attribution leaked into Liquid request")
		}
		messages := body["messages"].([]any)
		msg := messages[0].(map[string]any)
		if msg["content"] != "hello" {
			t.Errorf("message content %v", msg["content"])
		}
		if len(messages) != 3 {
			t.Errorf("message count %d", len(messages))
		}
		assistant := messages[1].(map[string]any)
		calls := assistant["tool_calls"].([]any)
		call := calls[0].(map[string]any)
		if _, ok := call["index"]; ok {
			t.Error("stream-only tool index was sent in replay")
		}
		if messages[2].(map[string]any)["tool_call_id"] != "call-old" {
			t.Error("tool result was not paired with its call")
		}
		tools := body["tools"].([]any)
		if tools[0].(map[string]any)["type"] != "function" {
			t.Error("tool declaration missing")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"type\":\"function\",\"function\":{\"name\":\"read\",\"arguments\":\"{\\\"path\\\":\\\"a\\\"}\"}}]}}]}\n\n")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"tool_calls\"}]}\n\n")
		io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":1,\"total_tokens\":3,\"cost\":0}}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	})
	defer s.Close()
	req := openrouter.Request{
		Model: "org/live", Stream: true, AttributionID: "private",
		Messages: []openrouter.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", ToolCalls: []openrouter.ToolCall{{ID: "call-old", Type: "function", Index: 7, Function: openrouter.ToolCallFunction{Name: "read", Arguments: `{"path":"a"}`}}}},
			{Role: "tool", ToolCallID: "call-old", Content: "result"},
		},
		Tools: []openrouter.Tool{{Type: "function", Function: openrouter.ToolFunction{Name: "read", Parameters: map[string]string{"type": "object"}}}},
	}
	var events []openrouter.Event
	if err := c.Chat(context.Background(), req, func(e openrouter.Event) { events = append(events, e) }); err != nil {
		t.Fatal(err)
	}
	var text string
	var tool *openrouter.ToolCall
	var usage *openrouter.Usage
	for _, e := range events {
		switch e.Kind {
		case openrouter.EventDelta:
			text += e.Text
		case openrouter.EventTool:
			tool = e.ToolCall
		case openrouter.EventUsage:
			usage = e.Usage
		}
	}
	if text != "ok" {
		t.Errorf("text %q", text)
	}
	if tool == nil || tool.ID != "call-1" || tool.Function.Name != "read" {
		t.Errorf("tool %#v", tool)
	}
	if usage == nil || usage.Cost == nil || *usage.Cost != 0 {
		t.Errorf("usage %#v", usage)
	}
	if events[len(events)-1].Kind != openrouter.EventFinish || !events[len(events)-1].Finished {
		t.Fatalf("terminal event %#v", events[len(events)-1])
	}
}

func TestStreamFragmentedParallelToolCallsPreserveOrder(t *testing.T) {
	c := New("synthetic-secret")
	stream := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":1,\"id\":\"b\",\"type\":\"function\",\"function\":{\"name\":\"sec\",\"arguments\":\"ond\"}},{\"index\":0,\"id\":\"a\",\"type\":\"function\",\"function\":{\"name\":\"fir\",\"arguments\":\"st\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"st\",\"arguments\":\"!\"}},{\"index\":1,\"function\":{\"name\":\"ond\",\"arguments\":\"!\"}}]}}]}\n\n" +
		"data: [DONE]\n\n"
	var events []openrouter.Event
	if err := c.stream(context.Background(), strings.NewReader(stream), func(e openrouter.Event) { events = append(events, e) }); err != nil {
		t.Fatal(err)
	}
	var calls []openrouter.ToolCall
	for _, e := range events {
		if e.Kind == openrouter.EventTool {
			calls = append(calls, *e.ToolCall)
		}
	}
	if len(calls) != 2 || calls[0].ID != "a" || calls[0].Function.Name != "first" || calls[0].Function.Arguments != "st!" || calls[1].ID != "b" || calls[1].Function.Name != "second" || calls[1].Function.Arguments != "ond!" {
		t.Fatalf("calls %#v", calls)
	}
}

func TestStreamMalformedChunkAndCancellationFinishIncomplete(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  context.Context
		body string
	}{
		{name: "malformed", ctx: context.Background(), body: "data: {not-json}\n\n"},
		{name: "cancelled", ctx: cancelledContext(t), body: "data: [DONE]\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var events []openrouter.Event
			_ = New("synthetic-secret").stream(tc.ctx, strings.NewReader(tc.body), func(e openrouter.Event) { events = append(events, e) })
			finishes := 0
			for _, e := range events {
				if e.Kind == openrouter.EventFinish {
					finishes++
					if e.Finished {
						t.Error("failed stream marked complete")
					}
				}
			}
			if finishes != 1 {
				t.Errorf("finish events %d", finishes)
			}
		})
	}
}

func cancelledContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestModelsMarksPricingUnknownAndUsageUnsupported(t *testing.T) {
	c, s := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("path %q", r.URL.Path)
		}
		io.WriteString(w, `{"object":"list","data":[{"id":"live-model","owned_by":"liquid"}]}`)
	})
	defer s.Close()
	models, err := c.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "live-model" || !models[0].PricingUnknown {
		t.Fatalf("models %#v", models)
	}
	if _, err := c.KeyUsage(context.Background()); err != ErrKeyUsageUnsupported {
		t.Errorf("KeyUsage error %v", err)
	}
}

func TestChatRedactsBoundedHTTPError(t *testing.T) {
	c, s := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "invalid synthetic-secret credential")
	})
	defer s.Close()
	var events []openrouter.Event
	if err := c.Chat(context.Background(), openrouter.Request{}, func(e openrouter.Event) { events = append(events, e) }); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Kind != openrouter.EventError || events[1].Kind != openrouter.EventFinish {
		t.Fatalf("events %#v", events)
	}
	if strings.Contains(events[0].Err.Error(), "synthetic-secret") {
		t.Errorf("credential leaked: %v", events[0].Err)
	}
}

func TestChatRejectsCrossOriginRedirectWithoutFollowing(t *testing.T) {
	var destinationCalls int
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destinationCalls++ }))
	defer destination.Close()
	c, s := testClient(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) })
	defer s.Close()
	var events []openrouter.Event
	if err := c.Chat(context.Background(), openrouter.Request{}, func(e openrouter.Event) { events = append(events, e) }); err != nil {
		t.Fatal(err)
	}
	if destinationCalls != 0 {
		t.Fatalf("cross-origin redirect reached destination %d times", destinationCalls)
	}
}

func TestChatIncompleteStreamIsNotFinished(t *testing.T) {
	c, s := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
	})
	defer s.Close()
	var events []openrouter.Event
	if err := c.Chat(context.Background(), openrouter.Request{}, func(e openrouter.Event) { events = append(events, e) }); err != nil {
		t.Fatal(err)
	}
	last := events[len(events)-1]
	if last.Kind != openrouter.EventFinish || last.Finished {
		t.Fatalf("terminal event %#v", last)
	}
	if events[0].Text != "partial" {
		t.Fatalf("partial event %#v", events[0])
	}
}
