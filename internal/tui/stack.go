package tui

import (
	"strings"
)

// The footer stack, drawn at the bottom of the screen in nine fixed rows with the
// log filling everything above it.
//
// The order is the one the reader settled, from the bottom of the screen upward: a
// blank row, a separator, the bottom bar, the top bar, a separator, the field, the
// twiddle, the active row, and the task row. Everything above the twiddle row is
// the output area.
//
// The two bars are named by where they sit rather than by what they carry. The top
// bar is nearer the log and holds what changes second by second; the bottom bar is
// nearer the bottom of the screen and holds what is settled, along with the session
// and the two capture modes. Both drop whole entries from the right when they will
// not fit, so the order they are given in is the order of loss.
//
// The prompt is gone. There was a `root@localhost $ ` at the head of the field and it
// is not in this layout, so the field carries only what has been typed and both it
// and the twiddle sit at column five. A reader is typing at a known place on the
// bottom row of the screen and does not need to be told which account they are.
//
// All nine rows are drawn whether or not anything is running. A footer that grows
// when a turn starts is a footer that moves the field under the reader's hands, and
// the one thing this frame is for is that the reader's place in it does not move.

// rule is the horizontal rule between the stack's rows.
//
// A box-drawing figure rather than a hyphen, since it is one column rather than one, and
// a hyphen at the width of a rule leaves a visible notch every other character row.
const rule = "─"

// Prompt is gone.
//
// It is named only so a reader of this file can see that the layout is a change and
// not an omission, and so nothing else reaches for it.
const Prompt = ""

// FieldIndent is how far in the field and the twiddle sit.
//
// Five columns, so the figure and the text beside it have the same left edge and a
// reader's eye does not jump between rows to follow them.
const FieldIndent = 5

// escapeMoveUp moves the cursor up a row.
const escapeMoveUp = "\x1b[1A"

// escapeEraseLine clears the row the cursor is on.
//
// The row is cleared before it is written rather than the figure being overwritten, since
// two braille cells do not cover a whole row and a figure drawn over the last one leaves
// a smear rather than a turn.
const escapeEraseLine = "\x1b[2K"

// stackRows is how many rows the stack occupies, smallest first.
//
// The smallest is what a short terminal is left with: the field and nothing else, since a
// reader who cannot type cannot use the interface and everything above it is a luxury.
// Which of the nine rows are shed, and in what order, is not settled. What is settled
// is the count, and this returns it for a terminal tall enough to hold the whole thing.
func stackRows(height int) int {
	switch {
	case height >= 9:
		return 9
	default:
		return 1
	}
}

// logRows is how many rows are left for the log above a stack.
//
// It is never less than one, so the scroll region has something to scroll and a terminal
// with no room shows a log rather than nothing.
func logRows(height int) int {
	if n := height - stackRows(height); n > 1 {
		return n
	}
	return 1
}

// Bar is what the two status bars carry.
//
// It is a value rather than something read off the session, so the drawing can be tested
// with figures a test chose rather than with figures a session happened to have.
type Bar struct {
	// Top is the top bar, nearer the log: Status, Reasoning, Context, In, Out, Cost,
	// Credits.
	Top string

	// Bottom is the bottom bar, nearer the bottom of the screen: Session, [Mouse],
	// [Copy], Provider, Model, Approval, Verbosity.
	Bottom string

	// Field is the reader's own line, drawn at column five with no prompt in front of
	// it.
	Field string

	// Twiddle is the figure and its state word, empty when idle.
	Twiddle string

	// Active is the row reporting what this session is doing, and is drawn whether or
	// not that is idle.
	Active string

	// Tasks is the row reporting how many tasks are running and what the newest is
	// doing.
	Tasks string
}

// DrawLog writes rows downward into the normal screen buffer.
//
// It is the only thing that appends to the screen, and it appends: nothing here redraws
// a row already written. The one exception is the twiddle, drawn by DrawTwiddle on its
// own row.
//
// Rows are cut to the width before they are written, so what reaches the terminal is what
// a copy of the same row produces. A row allowed to wrap would leave the terminal holding
// more lines than the log has rows, and the log and the screen would stop agreeing about
// what was said.
func DrawLog(w *Screen, rows []Row, palette Palette) {
	if len(rows) == 0 {
		return
	}

	width := w.Width()
	for _, row := range rows {
		w.Write(foldRow(palette, PlainRow(row), width) + "\r\n")
	}
}

// foldRow renders one row, cut to the width.
//
// The palette is asked for a role per span rather than per row, since a row may carry
// several: a tool line is a tool colour over its own words. A row with no spans is written
// plainly, which is the common case and the one that must cost the least.
//
// The walk is in one piece: every byte of the row is written exactly once, either inside a
// span or outside one. Writing the text before each span and then jumping past the span
// loses the span's own text, so a row whose spans cover its middle arrives with a hole in
// it, which is worse than a row with no colour at all.
func foldRow(p Palette, row Row, width int) string {
	text := CutColumnFromEnd(row.Text, width)
	if !p.On() || len(row.Spans) == 0 {
		return text
	}

	var b strings.Builder
	b.WriteString(p.Base())

	written := 0
	for _, span := range row.Spans {
		start, end := spanBounds(span, len(text))
		if end <= written {
			// Spans arrive in whatever order the writer produced them, and an
			// out-of-order one would write its own text twice. Skipping it is better
			// than a row that says the same words in the wrong colours.
			continue
		}

		b.WriteString(text[written:start])
		b.WriteString(p.Sequence(span.Role))
		b.WriteString(text[start:end])
		written = end
	}
	b.WriteString(text[written:])

	// The reset carries the theme's base back, so a row ending mid-span leaves the next
	// row on the theme's background rather than on whatever the last colour was. It is
	// written after the whole row rather than inside the loop, since a reset before the
	// tail would leave the tail on the wrong background.
	b.WriteString(p.Reset())
	return b.String()
}

