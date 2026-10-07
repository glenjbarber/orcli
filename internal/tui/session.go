// Package tui draws the interface.
//
// The screen is a frame: five fixed rows below a scrollback region that takes
// whatever height is left. The fixed rows are the pane bar, two status bars, one
// blank separator line, and the prompt, in that order from the bottom; see the
// comment above barRows in stack.go for their order and rationale. The
// scrollback above them holds the log: a downward-scrolling sequence of rows,
// newest just above the prompt, oldest pushed off the top as it grows. This file
// holds that log, along with the session and the terminal check.
//
// # What this file is not
//
// It is not the whole package. The palette, the line editor, the terminal
// control, and the command table are separate concerns with their own files and
// their own decisions.
//
// # History
//
// An earlier version of this file described a log with no frame at all: the
// alternate screen given up, and the reader's own scrollback carrying the
// transcript, because the frame that preceded it was redrawn whole at every
// repaint, which is the wrong model for output that arrives over minutes. That
// was superseded (2026-10-06) when the frame returned, this time redrawing only
// what changed rather than the whole screen at every repaint (see paint in
// run.go); it carries forward loreloom/UI-redesign.md's shape, which stack.go
// describes.
package tui

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
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
	// sent, since a request with no model is a request the endpoint cannot answer
	// and the reader is better served by being told which key to press.
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

	// BreakInterval is how long the session runs before a screen-break
	// reminder interrupts it. Zero turns the reminder off: a caller that wants
	// no reminder at all says so with the zero value rather than with a
	// sentinel duration that reads as a figure somebody chose.
	BreakInterval time.Duration

	// BreakBell reports whether the terminal bell is rung when a screen break
	// starts. It is independent of Bell above, which is about a reply arriving
	// rather than about a break starting.
	BreakBell bool

	// Mouse reports whether mouse reporting is on.
	Mouse bool

	// Cognito reports that nothing is to be recorded.
	//
	// It is a field rather than something the session infers, because the promise
	// is about the host and not only about the conversation. A worker that writes
	// a file records what it did even though the transcript records nothing, and a
	// reader who asked for nothing recorded should not get a findings file.
	Cognito bool

	// PaneActiveColor is the colour the pane bar is drawn in while a worker is
	// running at the shown pane. Nil leaves the bar in chrome, the way it drew
	// before this existed, and is what a session built with no opinion about it
	// (every test in this package but the ones that ask for it) gets.
	//
	// It is separate from Color above on purpose: Color is the plain on/off
	// toggle this package already had, and this is a second, distinct pair of
	// colours for pane state that only applies once Color is on.
	PaneActiveColor *RGB

	// PaneDoneColor is the colour the pane bar is drawn in once a worker at the
	// shown pane has finished on its own, and keeps drawing in until another
	// worker starts there. See Session.PaneState for why "until the next one
	// starts" rather than a timed flash: there is no clock in the frame to lose
	// a race against, and a reader who has not looked back since still finds
	// the pane coloured the way they left it.
	PaneDoneColor *RGB
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

// Session owns the log, the levels and the state the frame reports.
//
// It is the thing a Run drives, and it is deliberately small: the log, the levels,
// the workers, the four states, and the counters the frame draws. Everything with a
// decision in it, the terminal control, the line editor, the palette and the command
// table, is a separate concern with its own file, and a session that grew all of them
// would be the one file that decides everything.
type Session struct {
	// log is the record of what has been written. It is a value rather than a
	// pointer so a Session is one thing rather than two that can disagree.
	log Log

	// levels is the table of thread identities. It has its own lock rather than
	// sharing the session's, since a level is asked about while a turn is running
	// and holding the state lock for that would block the painter.
	levels *levels

	opts Options

	// workers are the background turns this session started.
	//
	// They are under the session's own lock rather than the levels' lock, because a
	// worker is asked about by the reader while a turn is running and the levels'
	// lock is held briefly for the table rather than for the duration of a turn.
	workers []*Worker

	// editor is the field on the prompt row.
	//
	// It lives here rather than on the loop that draws it, because 0000022 settled
	// that the prompt belongs to the focused session and a second pane needs a
	// field of its own rather than one shared with the pane it is not showing.
	// Nothing else touches it concurrently, so it carries no lock of its own, the
	// way it carried none when the loop held it.
	editor Editor

	// history is the record of lines this session has sent, and the walk Up
	// and Down take through it.
	//
	// It lives here rather than on the editor for the same reason the editor
	// itself lives here and not on the loop that draws it: DESIGN.md §3
	// gives a session the input history as something it owns, alongside the
	// messages and the transcript, so a second pane gets a history of its
	// own rather than one shared with the pane it is not showing. Nothing
	// else touches it concurrently, so, like the editor, it carries no lock
	// of its own.
	history History

	mu     sync.RWMutex
	state  State
	detail string

	// cancel stops the turn now in flight, or is nil when there is none.
	//
	// It is guarded by the same lock as state rather than a lock of its own, since a
	// turn belongs to the conversation it was asked of in the same way state does,
	// and a second pane will want a cancel of its own rather than one shared with
	// the pane it is not showing.
	cancel context.CancelFunc

	// scroll is how many rows back from the live edge the viewport sits. Zero is the
	// live edge. It is guarded by the same lock as state for the same reason cancel
	// is: Shift+Up/Shift+Down move it from the input goroutine while paint reads it
	// from the same or a timer goroutine.
	scroll int

	// lastBreak is when the last screen break ended, or when the session
	// started if none has yet. BreakDue measures from it rather than from a
	// ticking countdown of its own, so the figure survives whatever paused or
	// resumed the repaint tick without drifting.
	lastBreak time.Time

	// breakEnd is when the break in progress is due to end. It is the zero
	// time while no break is running, which TickBreak never mistakes for a due
	// time because it only reads this field while state is StateBreak.
	breakEnd time.Time
}

