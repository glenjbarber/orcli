package tui

import (
	"context"
	"errors"
	"sync"
)

// Options is what a session is built from.
//
// Everything the interface needs is here rather than read from the environment or
// from a file of its own. The credential, the model and the preferences have
// already been read by whoever started the program, and a session that went back
// for them would be a second reader of a file holding a key.
type Options struct {
	// APIKey is the credential. It is held here and never written to a row.
	//
	// A log is a thing a reader selects out of and pastes somewhere else, so a
	// credential that reached one would be a credential in the clipboard, in the
	// scrollback, and in whatever the reader pasted it into. Nothing here formats
	// it and nothing here filters for it, because nothing here prints it.
	APIKey string

	// Model is the model a turn is sent to. An empty one is reported rather than
	// sent, since a request with no model is a request the endpoint cannot
	// answer and the reader is better served by being told which key to press.
	Model string

	// Provider names the endpoint host, for the status bar.
	Provider string

	// Approval is the session approval mode: ask, allow, or deny.
	//
	// It is named rather than taken from the configuration, since the
	// configuration holds a string that has to be parsed and a parse failure at
	// startup is better than a parse failure at the first tool call.
	Approval Approval

	// WorkingDir is the directory tools are contained to.
	WorkingDir string

	// Color reports whether colour output is on.
	Color bool

	// Bell reports whether the terminal bell is rung when a reply finishes
	// arriving.
	Bell bool

	// Mouse reports whether mouse reporting is on.
	Mouse bool
}

// Approval is the mode a tool call is settled under.
//
// It lives here rather than in internal/config so that the interface names what it
// does about a call rather than importing a configuration type to look at, and so
// that a session constructed in a test carries one value rather than a file.
type Approval string

// The three modes, spelled as the reader writes them.
//
// A refusal is named rather than defaulted to: a session that cannot say what it
// would do about a program that writes is a session that should not be running
// tools at all.
const (
	// ApprovalAsk means every call the rules do not settle is put to the reader.
	ApprovalAsk Approval = "ask"
	// ApprovalAllow means a call runs without asking, within the allowlist.
	ApprovalAllow Approval = "allow"
	// ApprovalDeny means nothing runs.
	ApprovalDeny Approval = "deny"
)

// Session owns the log and the state the footer stack reports.
//
// It is the thing a Run drives, and it is deliberately small: the log, the four
// states, and the counters the bars show. Everything that has a decision in it,
// the terminal control, the line editor, the palette and the command table, is a
// separate concern with its own file, and a session that grew all of them would
// be the one file that decides everything.
type Session struct {
	// Log is the record of what has been written. It is a value rather than a
	// pointer so a Session is one thing rather than two that can disagree.
	log Log

	opts Options

	mu     sync.RWMutex
	state  State
	detail string
}

// State is what the client is doing, and it is one of four.
//
// The four are named rather than derived from a progress flag, because a reader
// needs to know which of them they are in and a boolean pair answers that in two
// places. `thinking` is the state a turn is in while the model is composing and
// before any of it has been written, which is the whole window in which the
// reader would otherwise have no idea anything was happening.
type State string

const (
	// StateIdle is nothing running. The reader may type.
	StateIdle State = "idle"
	// StateThinking is a turn running and composing its first text.
	StateThinking State = "thinking"
	// StateWorking is a turn running with something already delivered.
	StateWorking State = "working"
	// StatePaused is the reader holding the log still while a turn continues.
	StatePaused State = "paused"
)

// New returns a session holding the log and nothing else.
//
// The log starts with one row: the banner. A session that opens onto an empty
// screen gives a reader nothing to tell it has started, and a row that names the
// model is the one line of state a reader wants before typing anything.
func New(opts Options) *Session {
	s := &Session{opts: opts, state: StateIdle}
	s.log.Append(Row{
		Kind:  KindNotice,
		Level: 0,
		Text:  "orcli, a log and nothing else yet",
		Spans: []Span{{Start: 0, End: 5, Role: RoleEmphasis}},
	})
	return s
}

// Log returns the rows the session has written.
//
// It is a method rather than a field so that a caller cannot hold the Log and a
// Session separately and have them disagree about what was written.
func (s *Session) Log() *Log { return &s.log }

// Options returns what the session was built with.
func (s *Session) Options() Options { return s.opts }

// State reports what the client is doing, and any detail worth showing beside it.
//
// The detail is a second value rather than a suffix on the state, because the state
// is one of four and a fifth state spelled `paused, buffered 12 rows` would be a
// state rather than a state and a note, and a bar that grows a comma is a bar no
// reader can scan.
func (s *Session) State() (State, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state, s.detail
}

// SetState moves the session to a state.
//
// The transition is not checked. A session that refuses a transition it did not
// expect has to hold the state somewhere to refuse it, and the place it holds it
// is the place the caller already wrote, which makes the check a second source of
// truth about the same thing. The rule that matters is enforced by the caller:
// nothing may claim to be idle while a turn is running, since that is what tells a
// reader they can walk away.
func (s *Session) SetState(state State, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state, s.detail = state, detail
}

