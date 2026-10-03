package openrouter

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
)

// recorder collects the events a stream produced, in order.
//
// Order is part of what is being tested. A test that gathered only the final
// event would pass against a parser that reported the last thing and dropped
// everything before it, which is the failure this package exists to avoid.
type recorder struct {
	events []Event
}

func (r *recorder) onEvent(e Event) { r.events = append(r.events, e) }

// text returns every piece of reply text, in order.
func (r *recorder) text() string {
	var b strings.Builder
	for _, e := range r.events {
		if e.Kind == EventDelta {
			b.WriteString(e.Text)
		}
	}
	return b.String()
}

// kinds returns the kind of every event, in order.
func (r *recorder) kinds() []EventKind {
	out := make([]EventKind, 0, len(r.events))
	for _, e := range r.events {
		out = append(out, e.Kind)
	}
	return out
}

// tools returns the completed tool calls, in the order they were delivered.
func (r *recorder) tools() []ToolCall {
	var out []ToolCall
	for _, e := range r.events {
		if e.Kind == EventTool {
			out = append(out, *e.ToolCall)
		}
	}
	return out
}

// firstError returns the first failure reported, or nil.
func (r *recorder) firstError() error {
	for _, e := range r.events {
		if e.Kind == EventError {
			return e.Err
		}
	}
	return nil
}

// lastFinish returns the final event, which is always the one that ends the
// turn.
func (r *recorder) lastFinish() Event { return r.events[len(r.events)-1] }

// run drives the stream parser over a payload and returns what it reported.
func run(t *testing.T, payload string) *recorder {
	t.Helper()
	r := &recorder{}
	c := &Client{}
	if err := c.stream(context.Background(), strings.NewReader(payload), r.onEvent); err != nil {
		t.Fatalf("stream returned %v, want nil for a payload read from memory", err)
	}
	return r
}

// TestStreamRequiresTerminator is the property the whole package is built
// around: a stream that ended without its marker was cut, and presenting it as
// a complete reply is the outcome this client exists to prevent.
func TestStreamRequiresTerminator(t *testing.T) {
	r := run(t, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")

	finish := r.lastFinish()
	if finish.Kind != EventFinish {
		t.Errorf("last event is %v, want EventFinish", finish.Kind)
	}
	if finish.Finished {
		t.Error("Finished is true for a stream with no [DONE], want false")
	}
	if r.firstError() == nil {
		t.Error("no error was reported for a stream with no [DONE], want one")
	}
}

// TestStreamKeepsTextBeforeCut covers the other half of the same property. The
// text that arrived before the failure is usually more useful than the failure
// alone, so it is kept rather than taken back.
func TestStreamKeepsTextBeforeCut(t *testing.T) {
	r := run(t,
		"data: {\"choices\":[{\"delta\":{\"content\":\"the answer \"}}]}\n\n"+
			"data: {\"choices\":[{\"delta\":{\"content\":\"so far\"}}]}\n\n")

	if got, want := r.text(), "the answer so far"; got != want {
		t.Errorf("reply text is %q, want %q", got, want)
	}

	// The text must arrive before the failure, since a caller that renders on
	// the failure would otherwise show the failure first and the text after it.
	if len(r.events) < 2 {
		t.Fatalf("reported %d events, want at least 2", len(r.events))
	}
	if r.events[0].Kind != EventDelta {
		t.Errorf("first event is %v, want EventDelta", r.events[0].Kind)
	}
}

// TestStreamReportsDone covers the ordinary case, which is the one a reader
// should almost always see.
func TestStreamReportsDone(t *testing.T) {
	r := run(t,
		"data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"+
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"+
			"data: [DONE]\n\n")

	finish := r.lastFinish()
	if !finish.Finished {
		t.Error("Finished is false for a stream ending in [DONE], want true")
	}
	if finish.Reason != "stop" {
		t.Errorf("Reason is %q, want %q", finish.Reason, "stop")
	}
	if err := r.firstError(); err != nil {
		t.Errorf("an error was reported for a well formed stream: %v", err)
	}
	if got, want := r.kinds(), []EventKind{EventDelta, EventDone, EventFinish}; len(got) != len(want) ||
		got[0] != want[0] || got[1] != want[1] {
		t.Errorf("event kinds are %v, want %v", got, want)
	}
}

// TestStreamReassemblesByWireIndex is the reason the assembler keeps an index
// at all. A call at index 1 arriving before a call at index 0 must be
// delivered second, because arrival order is not the order the endpoint numbered
// them in and the index is the only field every fragment carries.
func TestStreamReassemblesByWireIndex(t *testing.T) {
	r := run(t,
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":1,\"id\":\"call_b\","+
			"\"function\":{\"name\":\"second\",\"arguments\":\"{\\\"n\\\":2}\"}}]}}]}\n\n"+
			"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_a\","+
			"\"function\":{\"name\":\"first\",\"arguments\":\"{\\\"n\\\":1}\"}}]}}]}\n\n"+
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"+
			"data: [DONE]\n\n")

	calls := r.tools()
	if len(calls) != 2 {
		t.Fatalf("got %d tool calls, want 2", len(calls))
	}
	if calls[0].Function.Name != "first" {
		t.Errorf("first delivered call is %q, want %q", calls[0].Function.Name, "first")
	}
	if calls[1].Function.Name != "second" {
		t.Errorf("second delivered call is %q, want %q", calls[1].Function.Name, "second")
	}
	if calls[0].ID != "call_a" || calls[1].ID != "call_b" {
		t.Errorf("call ids are %q and %q, want call_a and call_b", calls[0].ID, calls[1].ID)
	}
}

// TestStreamJoinsArgumentPieces covers a call split across fragments, which is
// how the endpoint sends arguments that are longer than one chunk.
func TestStreamJoinsArgumentPieces(t *testing.T) {
	r := run(t,
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_a\","+
			"\"function\":{\"name\":\"write_file\",\"arguments\":\"{\\\"path\\\":\"}}]}}]}\n\n"+
			"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,"+
			"\"function\":{\"arguments\":\"\\\"x\\\"}\"}}]}}]}\n\n"+
			"data: [DONE]\n\n")

	calls := r.tools()
	if len(calls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(calls))
	}
	if got, want := calls[0].Function.Arguments, `{"path":"x"}`; got != want {
		t.Errorf("arguments are %q, want %q", got, want)
	}
}