// State is what the client is doing, and it is one of four.
//
// The four are named rather than derived from a progress flag, because a reader
// needs to know which of them they are in and a boolean pair answers that in two
// places. `thinking` is the state a turn is in while the model is composing and
// before any of it has been written, which is the whole window in which the reader
// would otherwise have no idea anything was happening.
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
	// StateBreak is a screen-break reminder showing, counting down to when the
	// reader may resume.
	StateBreak State = "break"
)

// breakDuration is how long a screen break lasts once it starts.
//
// It is fixed rather than configurable: the approved design makes the
// interval before a break the configurable figure and the break itself a
// fixed two minutes, the length an eye needs to recover from near focus.
const breakDuration = 2 * time.Minute

// New returns a session holding the log and nothing else.
//
// The log starts with one row: the banner. A session that opens onto an empty screen
// gives a reader nothing to tell it has started, and a row that names the program is
// the one line of state a reader wants before typing anything.
func New(opts Options) *Session {
	s := &Session{opts: opts, state: StateIdle, levels: newLevels(), lastBreak: time.Now(), history: NewHistory()}
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
//
// It is a copy rather than the field, so a caller cannot reach into the session's
// own options and change what a turn is sent as.
func (s *Session) Options() Options { return s.opts }

// SetModel changes the model a turn is sent to.
//
// It is a method rather than a caller reaching into the options, since a command
// that wrote the model to the file and left the session answering with the old one
// would leave the frame drawing one model and the reader being answered by another.
// The two are one decision and this is where it is kept.
//
// It is taken under the session lock for the reason the state is: a command runs on
// the input goroutine and a turn reads the model on the request goroutine, and the
// two would otherwise be reaching for the same field without a lock between them.
//
// An empty model is refused rather than stored, since an empty model is a session
// that cannot ask anything and a caller storing one has a bug in it rather than a
// reader who asked for it.
func (s *Session) SetModel(model string) error {
	if model == "" {
		return ErrNoModel
	}

	s.mu.Lock()
	s.opts.Model = model
	s.mu.Unlock()
	return nil
}

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
// expect has to hold the state somewhere to refuse it, and the place it holds it is
// the place the caller already wrote, which makes the check a second source of truth
// about the same thing. The rule that matters is enforced by the caller: nothing may
// claim to be idle while a turn is running, since that is what tells a reader they
// can walk away.
func (s *Session) SetState(state State, detail string) {
	s.mu.Lock()
	s.state, s.detail = state, detail
	s.mu.Unlock()
}

// Editor returns the field on the prompt row.
//
// It returns a pointer so a caller can reach Editor's own mutating methods
// directly, the way the interface loop did when the field was its own. There is
// one editor per session, matching the one prompt a focused session shows.
func (s *Session) Editor() *Editor {
	return &s.editor
}

// History returns the record of lines this session has sent.
//
// It returns a pointer for the same reason Editor does: a caller reaches
// History's own Record, Up and Down directly, and there is one history per
// session, matching the one input field a focused session shows.
func (s *Session) History() *History {
	return &s.history
}

// SetCancel records the cancel for the turn now starting.
//
// It is called once, from the goroutine that just started the turn, before that
// goroutine does anything else that might race with a reader pressing escape.
func (s *Session) SetCancel(cancel context.CancelFunc) {
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
}

// ClearCancel drops the cancel without calling it.
//
// It is used once a turn has already returned on its own, so a reader who
// presses escape afterwards finds nothing left to stop.
func (s *Session) ClearCancel() {
	s.mu.Lock()
	s.cancel = nil
	s.mu.Unlock()
}

// StopTurn takes the cancel for the turn in flight, if there is one, and clears
// it, so a second call finds nothing left to take.
//
// The caller invokes what is returned. Taking and clearing under one lock, rather
// than reading the field and clearing it in two steps, is what keeps two readers
// pressing escape together from both receiving the same cancel.
func (s *Session) StopTurn() context.CancelFunc {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	return cancel
}

// ScrollUp moves the viewport back by n rows, away from the live edge.
//
// It is clamped to the log's own length rather than to a screen height, since the
// session does not know how tall the frame drawing it is; a frame asked to draw an
// offset past what it can show clamps that itself at draw time. Shift+Up/Shift+Down
// are the keys chosen for this (2026-10-06), distinct from the plain arrow keys, which
// are reserved for history, which Up/Down now walk.
func (s *Session) ScrollUp(n int) {
	s.mu.Lock()
	s.scroll += n
	if max := s.log.Len(); s.scroll > max {
		s.scroll = max
	}
	s.mu.Unlock()
}

// ScrollDown moves the viewport toward the live edge by n rows, floored there.
func (s *Session) ScrollDown(n int) {
	s.mu.Lock()
	s.scroll -= n
	if s.scroll < 0 {
		s.scroll = 0
	}
	s.mu.Unlock()
}

// ScrollOffset reports how many rows back from the live edge the viewport sits. Zero
// is the live edge itself.
func (s *Session) ScrollOffset() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.scroll
}

