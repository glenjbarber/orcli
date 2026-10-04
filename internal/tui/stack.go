package tui

import (
	"strings"
)

// The footer stack, drawn at the bottom of the screen with the prompt as the last
// row and the log filling everything above it.
//
// The order is the one the design fixed, from the bottom of the screen upward: the
// prompt, the key hints, the Provider bar, the Credits bar, each separated by a rule.
// The bars keep the field order DESIGN.md 4.4 gives them, since that order was
// decided with a reason behind it and moving the bars to the bottom does not move the
// reasoning with them.
//
// The hint row is drawn empty. Its contents are undecided and the spec says not to
// invent entries, so the row is present and blank: a stack with a gap in it is
// honest about the gap, and a stack with invented key names in it is a set of wrong
// things to press.

// rule is the horizontal rule between the stack's rows.
//
// A box-drawing figure rather than a hyphen, since it is one column rather than one
// and a hyphen at the width of a rule leaves a visible notch every other character
// row.
const rule = "─"

// Prompt is what the reader's line is drawn behind.
const Prompt = "root@localhost $ "

// escapeMoveUp moves the cursor up n rows.
const escapeMoveUp = "\x1b[1A"

// escapeEraseLine clears the row the cursor is on.
//
// The row is cleared before it is written rather than the figure being overwritten,
// since two braille cells do not cover a whole row and a figure drawn over the last
// one leaves a smear rather than a turn.
const escapeEraseLine = "\x1b[2K"

// stackRows is how many rows the stack occupies, smallest first.
//
// The smallest is what a short terminal is left with: the prompt and nothing else,
// since a reader who cannot type cannot use the interface and everything above it is a
// luxury. The twiddle goes first, then the hint row, then the bars.
func stackRows(height int) int {
	switch {
	case height >= 9:
		return 9
	case height >= 6:
		return 6
	default:
		return 1
	}
}

// logRows is how many rows are left for the log above a stack.
//
// It is never less than one, so the scroll region has something to scroll and a
// terminal with no room shows a log rather than nothing.
func logRows(height int) int {
	if n := height - stackRows(height); n > 1 {
		return n
	}
	return 1
}

// Bar is what the two status bars carry.
//
// It is a value rather than something read off the session, so the drawing can be
// tested with figures a test chose rather than with figures a session happened to
// have.
type Bar struct {
	// Provider is the Provider bar: Provider, Model, Status, Approval, in that
	// order and with no other field.
	Provider string

	// Credits is the Credits bar: Credits, Cost, Context, In, Out, Host. It is
	// empty in this unit, since nothing reports those figures yet.
	Credits string
}

// DrawLog writes rows downward into the normal screen buffer.
//
// It is the only thing that appends to the screen, and it appends: nothing here
// redraws a row already written. The one exception is the twiddle, drawn by
// DrawTwiddle on its own row.
//
// Rows are cut to the width before they are written, so what reaches the terminal is
// what a copy of the same row produces. A row allowed to wrap would leave the
// terminal holding more lines than the log has rows, and the log and the screen would
// stop agreeing about what was said.
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
// several: a tool line is a tool colour over its own words. A row with no spans is
// written plainly, which is the common case and the one that must cost the least.
func foldRow(p Palette, row Row, width int) string {
	text := CutColumnFromEnd(row.Text, width)
	if !p.On() || len(row.Spans) == 0 {
		return text
	}

	var b strings.Builder
	b.WriteString(p.Base())

	offset := 0
	for _, span := range row.Spans {
		start, end := spanBounds(span, len(text))
		if end <= offset {
			// Spans arrive in whatever order the writer produced them and an
			// out-of-order one would write its own text twice. Skipping it is better
			// than a row that says the same words in the wrong colours.
			continue
		}
		b.WriteString(p.Sequence(span.Role))
		b.WriteString(text[offset:start])
		offset = end
	}
	b.WriteString(text[offset:])

	// The reset carries the theme's base back, so a row ending mid-span leaves the
	// next row on the theme's background rather than on whatever the last colour was.
	b.WriteString(p.Reset())
	return b.String()
}

// spanBounds clamps one span to the text as written and as cut.
//
// A span is a byte range over the whole row, and a row cut from the tail has fewer
// bytes than it did, so a span past the cut ends at the cut rather than running past
// the end of what is written. A span beginning past the cut is dropped, which is the
// same as being empty.
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

// DrawStack writes the footer stack, from the prompt up.
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
	lines := stackLines(bar)

	// A terminal too short for the whole stack keeps the bottom of it, since the
	// prompt is the bottom row and a reader who cannot see it cannot type.
	if rows < len(lines) {
		lines = lines[len(lines)-rows:]
	}

	for _, line := range lines {
		w.Write(chromeLine(palette, line) + "\r\n")
	}
}