// TestStreamDropsCallWithoutName covers a fragment that never named its
// function. There would be nothing to run, and a call with no name handed to
// the interface is a request the interface cannot answer.
func TestStreamDropsCallWithoutName(t *testing.T) {
	r := run(t,
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_a\","+
			"\"function\":{\"arguments\":\"{\\\"n\\\":1}\"}}]}}]}\n\n"+
			"data: [DONE]\n\n")

	if calls := r.tools(); len(calls) != 0 {
		t.Errorf("got %d tool calls, want 0 for a call with no name", len(calls))
	}
	if err := r.firstError(); err != nil {
		t.Errorf("an error was reported: %v", err)
	}
}

// TestStreamKeepsNamedCallBesideUnnamed covers the same rule when a stream
// carries one usable call and one not. Dropping the unnamed one must not take
// the named one with it.
func TestStreamKeepsNamedCallBesideUnnamed(t *testing.T) {
	r := run(t,
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,"+
			"\"function\":{\"arguments\":\"{}\"}}]}}]}\n\n"+
			"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":1,\"id\":\"call_b\","+
			"\"function\":{\"name\":\"read_file\",\"arguments\":\"{}\"}}]}}]}\n\n"+
			"data: [DONE]\n\n")

	calls := r.tools()
	if len(calls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(calls))
	}
	if calls[0].Function.Name != "read_file" {
		t.Errorf("delivered call is %q, want %q", calls[0].Function.Name, "read_file")
	}
}

// TestDataOf covers the line reader. Only the data field is read, so a rename
// of the event name upstream cannot change what this parser does.
func TestDataOf(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
		want string
		ok   bool
	}{
		{"payload", "data: {\"a\":1}", `{"a":1}`, true},
		{"payload without space", "data:{\"a\":1}", `{"a":1}`, true},
		{"payload with carriage return", "data: x\r\n", "x", true},
		{"terminator", "data: [DONE]", "[DONE]", true},
		{"comment", ": keep-alive", "", false},
		{"blank", "", "", false},
		{"whitespace only", "   ", "", false},
		{"event name is not read", "event: message", "", false},
		{"id is not read", "id: 7", "", false},
		{"retry is not read", "retry: 100", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := dataOf(tc.line)
			if ok != tc.ok {
				t.Errorf("dataOf(%q) reported %v, want %v", tc.line, ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Errorf("dataOf(%q) is %q, want %q", tc.line, got, tc.want)
			}
		})
	}
}

// TestStreamIgnoresUnknownFields covers a payload carrying fields this client
// does not name. The endpoint is free to add to the envelope, and a parser
// that refused an unknown field would refuse the reply along with it.
func TestStreamIgnoresUnknownFields(t *testing.T) {
	r := run(t,
		"data: {\"id\":\"gen-1\",\"provider\":{\"name\":\"x\"},\"model\":\"m\","+
			"\"choices\":[{\"index\":0,\"logprobs\":null,"+
			"\"delta\":{\"role\":\"assistant\",\"content\":\"kept\"}}]}\n\n"+
			"data: [DONE]\n\n")

	if got, want := r.text(), "kept"; got != want {
		t.Errorf("reply text is %q, want %q", got, want)
	}
	if err := r.firstError(); err != nil {
		t.Errorf("an error was reported: %v", err)
	}
}

