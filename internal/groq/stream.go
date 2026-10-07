package groq

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/glenjbarber/orcli/internal/openrouter"
)

// This file is internal/openrouter's stream.go, read against the events and
// tool calls that package exports rather than its own private ones. See
// internal/groq/doc.go for why the two clients agree on the wire types but
// not on the parsing code behind them: nothing here is specific to
// OpenRouter's host, but it is also not shared code - each client reads SSE
// off its own response body and reports through its own onEvent callback,
// and a shared parser would need a third package with no client of its own to
// own the chunk and reportedError types below, which would be a shared
// dependency bought for roughly 250 lines whose logic genuinely never
// diverges between the two transports, up until the day Groq's own chunk
// shape (it is, after all, a different implementation of the same OpenAI API
// behind a different host) drifts from OpenRouter's and the shared package
// has to grow a branch for it anyway.

// maxTokenCount bounds a reported count, mirroring internal/openrouter's own.
const maxTokenCount = math.MaxInt32

// unindexedKey is the key given to a tool call fragment that carries no
// index and has nothing to continue.
const unindexedKey = math.MaxInt32

// chunk is one data payload from the stream, the same shape
// internal/openrouter's own chunk decodes.
type chunk struct {
	Choices []struct {
		Delta struct {
			Content   string         `json:"content"`
			ToolCalls []toolFragment `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`

	Error *reportedError `json:"error"`

	Usage json.RawMessage `json:"usage"`
}

// reportedError is the failure the endpoint wrote into the stream.
type reportedError struct {
	Message string `json:"message"`
}

// progress is what one stream attempt has delivered so far.
type progress struct {
	acc *assembler

	reason string
	marked bool

	delivered int

	reported string

	told bool
}

// report delivers one event and counts it.
func (p *progress) report(onEvent func(openrouter.Event), e openrouter.Event) {
	switch e.Kind {
	case openrouter.EventDelta, openrouter.EventTool:
		p.delivered++
	}
	onEvent(e)
}

// fail records a failure the endpoint reported inside the stream.
func (p *progress) fail(c *Client, message string, onEvent func(openrouter.Event)) {
	p.reported = message
	if p.delivered > 0 && !p.told {
		p.told = true
		onEvent(openrouter.Event{
			Kind: openrouter.EventError,
			Err:  fmt.Errorf("groq: %s", Filter(message, c.apiKey)),
		})
	}
}

// toolFragment is one piece of a tool call as the stream carries it.
type toolFragment struct {
	Index    *int   `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// assembler collects tool call fragments until a call is complete.
type assembler struct {
	order []int
	parts map[int]*openrouter.ToolCall
	last  int
	held  bool
}

// add records one fragment.
func (a *assembler) add(f toolFragment) {
	if a.parts == nil {
		a.parts = make(map[int]*openrouter.ToolCall)
	}

	key := unindexedKey
	switch {
	case f.Index != nil:
		key = *f.Index
	case a.held:
		key = a.last
	}

	call, ok := a.parts[key]
	if !ok {
		call = &openrouter.ToolCall{}
		a.parts[key] = call
		a.order = append(a.order, key)
	}
	a.last, a.held = key, true

	if f.ID != "" {
		call.ID = f.ID
	}
	if f.Type != "" {
		call.Type = f.Type
	}
	if f.Function.Name != "" {
		call.Function.Name = f.Function.Name
	}
	if f.Function.Arguments != "" {
		call.Function.Arguments += f.Function.Arguments
	}
}

// take returns the completed calls in wire index order and empties the
// assembler.
func (a *assembler) take() []openrouter.ToolCall {
	if len(a.order) == 0 {
		return nil
	}

	keys := make([]int, len(a.order))
	copy(keys, a.order)
	sort.Ints(keys)

	var calls []openrouter.ToolCall
	for _, k := range keys {
		if call := a.parts[k]; call.Function.Name != "" {
			calls = append(calls, *call)
		}
	}

	a.order, a.parts, a.held = nil, nil, false
	return calls
}

// stream reads Server-Sent Events from body and reports what it finds.
//
// See [openrouter.Client.stream]'s own doc comment for the rules this follows
// line for line: the terminating marker is required, text that arrived
// before a cut is kept, and errNothingDelivered - here, [undeliveredError] -
// is what tells [Client.Chat] an attempt cost nothing and may be retried.
func (c *Client) stream(ctx context.Context, body io.Reader, onEvent func(openrouter.Event)) error {
	br := bufio.NewReader(body)
	p := &progress{acc: &assembler{}}

	for {
		line, err := br.ReadString('\n')
		if line != "" {
			if data, ok := dataOf(line); ok && c.handleChunk(onEvent, p, data) {
				p.marked = true
			}
		}
		if err != nil {
			if err != io.EOF {
				onEvent(openrouter.Event{
					Kind: openrouter.EventError,
					Err:  fmt.Errorf("groq: read stream: %w", err),
				})
				return nil
			}
			break
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}

	c.deliver(p, onEvent)

	if p.reported != "" && p.delivered == 0 {
		return &undeliveredError{msg: Filter(p.reported, c.apiKey)}
	}

	if !p.marked && !p.told {
		onEvent(openrouter.Event{
			Kind: openrouter.EventError,
			Err:  errors.New("groq: stream ended without its terminating marker"),
		})
	}
	onEvent(openrouter.Event{Kind: openrouter.EventFinish, Finished: p.marked, Reason: p.reason})
	return nil
}

// dataOf returns the payload of a data line, and whether the line carried
// one.
func dataOf(line string) (string, bool) {
	line = strings.TrimRight(line, "\r\n")
	if line == "" || strings.HasPrefix(line, ":") {
		return "", false
	}
	rest, ok := strings.CutPrefix(line, "data:")
	if !ok {
		return "", false
	}
	return strings.TrimPrefix(rest, " "), true
}

// handleChunk reports the events one payload produced, and whether that
// payload was the terminating marker.
func (c *Client) handleChunk(onEvent func(openrouter.Event), p *progress, data string) bool {
	data = strings.TrimSpace(data)
	switch data {
	case "":
		return false
	case doneMarker:
		onEvent(openrouter.Event{Kind: openrouter.EventDone})
		return true
	}

	var ch chunk
	if err := json.Unmarshal([]byte(data), &ch); err != nil {
		onEvent(openrouter.Event{
			Kind: openrouter.EventError,
			Err:  fmt.Errorf("groq: unreadable stream payload: %w", err),
		})
		return false
	}

	if ch.Error != nil && ch.Error.Message != "" {
		p.fail(c, ch.Error.Message, onEvent)
		return false
	}

	for _, choice := range ch.Choices {
		if choice.Delta.Content != "" {
			p.report(onEvent, openrouter.Event{Kind: openrouter.EventDelta, Text: choice.Delta.Content})
		}
		for _, f := range choice.Delta.ToolCalls {
			p.acc.add(f)
		}
		if choice.FinishReason != "" {
			p.reason = choice.FinishReason
			c.deliver(p, onEvent)
		}
	}

	if u := decodeUsage(ch.Usage); u != nil {
		onEvent(openrouter.Event{Kind: openrouter.EventUsage, Usage: u})
	}

	return false
}

// deliver reports the calls held by the assembler as finished tool calls.
func (c *Client) deliver(p *progress, onEvent func(openrouter.Event)) {
	for _, call := range p.acc.take() {
		call := call
		p.report(onEvent, openrouter.Event{Kind: openrouter.EventTool, ToolCall: &call})
	}
}

// doneMarker is the payload that ends a stream.
const doneMarker = "[DONE]"

// decodeUsage reads the accounting, and reports nothing rather than failing
// when it cannot.
func decodeUsage(raw json.RawMessage) *openrouter.Usage {
	if len(raw) == 0 {
		return nil
	}
	var u usageWire
	if err := json.Unmarshal(raw, &u); err != nil {
		return nil
	}
	return u.usage()
}

// usageWire is the accounting as the endpoint writes it.
type usageWire struct {
	PromptTokens     *flexInt   `json:"prompt_tokens"`
	CompletionTokens *flexInt   `json:"completion_tokens"`
	TotalTokens      *flexInt   `json:"total_tokens"`
	Cost             *flexFloat `json:"cost"`
	CostUSD          *flexFloat `json:"cost_usd"`
}

// usage converts to the reported form, clamping what cannot be carried.
func (u *usageWire) usage() *openrouter.Usage {
	if u == nil {
		return nil
	}
	out := &openrouter.Usage{}
	if u.PromptTokens != nil {
		v := u.PromptTokens.count()
		out.PromptTokens = &v
	}
	if u.CompletionTokens != nil {
		v := u.CompletionTokens.count()
		out.CompletionTokens = &v
	}
	if u.TotalTokens != nil {
		v := u.TotalTokens.count()
		out.TotalTokens = &v
	}
	if u.Cost != nil {
		v := float64(*u.Cost)
		out.Cost = &v
	}
	if u.CostUSD != nil {
		v := float64(*u.CostUSD)
		out.CostUSD = &v
	}
	return out
}

// flexInt reads a count written as a number, a quoted number, or in exponent
// form.
type flexInt float64

// UnmarshalJSON implements json.Unmarshaler.
func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return fmt.Errorf("groq: token count %q is not a number", s)
	}
	*f = flexInt(v)
	return nil
}

// count returns the count as an int, clamped to what can be carried.
func (f flexInt) count() int {
	switch {
	case math.IsNaN(float64(f)), math.IsInf(float64(f), -1):
		return 0
	case float64(f) > maxTokenCount:
		return maxTokenCount
	case float64(f) < 0:
		return 0
	}
	return int(f)
}

// flexFloat reads a cost written as a number or a quoted number.
type flexFloat float64

// UnmarshalJSON implements json.Unmarshaler.
func (f *flexFloat) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return fmt.Errorf("groq: cost %q is not a number", s)
	}
	*f = flexFloat(v)
	return nil
}