// ErrNoModel reports a turn that has nothing to send.
//
// It is an error rather than an empty request, since the endpoint would answer a
// request with no model with a refusal that names the model rather than the
// reader, and the reader is the one who knows which key to press.
var ErrNoModel = errors.New("no model is chosen, so there is nothing to ask")

// Ready reports whether a turn can be sent, and why not when it cannot.
//
// It exists so that the check lives in one place. Every caller that wants to start
// a turn asks this rather than looking at the model itself, and a session that let
// one path send a request with no model and another refuse it is a session where
// the reader finds out by reading an error from the endpoint.
func (s *Session) Ready() error {
	if s.opts.Model == "" {
		return ErrNoModel
	}
	return nil
}

// Begin turns a question into a turn and reports what it took.
//
// The context is the caller's so that stopping a model stops the request and not
// the session, which is the only reason a turn can be stopped without leaving. The
// level is what the rows will be attributed to, and it is fixed here rather than
// looked up later: a row that took its level when it was written cannot be
// re-attributed by something that changed while the turn ran.
//
// The conversation is not consulted here. A session this size holds no turns yet,
// and the thing that will hold them is a separate concern, so this returns what a
// caller needs to start one and says nothing about what it will carry.
func (s *Session) Begin(ctx context.Context, question string, level int) (context.Context, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}
	if question == "" {
		return nil, errors.New("no question was given")
	}

	s.log.Append(Row{
		Kind:  KindQuestion,
		Level: level,
		Text:  question,
		Spans: []Span{{Start: 0, End: len(question), Role: RoleEmphasis}},
	})

	s.SetState(StateThinking, "")
	_ = ctx
	return ctx, nil
}

// Deliver records what a turn produced and moves the session out of thinking.
//
// A milestone is one line, since a log that grows a row per token is a log that
// scrolls past what the reader was reading. The first delivery moves the state from
// thinking to working, and that transition is the whole reason both exist: the
// window before the first one is the only time the reader has no evidence the
// turn is running at all.
//
// Text is written whole rather than a piece at a time, which is the decision the
// interface was redesigned for. A reply held for its turn arrives as a block, so
// the folding and the copy path see the same text the reader does.
func (s *Session) Deliver(text string, level int) {
	if text == "" {
		return
	}

	s.mu.Lock()
	thinking := s.state == StateThinking
	s.mu.Unlock()

	if thinking {
		s.SetState(StateWorking, "")
	}

	s.log.Append(Row{Kind: KindReply, Level: level, Text: text})
}

// Notice records a message from the client: a refusal, a failure, a milestone.
//
// It is a method rather than a caller reaching for the log, so that a notice lands
// in the order it happened even when the turn goroutine and the input goroutine are
// both writing, and so that a caller cannot forget the level and have a row
// attributed to the wrong responder.
func (s *Session) Notice(text string, level int, role Role) {
	s.log.Append(Row{
		Kind:  KindNotice,
		Level: level,
		Text:  text,
		Spans: []Span{{Start: 0, End: len(text), Role: role}},
	})
}

// Finished records that a turn ended and returns the session to idle.
//
// A turn that ended is idle whether it finished cleanly or was stopped, since a
// reader who stopped a model should not be told something is still running. The
// detail carries why, so the bar says `idle, stopped` rather than making the
// reader work out which kind of not-running this is.
func (s *Session) Finished(reason string) {
	s.log.Append(Row{
		Kind:  KindNotice,
		Level: 0,
		Text:  reason,
		Spans: []Span{{Start: 0, End: len(reason), Role: RoleDim}},
	})
	s.SetState(StateIdle, "")
}

// RowsAt returns the rows at a level, which is what `/copy N` copies.
//
// It is the level and not the row number, since a fold changes how many rows there
// are and a reader who counted them would be counting something that moves. A level
// names one exchange and covers every row of it, tool lines included, which is what
// copying a responder's whole answer means.
func (s *Session) RowsAt(level int) []Row {
	var out []Row
	for _, row := range s.log.Rows() {
		if row.Level == level {
			out = append(out, row)
		}
	}
	return out
}

// Levels reports the levels present in the log, lowest first.
//
// A reader typing `/copy 3` needs to know whether a 3 exists before pressing enter,
// and a bar listing them is how they know. The order is by value rather than by
// first appearance so the list does not reorder as a turn runs.
func (s *Session) Levels() []int {
	seen := map[int]bool{}
	for _, row := range s.log.Rows() {
		seen[row.Level] = true
	}

	out := make([]int, 0, len(seen))
	for level := range seen {
		out = append(out, level)
	}
	sortInts(out)
	return out
}

// sortInts orders a small slice in place.
//
// It is a function rather than a call into sort because the levels are a handful
// of integers and the package has no other use for sort, and a helper here keeps
// that visible rather than leaving a general import for one call.
func sortInts(xs []int) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}
