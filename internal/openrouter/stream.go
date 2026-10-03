package openrouter

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
)

// maxTokenCount bounds a reported count.
//
// A figure above the platform maximum is clamped rather than refused. The count
// is shown to a reader and is what decides when to compact, and neither is worth
// losing a stream over: a wrong count is a wrong count, and a refused stream is
// a lost reply.
const maxTokenCount = math.MaxInt32

// unindexedKey is the key given to a tool call fragment that carries no index
// and has nothing to continue.
//
// It sorts after every index the wire can carry, so a call the endpoint did not
// number is delivered last rather than displacing one it did.
const unindexedKey = math.MaxInt32

// chunk is one data payload from the stream.
//
// Only the fields this client acts on are decoded. A field the endpoint sends
// that is not named here is ignored rather than refused, since the endpoint is
// free to add to the envelope and a client that rejected an unknown field would
// reject the stream along with it.
type chunk struct {
	Choices []struct {
		Delta struct {
			Content   string         `json:"content"`
			ToolCalls []toolFragment `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`

	// Error is a failure the endpoint reported inside the stream.
	//
	// The endpoint reports an upstream failure as a payload carrying an error
	// object and nothing else. A parser reading only Choices and Usage sees a
	// chunk with neither, reports nothing, and then tells the reader the stream
	// ended without its terminating marker, which names a symptom rather than
	// the cause.
	Error *reportedError `json:"error"`

	// Usage is held raw and decoded on its own, and that is the whole point of
	// it being raw. The endpoint regularly sends a count in a shape this client
	// cannot read, and a decode that failed over it would take the reply text
	// sitting in the same payload with it. Accounting is worth less than the
	// words beside it.
	Usage json.RawMessage `json:"usage"`
}

// reportedError is the failure the endpoint wrote into the stream.
//
// Only the message is read. The code beside it is quoted as a number by some
// failures and as a string by others, and nothing this client does depends on
// distinguishing them: the message is what a reader is shown and what [Chat]
// decides a retry on.
type reportedError struct {
	Message string `json:"message"`
}

// progress is what one stream attempt has delivered so far.
//
// It exists so the retry rule is decided in one place rather than in the reader
// loop. A request that has delivered nothing has spent nothing and a second
// attempt cannot duplicate text the reader has already seen, so it is the one
// case worth trying again; a request that has delivered something is a reply
// that was paid for, and the text before a cut is kept rather than taken back.
type progress struct {
	acc *assembler

	reason string
	marked bool

	// delivered counts what the reader has been shown: reply text and completed
	// tool calls.
	delivered int

	// reported is the failure the endpoint wrote into the stream, if it wrote
	// one. It is kept as text rather than raised at once, since whether it can
	// be retried depends on delivered.
	reported string

	// told records that a failure has already been shown to the reader, so the
	// missing terminating marker does not arrive as a second and vaguer one.
	told bool
}

// report delivers one event and counts it, so the retry rule sees everything the
// reader saw rather than only what the parser chose to tally.
func (p *progress) report(onEvent func(Event), e Event) {
	switch e.Kind {
	case EventDelta, EventTool:
		p.delivered++
	}
	onEvent(e)
}

// fail records a failure the endpoint reported inside the stream.
//
// A failure arriving before anything was delivered is held rather than shown:
// the request has not failed, this attempt has, and [Chat] has to be free to try
// again without the reader having been told a reply is broken. A failure after
// text has arrived is shown at once, because the reply is short and the cause is
// what the reader needs now.
func (p *progress) fail(c *Client, message string, onEvent func(Event)) {
	p.reported = message
	if p.delivered > 0 && !p.told {
		p.told = true
		onEvent(Event{
			Kind: EventError,
			Err:  fmt.Errorf("openrouter: %s", Filter(message, c.apiKey)),
		})
	}
}

// toolFragment is one piece of a tool call as the stream carries it.
//
// The endpoint splits a call across chunks: an opening fragment carries the id
// and the name, and the fragments after it carry pieces of the arguments. The
// index is the only field every fragment is guaranteed to carry, which is why
// it is the thing pieces are joined on.
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
	parts map[int]*ToolCall
	last  int
	held  bool
}

// add records one fragment.
//
// A fragment carrying an index joins the call at that index. One carrying none
// continues the call most recently started, which is what an unindexed
// continuation is, and starts a call of its own when there is none to continue.
func (a *assembler) add(f toolFragment) {
	if a.parts == nil {
		a.parts = make(map[int]*ToolCall)
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
		call = &ToolCall{}
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
//
// A call whose name never arrived is dropped. There would be nothing to run, and
// a call with no name handed to the interface is a request the interface cannot
// answer.
func (a *assembler) take() []ToolCall {
	if len(a.order) == 0 {
		return nil
	}

	keys := make([]int, len(a.order))
	copy(keys, a.order)
	sort.Ints(keys)

	var calls []ToolCall
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
// The terminating marker is required rather than assumed. A stream that ends
// without it was cut short, and reporting it as a finished reply is the one
// outcome this client exists to avoid, so an unmarked end produces an error
// event and a final event with Finished false. The text that arrived before the
// cut has already been reported and is not taken back.
//
// The return value is not for the reader. It carries errNothingDelivered when
// the endpoint reported a failure and nothing at all arrived, which tells [Chat]
// the attempt cost nothing and may be made again. Everything else a reader needs
// has already gone to the callback.
func (c *Client) stream(ctx context.Context, body io.Reader, onEvent func(Event)) error {
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
				onEvent(Event{
					Kind: EventError,
					Err:  fmt.Errorf("openrouter: read stream: %w", err),
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

	// The one case worth trying again. Nothing was delivered, so nothing was
	// spent and a second attempt cannot duplicate anything, and the endpoint
	// named the cause rather than leaving a cut stream to be guessed at.
	if p.reported != "" && p.delivered == 0 {
		return &undeliveredError{msg: Filter(p.reported, c.apiKey)}
	}

	// The cause is known already when it arrived after text, and repeating it as
	// a missing marker would report the symptom a second time and let it read as
	// the whole of what went wrong.
	if !p.marked && !p.told {
		onEvent(Event{
			Kind: EventError,
			Err:  errors.New("openrouter: stream ended without its terminating marker"),
		})
	}
	onEvent(Event{Kind: EventFinish, Finished: p.marked, Reason: p.reason})
	return nil
}

// dataOf returns the payload of a data line, and whether the line carried one.
//
// A line beginning with a colon is a comment and a blank line is a separator.
// Neither is a payload. Only the data field is read: the kind of an event is
// carried by what it contains rather than by the name beside it, so a rename
// upstream cannot change what this client does with it.
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

// handleChunk reports the events one payload produced, and whether that payload
// was the terminating marker.
func (c *Client) handleChunk(onEvent func(Event), p *progress, data string) bool {
	data = strings.TrimSpace(data)
	switch data {
	case "":
		return false
	case doneMarker:
		onEvent(Event{Kind: EventDone})
		return true
	}

	var ch chunk
	if err := json.Unmarshal([]byte(data), &ch); err != nil {
		// A payload that is not JSON is reported and reading continues. The
		// alternative is to end a stream over one line the endpoint sent in a
		// shape this parser does not know, which loses the reply with it.
		onEvent(Event{
			Kind: EventError,
			Err:  fmt.Errorf("openrouter: unreadable stream payload: %w", err),
		})
		return false
	}

	if ch.Error != nil && ch.Error.Message != "" {
		p.fail(c, ch.Error.Message, onEvent)
		return false
	}

	for _, choice := range ch.Choices {
		if choice.Delta.Content != "" {
			p.report(onEvent, Event{Kind: EventDelta, Text: choice.Delta.Content})
		}
		for _, f := range choice.Delta.ToolCalls {
			p.acc.add(f)
		}
		if choice.FinishReason != "" {
			// The finish reason terminates the calls the stream carried, so
			// what has been collected is delivered here rather than held until
			// the end, which may never come.
			p.reason = choice.FinishReason
			c.deliver(p, onEvent)
		}
	}

	// The accounting is read after the reply has been reported, and a figure
	// that cannot be read costs the accounting alone.
	if u := decodeUsage(ch.Usage); u != nil {
		onEvent(Event{Kind: EventUsage, Usage: u})
	}

	return false
}

// deliver reports the calls held by the assembler as finished tool calls.
func (c *Client) deliver(p *progress, onEvent func(Event)) {
	for _, call := range p.acc.take() {
		call := call
		p.report(onEvent, Event{Kind: EventTool, ToolCall: &call})
	}
}

// doneMarker is the payload that ends a stream.
const doneMarker = "[DONE]"

// decodeUsage reads the accounting, and reports nothing rather than failing
// when it cannot.
//
// A payload carrying no accounting decodes to nothing, and so does a payload
// carrying accounting in a shape this client cannot read. The second case is why
// this is separate from the reply: a figure this client cannot parse must not
// cost the words beside it, and a session with no cost for one response is a
// session with an inexact total, which the ledger already knows how to say.
func decodeUsage(raw json.RawMessage) *Usage {
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
//
// The counts are read through flexInt rather than into the fields of Usage
// directly, because a count written as a quoted string or in exponent form is a
// count this endpoint is known to send, and refusing the payload over it would
// cost the reply that carried it.
type usageWire struct {
	PromptTokens     *flexInt   `json:"prompt_tokens"`
	CompletionTokens *flexInt   `json:"completion_tokens"`
	TotalTokens      *flexInt   `json:"total_tokens"`
	Cost             *flexFloat `json:"cost"`
	CostUSD          *flexFloat `json:"cost_usd"`
}

// usage converts to the reported form, clamping what cannot be carried.
func (u *usageWire) usage() *Usage {
	if u == nil {
		return nil
	}
	out := &Usage{}
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
		return fmt.Errorf("openrouter: token count %q is not a number", s)
	}
	// A figure past the float range arrives as an infinity with ErrRange. It is
	// clamped rather than refused, since it is a count and the reply around it
	// is worth more than the count being exact.
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
		return fmt.Errorf("openrouter: cost %q is not a number", s)
	}
	*f = flexFloat(v)
	return nil
}
