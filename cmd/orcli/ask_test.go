package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glenjbarber/orcli/internal/openrouter"
	"github.com/glenjbarber/orcli/internal/tools"
	"github.com/glenjbarber/orcli/internal/tui"
)

// fakeChat answers ask's calls from a script, one round of events per call, and
// records every request it was handed - the seam chatClient exists for, since
// *openrouter.Client's base URL cannot be pointed at a test server from this package.
type fakeChat struct {
	rounds [][]openrouter.Event
	seen   []openrouter.Request
}

func (f *fakeChat) Chat(_ context.Context, req openrouter.Request, onEvent func(openrouter.Event)) error {
	f.seen = append(f.seen, req)
	round := len(f.seen) - 1
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
// AGENTS.md, one request carrying just the user's question.
func TestAskSendsTheQuestionAlone(t *testing.T) {
	dir := t.TempDir()
	s := newTestSession(dir)
	fake := &fakeChat{rounds: [][]openrouter.Event{
		{
			{Kind: openrouter.EventDelta, Text: "an answer"},
			{Kind: openrouter.EventFinish, Reason: "stop", Finished: true},
		},
	}}

	a := ask(s, fake, "", nil, nil)
	if err := a(context.Background(), "a question", 0); err != nil {
		t.Fatalf("ask: %v", err)
	}

	if len(fake.seen) != 1 {
		t.Fatalf("saw %d requests, want 1", len(fake.seen))
	}
	msgs := fake.seen[0].Messages
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want the capability system message and the question", len(msgs))
	}
	if msgs[0].Role != "system" || !strings.Contains(msgs[0].Content, "orcli") {
		t.Errorf("the first message is %+v, want the capability message naming orcli", msgs[0])
	}
	if msgs[1].Role != "user" || msgs[1].Content != "a question" {
		t.Errorf("the second message is %+v, want the user's question", msgs[1])
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

	a := ask(s, fake, "", nil, nil)
	if err := a(context.Background(), "do something", 0); err != nil {
		t.Fatalf("ask: %v", err)
	}

	if len(fake.seen) != 2 {
		t.Fatalf("saw %d requests, want 2", len(fake.seen))
	}

	second := fake.seen[1].Messages
	if len(second) != 4 {
		t.Fatalf("the second request carries %d messages, want 4 (capability, user, assistant, tool)", len(second))
	}
	if second[2].Role != "assistant" || len(second[2].ToolCalls) != 1 || second[2].ToolCalls[0].ID != "call-1" {
		t.Errorf("the replayed assistant message is %+v, want the call-1 tool call carried back", second[2])
	}
	if second[3].Role != "tool" || second[3].ToolCallID != "call-1" {
		t.Errorf("the tool-role message is %+v, want it paired to call-1", second[3])
	}
	if second[3].Content == "" {
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
	fake := &fakeChat{rounds: rounds}

	a := ask(s, fake, "", nil, nil)
	if err := a(context.Background(), "loop forever", 0); err != nil {
		t.Fatalf("ask: %v", err)
	}

	if len(fake.seen) != maxToolRounds {
		t.Errorf("saw %d requests, want exactly maxToolRounds (%d)", len(fake.seen), maxToolRounds)
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

// TestIntroductionCarriesAgentsMD covers AGENTS.md: when it exists in the working
// directory, its exact contents are sent as the system message ahead of the question.
func TestIntroductionCarriesAgentsMD(t *testing.T) {
	dir := t.TempDir()
	const agentsText = "This project is orcli. Be terse."
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(agentsText), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s := newTestSession(dir)
	fake := &fakeChat{rounds: [][]openrouter.Event{
		{{Kind: openrouter.EventFinish, Reason: "stop", Finished: true}},
	}}

	a := ask(s, fake, "", nil, nil)
	if err := a(context.Background(), "hi", 0); err != nil {
		t.Fatalf("ask: %v", err)
	}

	msgs := fake.seen[0].Messages
	if len(msgs) != 3 {
		t.Fatalf("messages = %+v, want the capability message, the AGENTS.md message, and the question", msgs)
	}
	if msgs[1].Role != "system" || msgs[1].Content != agentsText {
		t.Errorf("the second message is %+v, want role system carrying %q", msgs[1], agentsText)
	}
	if msgs[2].Role != "user" || msgs[2].Content != "hi" {
		t.Errorf("the third message is %+v, want the user's question", msgs[2])
	}
}

// TestNoIntroductionWithoutAgentsMD covers the absence: a working directory with no
// AGENTS.md sends no system message at all, not an empty one.
func TestNoIntroductionWithoutAgentsMD(t *testing.T) {
	dir := t.TempDir()
	s := newTestSession(dir)
	fake := &fakeChat{rounds: [][]openrouter.Event{
		{{Kind: openrouter.EventFinish, Reason: "stop", Finished: true}},
	}}

	a := ask(s, fake, "", nil, nil)
	if err := a(context.Background(), "hi", 0); err != nil {
		t.Fatalf("ask: %v", err)
	}

	msgs := fake.seen[0].Messages
	if len(msgs) != 2 || msgs[0].Role != "system" || msgs[1].Role != "user" {
		t.Errorf("messages = %+v, want the capability message and the user's question, no AGENTS.md message", msgs)
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

// TestCapabilitiesNamesTheRealModel covers adr-0000042's model line: a session with
// a model configured names it and its provider, not a generic claim.
func TestCapabilitiesNamesTheRealModel(t *testing.T) {
	s := tui.New(tui.Options{Model: "some/model", Provider: "openrouter.ai"})
	got := capabilities(s, nil, nil)
	if !strings.Contains(got, "some/model") || !strings.Contains(got, "openrouter.ai") {
		t.Errorf("capabilities = %q, want the configured model and provider named", got)
	}
}

// TestCapabilitiesNamesAnAbsentModel covers the other side: a session with no model
// says so, rather than silently describing tools as if a model were there to use
// them.
func TestCapabilitiesNamesAnAbsentModel(t *testing.T) {
	s := tui.New(tui.Options{})
	got := capabilities(s, nil, nil)
	if !strings.Contains(got, "none configured") {
		t.Errorf("capabilities = %q, want it to say no model is configured", got)
	}
}

// TestCapabilitiesListsEveryToolByName covers the no-second-list rule: every tool
// passed in appears by its own Name(), and the text is the tool's own Describe()
// description, not a hand-written paraphrase.
func TestCapabilitiesListsEveryToolByName(t *testing.T) {
	dir := t.TempDir()
	toolset := newToolset(dir)
	s := tui.New(tui.Options{Model: "some/model"})
	got := capabilities(s, toolset, nil)

	for _, tool := range toolset {
		if !strings.Contains(got, tool.Name()) {
			t.Errorf("capabilities does not mention tool %q", tool.Name())
		}
		if !strings.Contains(got, tool.Describe().Function.Description) {
			t.Errorf("capabilities does not carry tool %q's own description", tool.Name())
		}
	}
}

// TestCapabilitiesNamesAbsentToolsHonestly covers a session with no tools wired in
// at all: absence is written out, per adr-0000042, not left as a gap the model has
// to guess at.
func TestCapabilitiesNamesAbsentToolsHonestly(t *testing.T) {
	s := tui.New(tui.Options{Model: "some/model"})
	got := capabilities(s, nil, nil)
	if !strings.Contains(got, "none available") {
		t.Errorf("capabilities = %q, want it to say no tools are available", got)
	}
}

// TestCapabilitiesReflectsCloudflareEitherWay covers both sides of the Cloudflare
// check: present when the callback says ready, absent when it says not.
func TestCapabilitiesReflectsCloudflareEitherWay(t *testing.T) {
	s := tui.New(tui.Options{Model: "some/model"})

	ready := capabilities(s, nil, func() bool { return true })
	if !strings.Contains(ready, "Cloudflare: a credential is configured") {
		t.Errorf("capabilities with Cloudflare ready = %q, want it named as available", ready)
	}

	notReady := capabilities(s, nil, func() bool { return false })
	if !strings.Contains(notReady, "Cloudflare: no credential is configured") {
		t.Errorf("capabilities with Cloudflare not ready = %q, want it named as unavailable", notReady)
	}

	nilCheck := capabilities(s, nil, nil)
	if !strings.Contains(nilCheck, "Cloudflare: no credential is configured") {
		t.Errorf("capabilities with a nil Cloudflare check = %q, want it treated as not ready", nilCheck)
	}
}