// TestStreamKeepsReadingAfterUnreadablePayload covers a payload that is not
// JSON. It is reported and reading continues, because the alternative is to
// end a stream over one line in a shape this parser does not know, which loses
// the reply that was already arriving.
func TestStreamKeepsReadingAfterUnreadablePayload(t *testing.T) {
	r := run(t,
		"data: {not json at all}\n\n"+
			"data: {\"choices\":[{\"delta\":{\"content\":\"after\"}}]}\n\n"+
			"data: [DONE]\n\n")

	if err := r.firstError(); err == nil {
		t.Error("no error was reported for an unreadable payload, want one")
	}
	if got, want := r.text(), "after"; got != want {
		t.Errorf("reply text is %q, want %q", got, want)
	}
	if !r.lastFinish().Finished {
		t.Error("Finished is false, want true: the stream did reach its marker")
	}
}

// TestStreamReportsUnreadableUsage covers usage written in a shape the strict
// decoder refuses. The reply around it is worth more than the shape of the
// accounting, so the payload is read and the stream continues.
func TestStreamReportsUnreadableUsage(t *testing.T) {
	r := run(t,
		"data: {\"choices\":[{\"delta\":{\"content\":\"text\"}}],"+
			"\"usage\":{\"prompt_tokens\":\"not a number at all\"}}\n\n"+
			"data: [DONE]\n\n")

	if got, want := r.text(), "text"; got != want {
		t.Errorf("reply text is %q, want %q", got, want)
	}
	if !r.lastFinish().Finished {
		t.Error("Finished is false, want true: the stream reached its marker")
	}
}

// TestStreamUsageZeroIsNotAbsent covers the reason every figure in Usage is a
// pointer. A response that spent nothing is not the same as one whose
// accounting never arrived, and a plain integer cannot tell those apart.
func TestStreamUsageZeroIsNotAbsent(t *testing.T) {
	r := run(t,
		"data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}],"+
			"\"usage\":{\"prompt_tokens\":0,\"completion_tokens\":0,\"total_tokens\":0}}\n\n"+
			"data: [DONE]\n\n")

	var usage *Usage
	for _, e := range r.events {
		if e.Kind == EventUsage {
			usage = e.Usage
		}
	}
	if usage == nil {
		t.Fatal("no usage event was reported for a payload carrying usage")
	}
	if usage.PromptTokens == nil {
		t.Fatal("PromptTokens is nil for a reported zero, want a pointer to 0")
	}
	if *usage.PromptTokens != 0 {
		t.Errorf("PromptTokens is %d, want 0", *usage.PromptTokens)
	}
	if usage.Cost != nil {
		t.Errorf("Cost is %v for a payload carrying no cost, want nil", *usage.Cost)
	}
}

// TestStreamWithoutUsageReportsNone covers the other half: a payload with no
// accounting produces no usage event at all, rather than a usage event holding
// zeros.
func TestStreamWithoutUsageReportsNone(t *testing.T) {
	r := run(t,
		"data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n"+
			"data: [DONE]\n\n")

	for _, e := range r.events {
		if e.Kind == EventUsage {
			t.Errorf("a usage event was reported for a payload carrying none: %+v", e.Usage)
		}
	}
}

// TestStreamReadsQuotedCounts covers the counts this endpoint is known to send
// as strings. Refusing the payload over them would cost the reply that carried
// them.
func TestStreamReadsQuotedCounts(t *testing.T) {
	for _, form := range []string{"\"1234\"", "1234"} {
		t.Run(form, func(t *testing.T) {
			r := run(t,
				"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":"+form+","+
					"\"total_tokens\":\"2048\"}}\n\n"+
					"data: [DONE]\n\n")

			var usage *Usage
			for _, e := range r.events {
				if e.Kind == EventUsage {
					usage = e.Usage
				}
			}
			if usage == nil || usage.PromptTokens == nil {
				t.Fatalf("no prompt count was read from %s", form)
			}
			if *usage.PromptTokens != 1234 {
				t.Errorf("prompt count is %d, want 1234", *usage.PromptTokens)
			}
			if usage.TotalTokens == nil || *usage.TotalTokens != 2048 {
				t.Errorf("total count is not 2048 when written as a quoted string")
			}
		})
	}
}

