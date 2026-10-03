// Package saved reads and writes session files.
//
// A saved conversation is one SQLite file, and one file per save rather than one
// database of many. That is what lets a file be handed to somebody else, queried on
// its own with any SQLite tool, or deleted without touching anything else.
//
// The format is a database rather than a transcript on purpose. A reader who opens
// a saved conversation with their own tools should be able to read the turns as they
// are, without this client having to be involved, and that means the ordinary columns
// have to stay ordinary: role and content are plain text in a plain column, and
// anything this client added goes in an envelope beside them.
//
// # Versioning
//
// A file carries the schema version it was written by. A file written by an older
// version is still read. A file written by a later version is refused rather than
// half-read, since a conversation missing the turns a tool call made reads as though
// the model never called anything, and that is worse than being told the file cannot
// be opened here.
//
// # The driver
//
// The driver is pure Go, which matters because make crossbuild sets GOOS without a C
// toolchain on this host. A driver that compiles C produces a binary that builds for
// every target and then fails at the first query, and a cross-build gate that cannot
// cross-build stops meaning anything.
package saved

import (
	"errors"
	"fmt"
)

// schemaVersion is the version this client writes.
//
// Version 2 added one nullable column for the turns a tool call makes. One column
// carrying an envelope, not a column per field, is what leaves room for a field added
// later without migrating the files already written.
const schemaVersion = 2

// minimumReadable is the oldest version this client can read.
const minimumReadable = 1

// ErrNoStore is returned when no session file exists at the path asked for.
//
// It is a state rather than a fault: a session named by a reader who has not saved
// one yet is a session they are about to create.
var ErrNoStore = errors.New("saved: no saved session at that path")

// ErrFutureVersion is returned when a file was written by a later client.
//
// It is refused rather than half-read. A conversation missing the turns a tool call
// made reads as though the model never called anything, which is a wrong answer
// rather than a missing one.
var ErrFutureVersion = errors.New("saved: the session was written by a later version")

// ErrNotAnObject is returned when the turns envelope is not what it should be.
var ErrNotAnObject = errors.New("saved: the turns are not a JSON object")

// Session is a saved conversation.
//
// The fields are the conversation as a reader sees it. The counts are stored so a
// loaded session reports what it cost rather than starting again at nothing, since a
// session ledger belongs to the session and money already spent is not undone by
// clearing the conversation it was spent on.
type Session struct {
	// Name is what the file is called, and what the reader typed to get it.
	Name string

	// Model is the model the conversation was held with.
	Model string

	// Turns are the messages, in order.
	Turns []Turn

	// PromptTokens, CompletionTokens and TotalTokens are the counts restored from
	// the file. They are per conversation and are restored by a load, unlike the
	// session ledger, which is not.
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// Turn is one message in a saved conversation.
//
// The columns are plain and the envelope is beside them. Role and content are what a
// reader can query with any SQLite tool, which is the whole reason the format is a
// database and not a transcript.
type Turn struct {
	// Role is who said it: system, user, assistant, or tool.
	Role string

	// Content is what was said.
	Content string

	// ToolCalls is the envelope, carrying the turns an assistant turn's tool calls
	// made. It is nil for an ordinary turn rather than empty, so a column added
	// later has somewhere to go without migrating what is already written.
	ToolCalls []ToolTurn

	// Name is the tool a tool-role turn answered, which is how the answer is
	// matched to the call that asked for it.
	Name string

	// ToolCallID is the call this turn is the answer to.
	ToolCallID string
}

// ToolTurn is one call an assistant turn made.
type ToolTurn struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Index    int    `json:"index"`
	Name     string `json:"name"`
	Args     string `json:"args"`
	Result   string `json:"result,omitempty"`
	Rejected string `json:"rejected,omitempty"`
}

// Empty reports whether a turn carries no envelope.
//
// An ordinary turn stores no envelope at all rather than an empty one, which is what
// leaves room for a field added later without migrating the files already written.
func (t Turn) Empty() bool { return len(t.ToolCalls) == 0 }

// describeVersion renders a version mismatch for a reader.
//
// The numbers are in the message rather than only in the sentinel, since a reader
// being told "the session was written by a later version" learns less than one told
// which version wrote it and which is reading it.
func describeVersion(wrote, reads int) error {
	return fmt.Errorf("%w: it was written by version %d and this client reads up to %d",
		ErrFutureVersion, wrote, reads)
}
