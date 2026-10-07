// Package tui draws the interface.
//
// Nothing here exists yet. This file holds the log, which is the part of the
// frame every other decision hangs off: a downward-scrolling sequence of rows
// written into the normal screen buffer, the way `brew` reports a run. The header,
// the pane and the fixed input block are gone, so the log is what the reader
// reads, what `/copy` copies, and what `/search` searches.
//
// # Why a log rather than a frame
//
// The frame that preceded this one was redrawn whole at every repaint, which is
// what a fixed layout needs and the wrong model for output that arrives over
// minutes. A model answering a question one tool call at a time produces rows that
// belong in the order they happened, and a reader who scrolls back to compare two
// tool results is reading history rather than a viewport.
//
// The cost of a log is that the reader's own scrollback is where the transcript
// lives, so the client must stop taking it: the alternate screen is given up, and
// what was on the screen before the client started comes back when it stops.
//
// # What this file is not
//
// It is not the whole package. The palette, the line editor, the terminal control,
// the session and the command table are separate concerns with their own
// decisions, and the design record at DESIGN-NOTES.md describes where each of them
// is headed.
package tui

import (
	"strings"
	"sync"
)

// Log is the record of what has been written, one row at a time.
//
// Rows are held whole and folded when they are drawn, never split as they arrive.
// The reason is the same one the reply rows kept: a fenced code block spans lines,
// so folding at arrival time leaves each fence marker on its own row and the fold
// then breaks code that must not be broken.
//
// The lock is held for the length of one method and never across a draw or an
// event. A turn goroutine appends while the painter reads, and a reader who is
// scrollback rather than a viewport means the two are concurrent by design rather
// than by accident.
type Log struct {
	mu sync.RWMutex

	rows []Row

	// folded counts how many rows have been dropped from the front because the
	// log outgrew what it holds. It is reported rather than hidden: a reader
	// told that rows went missing can scroll their own scrollback for them, and a
	// reader not told is looking at a transcript with holes in it.
	folded int
}

// Row is one line of the log, with the spans that colour it.
//
// A row carries plain text and its styling beside it, never styling inside the
// text. The rule is that a row can be measured, folded, searched and copied without
// knowing anything about colour, and that a model cannot end the frame by writing
// an escape into a reply.
type Row struct {
	// Text is the row as it will appear on the terminal, with the bytes a
	// terminal would act on removed. A reply is written by a model and a model
	// passes on whatever it was given, so a row can carry an escape that would
	// move the cursor or retitle the window if it were written out as received.
	Text string

	// Spans are byte ranges into Text, each naming what the text is rather than
	// what colour it is. Several may cover one row, and they are trimmed and cut
	// with the row rather than being recomputed.
	Spans []Span

	// Level is the thread the row belongs to, shown to the reader as the number
	// in the gutter and used by `/copy N`.
	//
	// It is attached when the row is written rather than looked up from pane
	// state, because a level that moved while a turn ran would re-attribute a row
	// the reader has already read to a different responder.
	Level int

	// Kind is what produced the row, which decides its marker. It is a value and
	// never a string carrying a prefix, since a model can write `[shell]` at the
	// start of a sentence as easily as the client can, and a guess from the text
	// would colour a sentence the model chose that way.
	Kind RowKind
}

// RowKind labels what a row is, for the marker and the role beside it.
type RowKind int

const (
	// KindReply is model text.
	KindReply RowKind = iota
	// KindQuestion is something the reader asked, echoed into the log.
	KindQuestion
	// KindTool is a tool call and its outcome.
	KindTool
	// KindQueued is a prompt accepted while a turn was working.
	KindQueued
	// KindNotice is a message from the client: a refusal, a failure, a milestone.
	KindNotice
	// KindChrome is the twiddle row and anything else the client draws for itself.
	KindChrome
)

// Span is a byte range over a row, naming a role rather than a colour.
//
// The role is resolved to a colour at draw time, so a row means the same thing
// whether colour is on or off and changing a theme costs no repaint of the log.
type Span struct {
	// Start and End are byte offsets into the row text. End is exclusive, which
	// is what makes an empty span expressible and therefore unnecessary.
	Start int
	End   int

	// Role is what the text is: a heading, a piece of emphasis, a tool identity.
	Role Role
}

// Role names what a piece of text is rather than what colour it is.
//
// From DESIGN-NOTES.md. The tool identities are separate roles rather than one
// role with three shades, because a reader who cannot tell a file read from a
// command cannot read the log, and that property is tested rather than assumed.
type Role int

const (
	// RoleChrome is the frame around the text: rules, bars, markers.
	RoleChrome Role = iota
	// RoleNotice is a message the client wrote.
	RoleNotice
	// RoleFailure is a failure.
	RoleFailure
	// RoleSuccess is something that completed.
	RoleSuccess
	// RoleApproval is a question asking the reader to do something.
	RoleApproval
	// RoleDim is text that is present but not the point.
	RoleDim
	// RoleCode is a fenced or inline code run.
	RoleCode
	// RoleHeading is a heading line.
	RoleHeading
	// RoleEmphasis is bold or italic, which the plain text does not keep between.
	RoleEmphasis
	// RoleQuote is a quoted line.
	RoleQuote
	// RoleList is a list marker.
	RoleList
	// RoleLink is a link target.
	RoleLink
	// RoleToolFS is a filesystem call.
	RoleToolFS
	// RoleToolGit is a git call.
	RoleToolGit
	// RoleToolShell is a shell call.
	RoleToolShell
)

