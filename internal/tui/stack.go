package tui

import (
	"strings"
)

// The footer stack, drawn at the bottom of the screen with the log filling
// everything above it.
//
// The order is the one the reader settled, from the bottom of the screen upward: the
// two status bars, a blank row, and the prompt row. The prompt row carries either the
// reader's own line or, while a turn is running, the figure and the state word in place
// of the `root@localhost $ ` text.
//
// Everything from the prompt row down is what orcli owns. Above it belongs to the
// terminal: this package writes rows there and never moves the viewport, so the reader's
// own scrollback is the transcript and nothing here has to manage it.
//
// The rule spans the full terminal width rather than sitting at column one. A rule at the
// width of the terminal reads as a divider, and a single figure at column one reads as a
// left border, and they are two different frames. The cost is that a resize has to
// redraw it, which is why FillBar asks the caller for the width rather than reading one
// itself.
//
// There is no rule at the very bottom. A footer that ends in a rule is a closed box, and
// this one sits against the bottom edge of the terminal where the shell prompt will be
// when the program exits: a rule there would be the last thing on screen and would read
// as a border to something that is not bordered.

// rule is the horizontal rule above the prompt row.
//
// A box-drawing figure rather than a hyphen, since it is one column rather than one, and
// a hyphen at the width of a rule leaves a visible notch every other character row.
const rule = "─"

// Prompt is what the reader's line is drawn behind.
//
// It is dropped while a turn is running, in place of the figure, so that the row the
// reader is typing into keeps its position and the figure does not push the field
// upwards. The row is the same row either way: only the text in front of the reader's
// own line changes.
const Prompt = "root@localhost $ "

// FieldIndent is how far in the prompt row's own text sits behind the prompt.
//
// Zero, since the prompt is at column one and the caret follows the text the reader has
// typed rather than the prompt in front of it. It is named rather than deleted because
// the caret arithmetic in caret.go offsets by it, and a constant of zero there reads as
// "this was decided" rather than "this was forgotten".
const FieldIndent = 0

// TwiddleIndent is how far in the figure sits when the prompt row is carrying it.
//
// Two columns, so the figure and the word beside it have a left edge of their own and
// are not read as part of the bottom bar below.
const TwiddleIndent = 2

// The rows of the stack, as positions in the screen-order slice stackLines returns.
//
// They are named rather than counted from an end because the prompt row is the bottom
// row of the stack and every other row is above it, which is the one arrangement where a
// truncation from the end keeps what the reader is typing into.
const (
	rowRule = iota
	rowPrompt
	rowBlank
	rowTopBar
	rowBottomBar

	// fullStackRows is every row the stack draws. It is named once so the count the
	// stack renders and the count the tests assert cannot drift apart.
	fullStackRows
)

// stackRowsToKeep returns the positions of the rows a terminal of this height draws, in
// screen order.
//
// It is a keep-list rather than a truncation because the rows worth keeping on a short
// terminal are the prompt row and the blank under it, and the bars are shed first: they
// report what has already happened while the prompt is what the reader is about to do.
//
// On a terminal tall enough it is every row in order. Below that it is the floor. Which
// rows are shed on the way down, and in what order, is the reader's decision and is not
// settled; this is a floor rather than a ladder.
func stackRowsToKeep(height int) []int {
	if height >= fullStackRows {
		keep := make([]int, fullStackRows)
		for i := range keep {
			keep[i] = i
		}
		return keep
	}
	return []int{rowPrompt, rowBlank}
}

// Bar is what the footer carries.
//
// It is a value rather than something read off the session, so the drawing can be tested
// with figures a test chose rather than with figures a session happened to have.
type Bar struct {
	// Top is the bar nearer the log, holding what changes second by second.
	Top string

	// Bottom is the bar nearer the bottom of the screen, holding what is settled
	// along with the session and the two capture modes.
	Bottom string

	// Field is the reader's own line, drawn after the prompt on the prompt row. It
	// is empty while a turn is running, since the figure has the row then.
	Field string

	// Twiddle is the figure and the state word, indented two, and is empty when no
	// turn is running. When it is set the prompt is not drawn, so the row carries
	// one or the other and never both.
	Twiddle string
}

// DrawLog writes rows downward to the terminal, above the footer.
//
// It writes downward and never moves the viewport. Nothing here scrolls, clips or
// positions: the terminal owns the rows above the footer and the reader's own scrollback
// is the transcript. A row that would be wider than the terminal is cut rather than
// wrapped, since a wrapped row is two rows and the terminal and the log would then
// disagree about what was said.
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

// DrawStack writes the footer, from the lower bar up.
//
// The stack is written as terminal rows rather than into a pinned region, so it is the
// reader's decision how often it is redrawn: a caller that redraws it on every keystroke
// will advance the screen by the footer's height each time. Run and Start both redraw it
// only when one of its rows has changed.
func DrawStack(w *Screen, bar Bar, palette Palette) {
	if w.Height() < 1 {
		return
	}

	lines := stackLines(bar, palette, w.Width())
	for _, at := range stackRowsToKeep(w.Height()) {
		w.Write(lines[at] + "\r\n")
	}
}

// stackLines renders the stack as rows in screen order.
//
// The first row returned is the top of the stack and the last is the bottom of the
// screen, because DrawStack writes downward and the terminal puts the first line it
// receives where the cursor is. The rule is first and the bottom bar is last.
//
// The prompt row carries the prompt and the reader's own text when no turn is running,
// and the figure and the state word when one is. The row is the same row either way: what
// changes is the text in front of the reader's line, not the row's position, so a reader
// watching a turn start does not find their field has moved.
func stackLines(bar Bar, palette Palette, width int) []string {
	lines := make([]string, fullStackRows)
	lines[rowRule] = FillBar(rule, width)
	lines[rowPrompt] = chromeLine(palette, promptRow(bar))
	lines[rowBlank] = ""
	lines[rowTopBar] = chromeLine(palette, bar.Top)
	lines[rowBottomBar] = chromeLine(palette, bar.Bottom)
	return lines
}

// promptRow is what the prompt row carries: the figure while a turn runs, and the
// prompt and the reader's own text otherwise.
//
// The two are exclusive rather than both drawn, since a row carrying the figure and the
// prompt behind it is a row where the reader cannot tell where they are typing to end.
func promptRow(bar Bar) string {
	if bar.Twiddle != "" {
		return bar.Twiddle
	}
	return Prompt + bar.Field
}

// FillBar repeats a figure across the width of the terminal.
//
// It counts display columns rather than bytes, since the figure is three bytes to the
// column and a byte count would produce a rule a third of the width it should be. The
// remainder is padded with spaces rather than cut, so a rule is never a fraction of a
// figure long.
func FillBar(figure string, width int) string {
	if width < 1 || figure == "" {
		return ""
	}
	one := DisplayWidth(figure)
	if one < 1 {
		return ""
	}
	return strings.Repeat(figure, width/one) + strings.Repeat(" ", width%one)
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