// AtLiveEdge reports whether the viewport is at the live edge, which is what bar two's
// scrollback field reads (DESIGN.md §5: this is a fact read each redraw, not a stored
// mode).
func (s *Session) AtLiveEdge() bool {
	return s.ScrollOffset() == 0
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
// one path send a request with no model and another refuse it is a session where the
// reader finds out by reading an error from the endpoint.
func (s *Session) Ready() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.opts.Model == "" {
		return ErrNoModel
	}
	return nil
}

// Begin turns a question into a turn and reports what it took.
//
// The context is the caller's so that stopping a model stops the request and not
// the session, which is the only reason a turn can be stopped without leaving.
//
// The level is the thread the rows will be attributed to. It is not derived from
// the session here: a caller holds the level it asked for and passes it back, so
// the level a row carries is the level the thread was given rather than the level
// the session happens to be in when the row is written.
//
// The conversation is not consulted. A session this size holds no turns yet, and the
// thing that will hold them is a separate concern, so this returns what a caller needs
// to start one and says nothing about what it will carry.
func (s *Session) Begin(ctx context.Context, question string, level int) (context.Context, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}
	if question == "" {
		return nil, ErrNoQuestion
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
// thinking to working, and that transition is the whole reason both exist: the window
// before the first one is the only time the reader has no evidence the turn is
// running at all.
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
// It is a method rather than a caller reaching for the log, so that a notice lands in
// the order it happened even when the turn goroutine and the input goroutine are both
// writing, and so that a caller cannot forget the level and have a row attributed to
// the wrong responder.
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
// detail carries why, so the bar says `idle, stopped` rather than making the reader
// work out which kind of not-running this is.
func (s *Session) Finished(reason string) {
	s.log.Append(Row{
		Kind:  KindNotice,
		Level: 0,
		Text:  reason,
		Spans: []Span{{Start: 0, End: len(reason), Role: RoleDim}},
	})
	s.SetState(StateIdle, "")
}

// BreakDue reports whether a screen-break reminder is due at now.
//
// It is due only while the session is idle. A turn in flight is not
// interrupted by a reminder unrelated to it, and a break already showing is
// not due a second time, so a caller can poll this on every tick without
// guarding it itself.
func (s *Session) BreakDue(now time.Time) bool {
	if s.opts.BreakInterval <= 0 {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.state != StateIdle {
		return false
	}
	return now.Sub(s.lastBreak) >= s.opts.BreakInterval
}

// StartBreak begins a screen-break reminder at now, lasting breakDuration.
//
// The prompt is cleared, matching the approved interaction: the break shows a
// blank input box rather than one still holding what the reader had typed,
// and that input box stays usable through the break rather than being locked.
func (s *Session) StartBreak(now time.Time) {
	s.mu.Lock()
	s.state = StateBreak
	s.detail = formatRemaining(breakDuration)
	s.breakEnd = now.Add(breakDuration)
	s.mu.Unlock()

	s.editor.Reset()
	text := "screen break: look away from the screen for two minutes"
	s.log.Append(Row{
		Kind:  KindNotice,
		Level: 0,
		Text:  text,
		Spans: []Span{{Start: 0, End: len("screen break"), Role: RoleEmphasis}},
	})
}

// TickBreak advances a running break's countdown, or ends it once breakEnd
// has passed.
//
// It is driven by the repaint tick rather than by a timer of its own, since
// the frame already wakes on an interval and a second clock could disagree
// with the first about when the break is over.
func (s *Session) TickBreak(now time.Time) {
	s.mu.Lock()
	if s.state != StateBreak {
		s.mu.Unlock()
		return
	}

	remaining := s.breakEnd.Sub(now)
	if remaining > 0 {
		s.detail = formatRemaining(remaining)
		s.mu.Unlock()
		return
	}

	s.state = StateIdle
	s.detail = ""
	s.lastBreak = now
	s.mu.Unlock()

	s.log.Append(Row{
		Kind:  KindNotice,
		Level: 0,
		Text:  "screen break ended",
		Spans: []Span{{Start: 0, End: len("screen break ended"), Role: RoleDim}},
	})
}

// formatRemaining renders a duration as the countdown the state field shows,
// rounded to the nearest second so the figure does not flicker between two
// seconds a tick apart.
func formatRemaining(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Round(time.Second) / time.Second)
	return fmt.Sprintf("%dm%02ds remaining", total/60, total%60)
}

// RowsAt returns the rows at a level, which is what `/copy N` copies.
//
// It is the level and not the row number, since a fold changes how many rows there are
// and a reader who counted them would be counting something that moves. A level names
// one exchange and covers every row of it, tool lines included, which is what copying
// a responder's whole answer means.
//
// A retired level still answers, and says so. The reader gets their rows and learns
// the level is closed, rather than being told there is nothing there and left to
// guess whether they mistyped the number.
func (s *Session) RowsAt(level int) []Row {
	var out []Row
	for _, row := range s.log.Rows() {
		if row.Level == level {
			out = append(out, row)
		}
	}
	return out
}

// CopyLevel returns what `/copy N` copies, and reports a closed level rather than
// refusing to answer.
//
// The distinction is the whole reason a retired level keeps a tombstone: a reader
// who typed a wrong number and a reader who typed a stale one are two different
// situations, and an answer that treats them the same makes the handle untrustworthy.
func (s *Session) CopyLevel(n int) ([]Row, error) {
	level, known := s.levels.lookup(n)
	if !known {
		return nil, ErrNoLevel
	}

	rows := s.RowsAt(n)
	if level.Closed {
		return rows, ErrLevelClosed
	}
	return rows, nil
}

// OpenLevel hands out a level for a thread branching from parent.
//
// The number is allocated here rather than when the thread is asked, so a level is
// spent even if the reader walks away from the thread before asking it anything.
// A number that could be reclaimed is one that could be handed out twice, and a
// handle pointing at the wrong exchange is worse than a gap in the numbering a
// reader can see.
func (s *Session) OpenLevel(parent int, title string) (Level, error) {
	return s.levels.open(parent, title)
}

// CloseLevel ends a level and keeps what it was.
//
// This is what a thread does to itself when it has finished, and what a reader
// invokes as `/close`. The two are the same operation because they answer the same
// question, which is whether this exchange is still a thing `/copy` can reach.
func (s *Session) CloseLevel(n int) error {
	return s.levels.retire(n)
}

// Level reports a level and whether it was handed out.
func (s *Session) Level(n int) (Level, bool) {
	return s.levels.lookup(n)
}

// Levels returns every level handed out, ordered, closed ones included.
//
// A list that silently dropped retired entries is how a reader concludes they
// mistyped, so the bar shows every number that was ever given out.
func (s *Session) Levels() []Level { return s.levels.all() }

// OpenLevels returns the levels a new thread may branch from.
func (s *Session) OpenLevels() []Level { return s.levels.openLevels() }
