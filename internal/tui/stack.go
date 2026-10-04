package tui

import "strings"

// The footer stack, drawn at the bottom of the screen in seven fixed rows with the
// log filling everything above it.
//
// The order is the one the reader settled, from the bottom of the screen upward: a
// blank row, a separator, the bottom bar, the top bar, a separator, the prompt, and
// the twiddle row. Everything above the twiddle row is the output area.
//
// The two bars are named by where they sit rather than by what they carry. The top
// bar is nearer the log and holds what changes second by second; the bottom bar is
// nearer the bottom of the screen and holds what is settled. Both drop whole fields
// from the right when they will not fit, so the field order is the order of loss: the
// fields on the top bar that survive longest are the ones a reader watches while a
// turn runs.
//
// There is no hint row. The merged frame carried one, drawn blank with a comment
// saying its contents were undecided, and this layout has no row for it. A row with a
// comment about a gap in it is worse than no row.

// rule is the horizontal rule between the stack's rows.
//
// A box-drawing figure rather than a hyphen, since it is one column rather than one, and
// a hyphen at the width of a rule leaves a visible notch every other character row.
const rule = "─"

// Prompt is what the reader's line is drawn behind.
const Prompt = "root@localhost $ "

// escapeMoveUp moves the cursor up a row.
//
// It is a bare move with no erase of the rows it passed over, since the row it lands on
// is cleared by whatever writes next: the twiddle clears its own, and a paint rewrites
// the stack whole.
const escapeMoveUp = "\x1b[1A"

// escapeEraseLine clears the row the cursor is on.
//
// The row is cleared before it is written rather than the figure being overwritten, since
// two braille cells do not cover a whole row and a figure drawn over the last one leaves
// a smear rather than a turn.
const escapeEraseLine = "\x1b[2K"

// stackHeight is how many rows the stack occupies on a terminal tall enough.
//
// The seven come from the layout the reader settled, from the bottom of the screen
// upward: a blank row, a separator, the bottom bar, the top bar, a separator, the
// prompt, and the twiddle row. Everything above the twiddle row is the log.
//
// It is a named constant rather than a figure at each use because the scroll region,
// the short-terminal behaviour and the painter all have to agree about it, and three
// places that each count the rows is three places that can disagree.
const stackHeight = 7

// stackRows is how many rows the stack occupies.
//
// It is as many of the seven as the terminal has room for, and not a ladder. A ladder
// decides which rows are shed in which order, and that order is not settled, so
// returning one row for a short terminal would be claiming a decision this does not
// have: a three row terminal would be left with the bottom row, which is the blank one
// at the bottom of the screen, and a reader who cannot see the prompt cannot type.
//
// What is settled is the count at full height and that the prompt survives, and this
// returns exactly that much. The ladder is the unit that decides the rest.
func stackRows(height int) int {
	switch {
	case height >= stackHeight:
		return stackHeight
	case height < 1:
		return 1
	default:
		return height
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

	// Bottom is the bottom bar, nearer the bottom of the screen: Provider, Model,
	// Verbosity, Approval.
	Bottom string

	// Field is the prompt row's text, drawn behind the editor's own field rather
	// than beside it, since the editor holds what the reader typed and not the prompt.
	Field string
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
//
// The prompt row carries the editor's field rather than the bare prompt, so the painter
// and the editor are not two things writing one row.
func DrawStack(w *Screen, bar Bar, palette Palette) {
	height := w.Height()
	if height < 1 {
		return
	}

	rows := stackRows(height)
	lines := stackLines(bar, palette)

	// A terminal too short for the whole stack keeps the bottom of it, since the prompt
	// is near the bottom and a reader who cannot see it cannot type.
	if rows < len(lines) {
		lines = lines[len(lines)-rows:]
	}

	for _, line := range lines {
		w.Write(line + "\r\n")
	}
}

// stackLines renders the stack as rows, bottom of the screen last.
//
// It is a function rather than a value so the caller asks for the stack when it is
// drawing rather than holding a value that goes stale when a figure changes.
func stackLines(bar Bar, palette Palette) []string {
	return []string{
		"",
		rule,
		chromeLine(palette, bar.Bottom),
		chromeLine(palette, bar.Top),
		rule,
		chromeLine(palette, bar.Field),
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
// cursor up one row to the row directly above the prompt, which is where the twiddle
// row is in this layout, and writes there. Every other row is written once and left
// alone, which is what makes the reader's own scrollback the transcript.
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
	w.Write(TwiddleHue(step) + Twiddle(step) + "  " + state + palette.Reset())
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

// RenderTop renders the top bar, the one nearer the log.
//
// The field order is the order of loss, and it is the reverse of what the merged frame
// did with Status: the fields that change second by second are the ones that survive a
// narrow bar longest, and Status is last to go for exactly that reason. Context, In,
// Out, Cost and Credits are carried by nothing yet, so they render as a dash until a
// caller has a figure for them.
func RenderTop(status, reasoning, contextUsed, in, out, cost, credits string) string {
	return RenderBar([]Field{
		{Name: "Status", Value: status},
		{Name: "Reasoning", Value: reasoning},
		{Name: "Context", Value: contextUsed},
		{Name: "In", Value: in},
		{Name: "Out", Value: out},
		{Name: "Cost", Value: cost},
		{Name: "Credits", Value: credits},
	}, 0)
}

// RenderBottom renders the bottom bar, the one nearer the bottom of the screen.
//
// The order of loss is Verbosity, Approval, Model, Provider. Approval is ahead of
// Verbosity for the reason it was moved there: it says whether programs run without
// asking, and it is not the first thing a narrow bar should lose.
func RenderBottom(provider, model, verbosity, approval string) string {
	return RenderBar([]Field{
		{Name: "Provider", Value: provider},
		{Name: "Model", Value: model},
		{Name: "Verbosity", Value: verbosity},
		{Name: "Approval", Value: approval},
	}, 0)
}

// RenderProvider renders the bottom bar from the figures a session reports.
//
// It is kept as a name a caller already uses, and it renders the bottom bar rather than
// the bar the merged frame called Provider: the bar nearest the bottom of the screen is
// the one that names the provider, and the field order on it is the one the reader
// settled.
func RenderProvider(provider, model, verbosity, approval string) string {
	return RenderBottom(provider, model, verbosity, approval)
}

// RenderStatus renders the top bar from the figures a session reports.
//
// The state carries the detail as a note beside it rather than as a fifth field, since
// a bar that grows a comma is a bar no reader can scan.
func RenderStatus(status, reasoning, contextUsed, in, out, cost, credits string) string {
	return RenderTop(status, reasoning, contextUsed, in, out, cost, credits)
}

// detailSuffix renders the note beside a state.
//
// It is a parenthesised note rather than a fifth field, since a bar that grows a comma is
// a bar no reader can scan, and a state spelled `paused, buffered 12` is a state rather
// than a state and a note.
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
