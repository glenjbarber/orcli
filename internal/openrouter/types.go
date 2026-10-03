package openrouter

// Message is one turn of a conversation.
//
// Content carries text for both roles. The tool calls of an assistant turn are
// held in ToolCalls, and the answer to a call is held in the Content of a
// tool-role message, which is how the endpoint expects a round that called tools
// to be replayed.
type Message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	Name      string     `json:"name,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall is a request from the model to run a tool.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Index    int              `json:"index"`
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction is the name and arguments of a call.
//
// Arguments are a string because that is how the endpoint sends them. A caller
// that wants them as data unmarshals them, and a body that is not a JSON object
// is an error at that point rather than a tool call with no arguments.
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool is a tool offered to the model.
//
// The shape here is fixed by the endpoint. What a tool may do is decided by the
// tools package, and what a reader is asked before it happens is decided by the
// interface package, not here.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction declares a tool's parameters.
type ToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}