// TestStreamClampsHugeCount covers a figure above the platform maximum. A wrong
// count is a wrong count, and a refused stream is a lost reply.
func TestStreamClampsHugeCount(t *testing.T) {
	r := run(t,
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1e30}}\n\n"+
			"data: [DONE]\n\n")

	var usage *Usage
	for _, e := range r.events {
		if e.Kind == EventUsage {
			usage = e.Usage
		}
	}
	if usage == nil || usage.PromptTokens == nil {
		t.Fatal("no prompt count was read from a payload carrying one")
	}
	if *usage.PromptTokens != maxTokenCount {
		t.Errorf("prompt count is %d, want it clamped to %d", *usage.PromptTokens, maxTokenCount)
	}
	if err := r.firstError(); err != nil {
		t.Errorf("an error was reported: %v", err)
	}
}

// TestStreamReadsCost covers cost in either of the shapes the endpoint uses,
// and covers its absence, since a session ledger adds nothing for a response
// that carried no figure.
func TestStreamReadsCost(t *testing.T) {
	for _, form := range []string{"0.00042", "\"0.00042\""} {
		t.Run(form, func(t *testing.T) {
			r := run(t,
				"data: {\"choices\":[],\"usage\":{\"cost\":"+form+"}}\n\n"+
					"data: [DONE]\n\n")

			var usage *Usage
			for _, e := range r.events {
				if e.Kind == EventUsage {
					usage = e.Usage
				}
			}
			if usage == nil || usage.Cost == nil {
				t.Fatalf("no cost was read from %s", form)
			}
			if math.Abs(*usage.Cost-0.00042) > 1e-12 {
				t.Errorf("cost is %v, want 0.00042", *usage.Cost)
			}
			if usage.CostUSD != nil {
				t.Errorf("CostUSD is %v for a payload carrying no cost_usd, want nil", *usage.CostUSD)
			}
		})
	}
}

// TestCountClampsEdgeValues covers the bounds directly, including the ones JSON
// cannot express.
func TestCountClampsEdgeValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   flexInt
		want int
	}{
		{"ordinary", 4096, 4096},
		{"zero", 0, 0},
		{"negative", -1, 0},
		{"above the maximum", maxTokenCount + 1, maxTokenCount},
		{"positive infinity", flexInt(math.Inf(1)), maxTokenCount},
		{"negative infinity", flexInt(math.Inf(-1)), 0},
		{"not a number", flexInt(math.NaN()), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.count(); got != tc.want {
				t.Errorf("count() is %d, want %d", got, tc.want)
			}
		})
	}
}

// TestStreamFinishesOnce covers that the turn ends exactly once, however many
// markers or finish reasons the stream carried.
func TestStreamFinishesOnce(t *testing.T) {
	r := run(t,
		"data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n"+
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"length\"}]}\n\n"+
			"data: [DONE]\n\n")

	var finishes int
	for _, e := range r.events {
		if e.Kind == EventFinish {
			finishes++
		}
	}
	if finishes != 1 {
		t.Errorf("reported %d finish events, want 1", finishes)
	}
}

// TestChatRefusesWithoutCredential covers the check made before the request.
// The backend answers a request with no credential exactly as it answers an
// invalid one, so the two cannot be told apart from the response alone.
func TestChatRefusesWithoutCredential(t *testing.T) {
	r := &recorder{}
	err := New("").Chat(context.Background(), Request{}, r.onEvent)
	if !errors.Is(err, ErrNoAPIKey) {
		t.Errorf("Chat returned %v, want ErrNoAPIKey", err)
	}
	if len(r.events) != 0 {
		t.Errorf("reported %d events without a credential, want 0", len(r.events))
	}
}

// TestChatRequiresCallback covers the guard on the callback. A nil callback
// with a stream arriving would panic rather than report, and a panic in the
// transport is not a failure the reader can be shown.
func TestChatRequiresCallback(t *testing.T) {
	err := New("key").Chat(context.Background(), Request{}, nil)
	if err == nil {
		t.Error("Chat with a nil callback returned nil, want an error")
	}
}

// TestFilter covers redaction. The credential travels in a header, so it is in
// no body this client builds, but a server or a proxy can quote it back, and
// every string here is shown in a pane and ends up in terminal scrollback.
func TestFilter(t *testing.T) {
	const key = "sk-secret-value"

	if got, want := Filter("quoted back: "+key, key), "quoted back: [redacted]"; got != want {
		t.Errorf("Filter is %q, want %q", got, want)
	}
	if got, want := Filter("nothing to do", key), "nothing to do"; got != want {
		t.Errorf("Filter is %q, want %q", got, want)
	}
	if got, want := Filter("an empty secret", ""), "an empty secret"; got != want {
		t.Errorf("Filter is %q, want %q", got, want)
	}
	if got, want := Filter("", key), ""; got != want {
		t.Errorf("Filter is %q, want %q", got, want)
	}
}