// LogBound is how many rows a log keeps.
//
// It is a bound and not a limit on the reader: a session left running for a day
// should not hold every tool call it ever made, and the rows it drops are in the
// terminal's own scrollback, which is where a reader goes looking for them. The
// figure is generous enough that an ordinary session never reaches it.
const LogBound = 20000

// Append adds a row and returns its index.
//
// The index refers to the row's position among the rows held now, and trimming
// moves every index. A caller that needs to refer to a row across a long turn
// holds the row by value rather than the index, which is what the level does for
// the reader.
func (l *Log) Append(row Row) int {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.rows = append(l.rows, row)

	if over := len(l.rows) - LogBound; over > 0 {
		// Drop whole rows from the front rather than cutting one in half: a row
		// cut at the top of the log is a row whose beginning is unreadable, and
		// the count says how many were lost so a reader is not left guessing.
		l.rows = append(l.rows[:0], l.rows[over:]...)
		l.folded += over
	}
	return len(l.rows) - 1
}

// Rows returns the rows held, oldest first.
//
// The slice is copied so the caller can read it while a turn appends without
// racing. The rows themselves are not copied, which is safe because a row is
// written once and never rewritten: an append that trims moves the slice header
// but does not touch a row that is still in it.
func (l *Log) Rows() []Row {
	l.mu.RLock()
	defer l.mu.RUnlock()

	out := make([]Row, len(l.rows))
	copy(out, l.rows)
	return out
}

// Len reports how many rows the log holds.
func (l *Log) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.rows)
}

// Folded reports how many rows have been dropped from the front.
func (l *Log) Folded() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.folded
}

// Truncate empties the log, keeping the count of what it held.
//
// `/clear` clears the visible transcript without pretending the session did
// nothing: the counter is what a reader would ask about, and a command that
// reported nothing about how much it removed would look like a quiet failure.
func (l *Log) Truncate() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.folded += len(l.rows)
	l.rows = nil
}

// plainRow removes what a terminal would act on, keeping the tab.
//
// The whole sequence is removed, not only the control byte. Dropping the ESC and
// keeping the rest is worse than keeping the sequence: a row carrying a
// clear-screen arrives at the reader as the letters `2J`, which is visible nonsense
// rather than nothing at all.
//
// A tab is kept, since it is a column of space rather than a sequence and
// dropping it would fold a table into a wall. The box-drawing and braille figures
// the interface draws with are kept, since they are text and a reader selecting a
// row out of the log gets the figure rather than a question mark.
// The Unicode replacement character is kept too: it is valid text, and removing
// it can change JSON or Markdown that the model returned.
func plainRow(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]

		if r == 0x1b {
			// A sequence reaches past the escape by however many runes it covers,
			// which is the count escapeLength reports rather than the index it
			// finds the terminator at.
			i += escapeLength(runes[i+1:])
			continue
		}

		switch {
		case r == '\t':
			b.WriteRune(r)
		case r < 0x20, r == 0x7f:
		case r >= 0x80 && r <= 0x9f:
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// escapeLength reports how many runes past the escape belong to its sequence.
//
// Three shapes cover what a terminal acts on. CSI runs until a final byte in the
// range 0x40 to 0x7e; the parameter and intermediate bytes before it are part of
// the same sequence and are counted too, so the bracket is the first rune of the
// count and the final byte the last. A string sequence, OSC among them, runs
// until BEL or the string terminator, and is how a clipboard write and a title
// change are spelled. Everything else after an escape is a two-byte sequence, or
// is the escape acting alone.
//
// A sequence with no terminator takes the rest of the row rather than the whole
// log, since one row the endpoint wrote badly should cost the reader that row and
// not the transcript.
func escapeLength(rest []rune) int {
	if len(rest) == 0 {
		return 0
	}

	switch rest[0] {
	case '[': // CSI, ending at a final byte in 0x40 to 0x7e.
		for i := 1; i < len(rest); i++ {
			if rest[i] >= 0x40 && rest[i] <= 0x7e {
				return i + 1
			}
		}
		return len(rest)
	case ']', 'P', 'X', '^', '_': // String sequences, ending at BEL or ST.
		for i := 1; i < len(rest); i++ {
			if rest[i] == 0x07 {
				return i + 1
			}
			if rest[i] == 0x1b && i+1 < len(rest) && rest[i+1] == '\\' {
				return i + 2
			}
		}
		return len(rest)
	default:
		// A two-byte sequence, or an escape acting alone.
		return 1
	}
}

// PlainRow returns a row with what a terminal would act on removed.
//
// It is exported because the copy path needs it as much as the draw path does, and
// a copy that carried escapes would paste a cursor movement into whatever the
// reader pasted it into.
func PlainRow(row Row) Row {
	row.Text = plainRow(row.Text)
	return row
}