// spanBounds clamps one span to the text as written and as cut.
//
// A span is a byte range over the whole row, and a row cut from the tail has fewer bytes
// than it did, so a span past the cut ends at the cut rather than running past the end of
// what is written. A span beginning past the cut is dropped, which is the same as being
// empty.
func spanBounds(span Span, length int) (int, int) {
	if span.Start < 0 || span.Start > length {
		return 0, 0
	}
	end := span.End
	if end > length {
		end = length
	}
	if end <= span.Start {
		return span.Start, span.Start
	}
	return span.Start, end
}

// DrawStack writes the footer stack, from the blank row at the bottom upward.
//
// The stack is drawn whole every time rather than its parts separately, since it is a
// fixed number of rows and rewriting all of it is cheaper than tracking which field
// changed. The log above it is never touched, which is what lets a reader scroll back
// through it.
func DrawStack(w *Screen, bar Bar, palette Palette) {
	height := w.Height()
	if height < 1 {
		return
	}

	rows := stackRows(height)
	lines := stackLines(bar, palette)

	// A terminal too short for the whole stack keeps the bottom of it, since the field
	// is near the bottom and a reader who cannot see it cannot type.
	if rows < len(lines) {
		lines = lines[:rows]
	}

	for _, line := range lines {
		w.Write(line + "\r\n")
	}
}

// stackLines renders the stack as rows, bottom of the screen last.
//
// stackLines renders the stack as rows, in screen order.
//
// The first row returned is the top of the stack and the last is the bottom of the
// screen, because DrawStack writes downward and the terminal puts the first line it
// receives where the cursor is. The blank row is therefore last, since it is the bottom
// row of the screen.
//
// The approved render reads, from the bottom of the screen upward: a blank row, a
// separator, the bottom bar, the top bar, a separator, the field, the twiddle, the active
// row, and the task row. Writing that order as written would put the blank row at the top
// and the task row at the bottom, which is the frame upside down.
func stackLines(bar Bar, palette Palette) []string {
	return []string{
		chromeLine(palette, indent(bar.Tasks)),
		chromeLine(palette, indent(bar.Active)),
		chromeLine(palette, indent(bar.Twiddle)),
		chromeLine(palette, indent(bar.Field)),
		rule,
		chromeLine(palette, bar.Top),
		chromeLine(palette, bar.Bottom),
		rule,
		"",
	}
}

// chromeLine wraps a stack row in the chrome role when colour is on.
//
// It takes a palette rather than reading one so the stack is testable with colour off,
// and so a row with no colour in it is the same row of text a reader would get with
// colour on and a terminal that ignores it.
func chromeLine(p Palette, line string) string {
	if !p.On() || line == "" {
		return line
	}
	return p.Sequence(RoleChrome) + line + p.Reset()
}

// DrawTwiddle writes the twiddle row in place.
//
// It is the only thing in the frame that redraws rather than appends, so it moves the
// cursor up one row to the row directly above the field, which is where the twiddle row
// is in this layout, and writes there. Every other row is written once and left alone,
// which is what makes the reader's own scrollback the transcript.
//
// The step rather than the time is the caller's, since the session owns when a frame is
// due and a function that read the clock itself would be a second thing that has to agree
// with the repaint rate.
//
// It is not called at all when idle, since the row is blank then and a figure that is
// not moving is a figure a reader has to learn to ignore.
func DrawTwiddle(w *Screen, palette Palette, step int, state string) {
	if state == "" {
		return
	}
	w.Write(escapeMoveUp + escapeEraseLine)
	w.Write(indent(TwiddleHue(step)+Twiddle(step)+"  "+state) + palette.Reset())
}

// Field is one bar field, a name and a figure.
type Field struct {
	// Name is what the reader reads.
	Name string

	// Value is the figure, empty where there is none.
	Value string
}

// RenderBar renders a bar from its fields, dropping whole fields when it will not fit.
//
// A bar too narrow for its fields drops them from the right, which is the order the
// fields were given in, and that order is how much a reader loses by losing each.
//
// Width is the terminal's, and a bar with no width given is not cut at all, since a caller
// that has not asked the terminal yet is a caller that would rather see the whole bar
// than a truncated one.
func RenderBar(fields []Field, width int) string {
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		parts = append(parts, f.Name+": "+orNone(f.Value))
	}
	return joinWithin(parts, " | ", width)
}

// joinWithin joins fields, dropping whole ones until the rest fit.
//
// Whole rather than cut, since a bar reading `Cred | Con | Hos` tells a reader less than
// one reading `Credits | Context | Host`, and a cut one looks like a value the reader
// mistyped.
func joinWithin(parts []string, sep string, width int) string {
	if width <= 0 {
		return strings.Join(parts, sep)
	}
	for len(parts) > 1 && DisplayWidth(strings.Join(parts, sep)) > width {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, sep)
}