// stackLines renders the stack as rows, bottom of the screen last.
//
// It is a function rather than a value so the caller asks for the stack when it is
// drawing rather than holding a value that goes stale when a figure changes.
func stackLines(bar Bar) []string {
	return []string{
		chromeLine(Palette{}, bar.Credits),
		rule,
		chromeLine(Palette{}, bar.Provider),
		rule,
		"",
		Prompt,
	}
}

// chromeLine wraps a stack row in the chrome role when colour is on.
//
// It takes a palette rather than reading one so the stack is testable with colour
// off, and so a row with no colour in it is the same row of text a reader would get
// with colour on and a terminal that ignores it.
func chromeLine(p Palette, line string) string {
	if !p.On() || line == "" {
		return line
	}
	return p.Sequence(RoleChrome) + line + p.Reset()
}

// DrawTwiddle writes the twiddle row in place, above the prompt.
//
// It is the only thing in the frame that redraws rather than appends, so it moves the
// cursor up to its own row and writes there. Every other row is written once and left
// alone, which is what makes the reader's own scrollback the transcript.
//
// The step rather than the time is the caller's, since the session owns when a frame is
// due and a function that read the clock itself would be a second thing that has to
// agree with the repaint rate.
func DrawTwiddle(w *Screen, palette Palette, step int) {
	w.Write(escapeMoveUp + escapeEraseLine)
	w.Write(TwiddleHue(step) + Twiddle(step) + palette.Reset())
}

// Field is one bar field, a name and a figure.
type Field struct {
	// Name is what the reader reads.
	Name string

	// Value is the figure, empty where there is none.
	Value string
}

// RenderBar renders a bar from its fields, dropping whole fields when it will not fit
// and cutting the context share rather than dropping it.
//
// A bar too narrow for its fields drops them from the right, which is the order the
// fields were given in, and that order is how much a reader loses by losing each. The
// context share is the exception and is cut, since it is the figure that says a
// compaction is coming and a reader who cannot see it does not know to expect one.
//
// Width is the terminal's, and a bar with no width given is not cut at all, since a
// caller that has not asked the terminal yet is a caller that would rather see the
// whole bar than a truncated one.
func RenderBar(fields []Field, width int) string {
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		parts = append(parts, f.Name+": "+orNone(f.Value))
	}
	return joinWithin(parts, " | ", width)
}

// joinWithin joins fields, dropping whole ones until the rest fit.
//
// Whole rather than cut, since a bar reading `Cred | Con | Hos` tells a reader less
// than one reading `Credits | Context | Host`, and a cut one looks like a value the
// reader mistyped.
func joinWithin(parts []string, sep string, width int) string {
	if width <= 0 {
		return strings.Join(parts, sep)
	}
	for len(parts) > 1 && DisplayWidth(strings.Join(parts, sep)) > width {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, sep)
}

// RenderProvider renders the Provider bar from the figures a session reports.
//
// The field order is the one DESIGN.md 4.4 fixes, and Status carries the state
// rather than having a field of its own, since a fifth field is what put paused in
// Status in the first place.
func RenderProvider(provider, model string, state State, detail, approval string) string {
	return RenderBar([]Field{
		{Name: "Provider", Value: provider},
		{Name: "Model", Value: model},
		{Name: "Status", Value: string(state) + detailSuffix(detail)},
		{Name: "Approval", Value: approval},
	}, 0)
}

// RenderCredits renders the Credits bar from the figures a session reports.
//
// The context share is the one field that is cut rather than dropped, and RenderBar
// does not know which field is which, so the caller passes the figures in order and
// this is where the order is recorded.
func RenderCredits(credits, cost, contextUsed, in, out, host string) string {
	return RenderBar([]Field{
		{Name: "Credits", Value: credits},
		{Name: "Cost", Value: cost},
		{Name: "Context", Value: contextUsed},
		{Name: "In", Value: in},
		{Name: "Out", Value: out},
		{Name: "Host", Value: host},
	}, 0)
}

// detailSuffix renders the note beside a state.
//
// It is a parenthesised note rather than a fifth field, since a bar that grows a comma
// is a bar no reader can scan, and a state spelled `paused, buffered 12` is a state
// rather than a state and a note.
func detailSuffix(detail string) string {
	if detail == "" {
		return ""
	}
	return " (" + detail + ")"
}

// orNone renders an empty string as a dash.
//
// A field with no value is a dash rather than nothing, so the bar holds its shape as
// values arrive and a reader scanning it is not reading a row whose columns move.
func orNone(s string) string {
	if s == "" {
		return "-"
	}
	return s
}