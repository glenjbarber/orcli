package openrouter

import (
	"strings"
	"testing"
)

// collect runs a payload through the stream parser and returns what it
// reported, in order.
//
// Driving the parser directly rather than through Chat keeps these tests free
// of a network and of a credential. The parser is where the decisions are, and
// Chat is where the decisions are merely invoked.
func collect(t *testing.T, payload string) []Event {
	t.Helper()
	var got []Event
	c := &Client{}
	if err := c.stream(t.Context(), strings.NewReader(payload), func(e Event) {
		got = append(got, e)
	}); err != nil {
		t.Fatalf("stream returned %v, want nil: a failure is reported through the callback", err)
	}
	return got
}

// deltas returns the reply text the events carried, joined.
func deltas(events []Event) string {
	var b strings.Builder
	for _, e := range events {
		if e.Kind == EventDelta {
			b.WriteString(e.Text)
		}
	}
	return b.String()
}

// find returns the first event of a kind, and whether there was one.
func find(events []Event, kind EventKind) (Event, bool) {
	for _, e := range events {
		if e.Kind == kind {
			return e, true
		}
	}
	return Event{}, false
}

// last returns the final event of a kind.
func last(events []Event, kind EventKind) (Event, bool) {
	var found Event
	ok := false
	for _, e := range events {
		if e.Kind == kind {
			found, ok = e, true
		}
	}
	return found, ok
}

