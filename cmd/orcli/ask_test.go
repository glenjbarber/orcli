package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/glenjbarber/orcli/internal/openrouter"
	"github.com/glenjbarber/orcli/internal/tools"
	"github.com/glenjbarber/orcli/internal/tui"
)

// fakeChat answers ask's calls from a script, one round of events per call, and
// records every request it was handed - the seam chatClient exists for, since
// *openrouter.Client's base URL cannot be pointed at a test server from this package.
type fakeChat struct {
	rounds          [][]openrouter.Event
	seen            []openrouter.Request
	calls           int
	discardRequests bool
}

func (f *fakeChat) Chat(_ context.Context, req openrouter.Request, onEvent func(openrouter.Event)) error {
	if !f.discardRequests {
		f.seen = append(f.seen, req)
	}
	round := f.calls
	f.calls++
	if round >= len(f.rounds) {
		onEvent(openrouter.Event{Kind: openrouter.EventFinish, Reason: "stop", Finished: true})
		return nil
	}
	for _, e := range f.rounds[round] {
		onEvent(e)
	}
	return nil
}

// newTestSession returns a session rooted at dir, ready for ask to build a toolset
// and an introduction from.
func newTestSession(dir string) *tui.Session {
	return tui.New(tui.Options{
		Model:      "some/model",
		WorkingDir: dir,
		Approval:   tui.ApprovalAllow,
	})
}

// TestAskSendsTheQuestionAlone covers the plain path: no tools called, no
// CONTEXT.md, one request carrying just the user's question.
func TestAskSendsTheQuestionAlone(t *testing.T) {
	dir := t.TempDir()
	s := newTestSession(dir)
	fake := &fakeChat{rounds: [][]openrouter.Event{
		{
			{Kind: openrouter.EventDelta, Text: "an answer"},
			{Kind: openrouter.EventFinish, Reason: "stop", Finished: true},
		},
	}}

	a := ask(s, fake, "", nil)
	if err := a(context.Background(), "a question", 0); err != nil {
		t.Fatalf("ask: %v", err)
	}

	if len(fake.seen) != 1 {
		t.Fatalf("saw %d requests, want 1", len(fake.seen))
	}
	msgs := fake.seen[0].Messages
	if len(msgs) != 1 || msgs[0].Role != "user" || msgs[0].Content != "a question" {
		t.Errorf("messages = %+v, want one user message carrying the question", msgs)
	}
	if len(fake.seen[0].Tools) == 0 {
		t.Error("the request carried no tools, want the real toolset offered")
	}

	rows := s.Log().Rows()
	if got := rows[len(rows)-2].Text; got != "an answer" {
		t.Errorf("the delivered reply is %q, want %q", got, "an answer")
	}
}

// TestAskRunsAToolCallAndReplays covers the round trip: the model calls a tool that
// does not exist (deliberately, so the test needs no subprocess), and the second
// request replays the assistant's call and the tool's answer, paired by ID.
func TestAskRunsAToolCallAndReplays(t *testing.T) {
	dir := t.TempDir()
	s := newTestSession(dir)
	fake := &fakeChat{rounds: [][]openrouter.Event{
		{
			{Kind: openrouter.EventTool, ToolCall: &openrouter.ToolCall{
				ID:       "call-1",
				Function: openrouter.ToolCallFunction{Name: "no-such-tool", Arguments: "{}"},
			}},
			{Kind: openrouter.EventFinish, Reason: "tool_calls", Finished: true},
		},
		{
			{Kind: openrouter.EventDelta, Text: "done"},
			{Kind: openrouter.EventFinish, Reason: "stop", Finished: true},
		},
	}}

	a := ask(s, fake, "", nil)
	if err := a(context.Background(), "do something", 0); err != nil {
		t.Fatalf("ask: %v", err)
	}

	if len(fake.seen) != 2 {
		t.Fatalf("saw %d requests, want 2", len(fake.seen))
	}

	second := fake.seen[1].Messages
	if len(second) != 3 {
		t.Fatalf("the second request carries %d messages, want 3 (user, assistant, tool)", len(second))
	}
	if second[1].Role != "assistant" || len(second[1].ToolCalls) != 1 || second[1].ToolCalls[0].ID != "call-1" {
		t.Errorf("the replayed assistant message is %+v, want the call-1 tool call carried back", second[1])
	}
	if second[2].Role != "tool" || second[2].ToolCallID != "call-1" {
		t.Errorf("the tool-role message is %+v, want it paired to call-1", second[2])
	}
	if second[2].Content == "" {
		t.Error("the tool-role message carries no content, want a refusal naming the unknown tool")
	}

	rows := s.Log().Rows()
	if got := rows[len(rows)-2].Text; got != "done" {
		t.Errorf("the final delivered reply is %q, want %q", got, "done")
	}
}

