package openrouter

// Request is one call to /chat/completions.
type Request struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
	Tools    []Tool    `json:"tools,omitempty"`
}

// EventKind labels an event.
//
// The kind labels the event rather than replacing it. A caller that already
// relied on the fields a stream chunk carried finds those fields untouched.
type EventKind int

const (
	// EventDelta is a piece of reply text.
	EventDelta EventKind = iota
	// EventUsage is the accounting the endpoint reported for a response.
	EventUsage
	// EventTool is a completed tool call, delivered once, at its terminator.
	EventTool
	// EventDone is the terminating [DONE] marker.
	EventDone
	// EventError is a failure. The text that arrived before it is kept.
	EventError
	// EventFinish is the end of a turn, with the reason the endpoint gave.
	EventFinish
)

// Event is one thing that happened during a stream.
type Event struct {
	Kind EventKind

	// Text is the reply text, for EventDelta.
	Text string

	// Usage is the accounting, for EventUsage.
	Usage *Usage

	// ToolCall is the completed call, for EventTool.
	ToolCall *ToolCall

	// Err is the failure, for EventError.
	Err error

	// Finished reports whether the stream ended on its terminator. It is false
	// for a stream that was cut short, which is the one case a caller must not
	// present as a complete reply.
	Finished bool

	// Reason is the finish reason the endpoint gave.
	Reason string
}

// Usage is the accounting for one response.
//
// Every figure is a pointer. A reported zero and an absent value are different,
// and a plain integer cannot tell them apart, so a response that spent nothing
// is distinguishable from one whose accounting never arrived.
type Usage struct {
	PromptTokens     *int     `json:"prompt_tokens"`
	CompletionTokens *int     `json:"completion_tokens"`
	TotalTokens      *int     `json:"total_tokens"`
	Cost             *float64 `json:"cost"`
	CostUSD          *float64 `json:"cost_usd"`
}