// count reports how many events of a kind were seen.
func count(events []Event, kind EventKind) int {
	n := 0
	for _, e := range events {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

// sse builds a stream from data payloads, in the framing the endpoint uses.
func sse(payloads ...string) string {
	var b strings.Builder
	for _, p := range payloads {
		b.WriteString("data: " + p + "\n\n")
	}
	return b.String()
}

// chunkOf builds a text delta payload.
func chunkOf(text string) string {
	return `{"choices":[{"delta":{"content":` + quote(text) + `}}]}`
}

// quote renders a Go string as a JSON string.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// TestStreamFinishedRequiresTheMarker is the property this package exists for.
//
// A stream that ends without its terminating marker was cut short. Presenting
// it as a complete reply is the one outcome the client is built to avoid, so
// the text that arrived is kept and the finish is reported as unfinished.
func TestStreamFinishedRequiresTheMarker(t *testing.T) {
	events := collect(t, sse(chunkOf("the first half")))

	if got, want := deltas(events), "the first half"; got != want {
		t.Errorf("reply text = %q, want %q: text that arrived before a cut is kept", got, want)
	}

	finish, ok := last(events, EventFinish)
	if !ok {
		t.Fatal("no finish event, want one")
	}
	if finish.Finished {
		t.Error("Finished = true, want false: a stream without its marker was cut short")
	}
	if _, ok := find(events, EventError); !ok {
		t.Error("no error event, want one: a cut stream is reported, not assumed finished")
	}
}

// TestStreamCompleteIsFinished checks the other side of the same rule, so that
// the first test cannot pass by a parser that never finishes anything.
func TestStreamCompleteIsFinished(t *testing.T) {
	events := collect(t, sse(chunkOf("hello "), chunkOf("there"), "[DONE]"))

	finish, ok := last(events, EventFinish)
	if !ok {
		t.Fatal("no finish event, want one")
	}
	if !finish.Finished {
		t.Error("Finished = false, want true: the marker was sent")
	}
	if _, ok := find(events, EventError); ok {
		t.Error("an error was reported for a stream that ended on its marker")
	}
	if got, want := count(events, EventDone), 1; got != want {
		t.Errorf("done events = %d, want %d", got, want)
	}
}

// TestStreamKeepsTextBeforeAFailure checks that a payload which cannot be read
// does not take the reply with it.
func TestStreamKeepsTextBeforeAFailure(t *testing.T) {
	events := collect(t, sse(chunkOf("kept"), "{not json", chunkOf(" too"), "[DONE]"))

	if got, want := deltas(events), "kept too"; got != want {
		t.Errorf("reply text = %q, want %q", got, want)
	}
	if _, ok := find(events, EventError); !ok {
		t.Error("no error event for the unreadable payload, want one")
	}
	if finish, ok := last(events, EventFinish); !ok || !finish.Finished {
		t.Error("the stream was not reported finished, want finished: one bad line is not a cut stream")
	}
}

// TestStreamJoinsFragmentsOnTheWireIndex is the reason fragments are joined
// where they are.
//
// The endpoint numbers each call, and the index is the only field every
// fragment carries. A call numbered 1 can therefore arrive before a call
// numbered 0, and arrival order must not decide the order they are delivered.
func TestStreamJoinsFragmentsOnTheWireIndex(t *testing.T) {
	payloads := []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_b",` +
			`"type":"function","function":{"name":"list_dir","arguments":""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a",` +
			`"type":"function","function":{"name":"read_file","arguments":` +
			`"{\"path\":\"a\"}"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":1,"function":` +
			`{"arguments":"{\"path\":\"b\"}"}}]}}]}`,
		`{"choices":[{"finish_reason":"tool_calls"}]}`,
		"[DONE]",
	}

	events := collect(t, sse(payloads...))

	var names []string
	for _, e := range events {
		if e.Kind == EventTool {
			names = append(names, e.ToolCall.Function.Name)
		}
	}
	if len(names) != 2 {
		t.Fatalf("tool calls = %v, want two", names)
	}
	if names[0] != "read_file" || names[1] != "list_dir" {
		t.Errorf("order = %v, want [read_file list_dir]: joined on the wire index", names)
	}
}

// TestStreamDropsACallWithNoName checks that a call the endpoint never finished
// announcing is not handed upward.
//
// There would be nothing to run, and a call with no name handed to the
// interface is a request the interface cannot answer.
func TestStreamDropsACallWithNoName(t *testing.T) {
	payload := `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a",` +
		`"type":"function","function":{"arguments":"{}"}}]},` +
		`"finish_reason":"tool_calls"}]}`

	events := collect(t, sse(payload, "[DONE]"))

	if got := count(events, EventTool); got != 0 {
		t.Errorf("tool calls = %d, want 0: a call with no name is dropped", got)
	}
}

// TestStreamDeliversCallsAtTheFinishReason checks that a call is delivered when
// the endpoint terminates it, rather than being held until a stream end that
// may never come.
func TestStreamDeliversCallsAtTheFinishReason(t *testing.T) {
	first := `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a",` +
		`"type":"function","function":{"name":"list_dir","arguments":"{}"}}]},` +
		`"finish_reason":"tool_calls"}]}`
	second := `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_b",` +
		`"type":"function","function":{"name":"read_file","arguments":"{}"}}]},` +
		`"finish_reason":"tool_calls"}]}`

	events := collect(t, sse(first, second, "[DONE]"))

	// The two payloads both use index 0. The first is delivered on its finish
	// reason, which frees the index for the second, so both arrive rather than
	// the second merging into the first.
	if got, want := count(events, EventTool), 2; got != want {
		t.Errorf("tool calls = %d, want %d: a delivered call frees its index", got, want)
	}
}

// TestStreamUsageIsDistinguishableFromAbsence checks the reason every figure
// is a pointer.
//
// A response that spent nothing and a response whose accounting never arrived
// are different, and a plain integer cannot tell them apart.
func TestStreamUsageIsDistinguishableFromAbsence(t *testing.T) {
	zero := `{"choices":[],"usage":{"prompt_tokens":0,"completion_tokens":0,` +
		`"total_tokens":0,"cost":0}}`
	events := collect(t, sse(zero, "[DONE]"))

	usage, ok := find(events, EventUsage)
	if !ok {
		t.Fatal("no usage event, want one")
	}
	if usage.Usage.PromptTokens == nil {
		t.Error("PromptTokens = nil, want a pointer to zero: a reported zero is not an absent value")
	}
	if usage.Usage.Cost == nil {
		t.Error("Cost = nil, want a pointer to zero")
	}

	// A payload with no usage at all reports no usage event, rather than an
	// event carrying a zero the endpoint never sent.
	if got := count(collect(t, sse(chunkOf("x"), "[DONE]")), EventUsage); got != 0 {
		t.Errorf("usage events = %d, want 0 when the endpoint sent no usage", got)
	}
}

// TestStreamReadsCountsWrittenAsText checks that a count the endpoint quotes is
// read rather than refused.
//
// Refusing the payload over it would cost the reply that carried the count,
// which is worth more than the count being exact.
func TestStreamReadsCountsWrittenAsText(t *testing.T) {
	payload := `{"choices":[],"usage":{"prompt_tokens":"1234",` +
		`"completion_tokens":1.2e3,"total_tokens":"2234","cost":"0.0042"}}`
	events := collect(t, sse(payload, "[DONE]"))

	usage, ok := find(events, EventUsage)
	if !ok {
		t.Fatal("no usage event, want one")
	}
	if got, want := *usage.Usage.PromptTokens, 1234; got != want {
		t.Errorf("PromptTokens = %d, want %d", got, want)
	}
	if got, want := *usage.Usage.CompletionTokens, 1200; got != want {
		t.Errorf("CompletionTokens = %d, want %d: exponent form is read", got, want)
	}
	if got, want := *usage.Usage.TotalTokens, 2234; got != want {
		t.Errorf("TotalTokens = %d, want %d", got, want)
	}
	if got, want := *usage.Usage.Cost, 0.0042; got != want {
		t.Errorf("Cost = %v, want %v", got, want)
	}
}

// TestStreamClampsACountBeyondThePlatform checks that an absurd figure is
// bounded rather than allowed to cut a reply short.
func TestStreamClampsACountBeyondThePlatform(t *testing.T) {
	payload := `{"choices":[],"usage":{"total_tokens":` +
		`123456789012345678901234567890}}`
	events := collect(t, sse(payload, "[DONE]"))

	usage, ok := find(events, EventUsage)
	if !ok {
		t.Fatal("no usage event, want one")
	}
	if got, want := *usage.Usage.TotalTokens, maxTokenCount; got != want {
		t.Errorf("TotalTokens = %d, want %d: a figure past the maximum is clamped", got, want)
	}
}

// TestDataOfReadsOnlyTheDataField checks that the kind of an event is carried
// by what it contains rather than by the name beside it.
//
// A rename upstream then cannot change what this client does with a payload.
func TestDataOfReadsOnlyTheDataField(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
		ok   bool
	}{
		{"payload", "data: {}", "{}", true},
		{"no space after colon", "data:{}", "{}", true},
		{"trailing newline", "data: {}\n", "{}", true},
		{"carriage return", "data: {}\r\n", "{}", true},
		{"comment", ": keep-alive", "", false},
		{"separator", "", "", false},
		{"event name", "event: message", "", false},
		{"id field", "id: 7", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := dataOf(tc.line)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if got != tc.want {
				t.Errorf("payload = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestStreamIgnoresUnindexedContinuation checks the fallback for a fragment
// carrying no index.
//
// The index is the only field every fragment is guaranteed to carry, and an
// endpoint that omits it on a continuation is describing a continuation of the
// call most recently started.
func TestStreamIgnoresUnindexedContinuation(t *testing.T) {
	first := `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a",` +
		`"type":"function","function":{"name":"read_file","arguments":"{\"pa"}}]}}]}`
	second := `{"choices":[{"delta":{"tool_calls":[{"function":` +
		`{"arguments":"th\":\"a\"}"}}]}}]}`
	third := `{"choices":[{"finish_reason":"tool_calls"}]}`

	events := collect(t, sse(first, second, third, "[DONE]"))

	if got, want := count(events, EventTool), 1; got != want {
		t.Fatalf("tool calls = %d, want %d", got, want)
	}
	call, _ := find(events, EventTool)
	if got, want := call.ToolCall.Function.Name, "read_file"; got != want {
		t.Errorf("name = %q, want %q", got, want)
	}
	if got, want := call.ToolCall.Function.Arguments, `{"path":"a"}`; got != want {
		t.Errorf("arguments = %q, want %q: pieces are concatenated in arrival order", got, want)
	}
}

// TestStreamDeliversAnUnnumberedCallLast checks that a fragment with neither an
// index nor a call to continue is delivered after the numbered ones, rather
// than displacing a call the endpoint did number.
func TestStreamDeliversAnUnnumberedCallLast(t *testing.T) {
	numbered := `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a",` +
		`"type":"function","function":{"name":"read_file","arguments":"{}"}}]},` +
		`"finish_reason":"tool_calls"}]}`
	unnumbered := `{"choices":[{"delta":{"tool_calls":[{"id":"call_z",` +
		`"type":"function","function":{"name":"list_dir","arguments":"{}"}}]},` +
		`"finish_reason":"tool_calls"}]}`

	events := collect(t, sse(numbered, unnumbered, "[DONE]"))

	var names []string
	for _, e := range events {
		if e.Kind == EventTool {
			names = append(names, e.ToolCall.Function.Name)
		}
	}
	if len(names) != 2 {
		t.Fatalf("tool calls = %v, want two", names)
	}
	if names[0] != "read_file" || names[1] != "list_dir" {
		t.Errorf("order = %v, want [read_file list_dir]", names)
	}
}

// TestFilterRedactsTheCredential checks that a diagnostic cannot quote the key
// back at the reader.
//
// The key travels in a header, so it is in no body this client builds, but a
// server or a proxy can quote it back, and every diagnostic ends up in terminal
// scrollback.
func TestFilterRedactsTheCredential(t *testing.T) {
	const key = "sk-or-v1-secret"
	got := Filter("failed with "+key+" in the body", key)

	if strings.Contains(got, key) {
		t.Errorf("filtered text still carries the credential: %q", got)
	}
	if !strings.Contains(got, "[redacted]") {
		t.Errorf("filtered text = %q, want it marked as redacted", got)
	}
}

// TestFilterLeavesTextAlone checks that filtering is not destructive when there
// is nothing to redact.
func TestFilterLeavesTextAlone(t *testing.T) {
	cases := []struct{ text, secret string }{
		{"nothing to redact", keyFixture},
		{"", keyFixture},
		{"anything", ""},
		{"", ""},
	}

	for _, tc := range cases {
		if got := Filter(tc.text, tc.secret); got != tc.text {
			t.Errorf("Filter(%q, %q) = %q, want it unchanged", tc.text, tc.secret, got)
		}
	}
}

// keyFixture is a credential-shaped string used by the filter tests.
const keyFixture = "sk-or-v1-abc123"