// TestAskStopsAfterTooManyToolRounds covers the bound: a model that never stops
// calling tools does not hang the turn forever.
func TestAskStopsAfterTooManyToolRounds(t *testing.T) {
	dir := t.TempDir()
	s := newTestSession(dir)

	toolCallEvents := []openrouter.Event{
		{Kind: openrouter.EventTool, ToolCall: &openrouter.ToolCall{
			ID:       "call-x",
			Function: openrouter.ToolCallFunction{Name: "no-such-tool", Arguments: "{}"},
		}},
		{Kind: openrouter.EventFinish, Reason: "tool_calls", Finished: true},
	}
	rounds := make([][]openrouter.Event, maxToolRounds+2)
	for i := range rounds {
		rounds[i] = toolCallEvents
	}
	fake := &fakeChat{rounds: rounds, discardRequests: true}

	a := ask(s, fake, "", nil)
	if err := a(context.Background(), "loop forever", 0); err != nil {
		t.Fatalf("ask: %v", err)
	}

	if fake.calls != maxToolRounds {
		t.Errorf("saw %d requests, want exactly maxToolRounds (%d)", fake.calls, maxToolRounds)
	}
	state, _ := s.State()
	if state != tui.StateIdle {
		t.Errorf("state after stopping is %q, want idle", state)
	}
	rows := s.Log().Rows()
	if got := rows[len(rows)-1].Text; got != "stopped" {
		t.Errorf("the last log row is %q, want %q", got, "stopped")
	}
}

// TestIntroductionCarriesContextMD covers CONTEXT.md: when it exists in the working
// directory, its exact contents are sent as the system message ahead of the question.
func TestIntroductionCarriesContextMD(t *testing.T) {
	dir := t.TempDir()
	const contextText = "This project is orcli. Be terse."
	if err := os.WriteFile(filepath.Join(dir, "CONTEXT.md"), []byte(contextText), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s := newTestSession(dir)
	fake := &fakeChat{rounds: [][]openrouter.Event{
		{{Kind: openrouter.EventFinish, Reason: "stop", Finished: true}},
	}}

	a := ask(s, fake, "", nil)
	if err := a(context.Background(), "hi", 0); err != nil {
		t.Fatalf("ask: %v", err)
	}

	msgs := fake.seen[0].Messages
	if len(msgs) != 2 {
		t.Fatalf("messages = %+v, want a system message and the user question", msgs)
	}
	if msgs[0].Role != "system" || msgs[0].Content != contextText {
		t.Errorf("the system message is %+v, want role system carrying %q", msgs[0], contextText)
	}
	if msgs[1].Role != "user" || msgs[1].Content != "hi" {
		t.Errorf("the second message is %+v, want the user's question", msgs[1])
	}
}

// TestNoIntroductionWithoutContextMD covers the absence: a working directory with no
// CONTEXT.md sends no system message at all, not an empty one.
func TestNoIntroductionWithoutContextMD(t *testing.T) {
	dir := t.TempDir()
	s := newTestSession(dir)
	fake := &fakeChat{rounds: [][]openrouter.Event{
		{{Kind: openrouter.EventFinish, Reason: "stop", Finished: true}},
	}}

	a := ask(s, fake, "", nil)
	if err := a(context.Background(), "hi", 0); err != nil {
		t.Fatalf("ask: %v", err)
	}

	msgs := fake.seen[0].Messages
	if len(msgs) != 1 || msgs[0].Role != "user" {
		t.Errorf("messages = %+v, want only the user's question with no CONTEXT.md present", msgs)
	}
}

// TestRunToolRespectsDeny covers that a denied approval mode reaches a real tool: the
// shell tool is contained by newToolset but refuses to run anything once the mode is
// deny, and runTool reports that refusal as the tool's answer rather than running it
// anyway or panicking.
func TestRunToolRespectsDeny(t *testing.T) {
	dir := t.TempDir()
	toolset := []tools.Tool{tools.NewShell(dir)}

	got := runTool(toolset, tools.ApprovalDeny, openrouter.ToolCall{
		Function: openrouter.ToolCallFunction{
			Name:      "shell",
			Arguments: `{"command":"echo","args":["hi"]}`,
		},
	})

	if got == "" {
		t.Fatal("runTool returned no text for a denied call")
	}
	if got == "hi" {
		t.Error("the shell command ran despite the deny mode")
	}
}

// TestRunToolNamesAnUnknownTool covers a call for a tool that is not in the set at
// all - the model asking for something this session never offered.
func TestRunToolNamesAnUnknownTool(t *testing.T) {
	got := runTool(nil, tools.ApprovalAllow, openrouter.ToolCall{
		Function: openrouter.ToolCallFunction{Name: "does-not-exist", Arguments: "{}"},
	})
	if got == "" {
		t.Fatal("runTool returned no text for an unknown tool")
	}
}

// TestToolSchemasCoverTheWholeSet covers the conversion from internal/tools' Schema
// to openrouter.Tool: every tool in the set gets a schema, by the same name.
func TestToolSchemasCoverTheWholeSet(t *testing.T) {
	dir := t.TempDir()
	toolset := newToolset(dir)
	schemas := toolSchemas(toolset)

	if len(schemas) != len(toolset) {
		t.Fatalf("got %d schemas for %d tools", len(schemas), len(toolset))
	}
	for i, tool := range toolset {
		if schemas[i].Function.Name != tool.Name() {
			t.Errorf("schema %d is named %q, want %q", i, schemas[i].Function.Name, tool.Name())
		}
		if schemas[i].Type != "function" {
			t.Errorf("schema %d has type %q, want %q", i, schemas[i].Type, "function")
		}
	}
}
