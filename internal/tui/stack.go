package tui

import (
	"math"
	"strconv"
	"strings"
)

// The footer stack, drawn at the top of the screen with the log filling everything below it.
// Six rows, from the top of the stack downward: the reserved row, the reserved row, the twiddle
// row, the bar carrying what changes second by second, the bar carrying the session and the
// provider, and the prompt. The reader named them statusbar1 through statusbar5 and the prompt,
// top to bottom, with the twiddle in statusbar3.
//
// The two leading rows carry placeholder text for now. They are drawn so the rows they will
// occupy are the footer's and not the log's, since a row that is written when there is something
// to put in it is a row that has to move everything below it.
//
// The stack is at the top of the screen, so the reader's own scrollback is above it and the log
// is below it. Those are three separate things and this package writes two of them: the shell's
// output from before the program started, the six rows, and the transcript.
//
// The prompt is the last of the six. It is the row the caret is placed on, and it is named by
// counting the rows rather than by a figure derived from the terminal's height, so a row that
// moved is a row every caller moved with it.

// Prompt is what the reader's line is drawn behind.
const Prompt = "root@localhost $ "

// ReservedRowText is what the two leading footer rows carry until they hold something.
//
// It is drawn rather than left blank so the rows are the footer's on a reader's screen and not
// the log's, since a row that is the footer's and is drawn as part of the log is a row that
// scrolls away. The text is a placeholder and reads as one, since a row whose contents a reader
// might act on is a row that will be believed.
//
// It is a constant rather than a string at the two call sites so the two rows cannot disagree
// about what they are standing in for.
const ReservedRowText = "[test text]"

// FieldIndent is how far in the prompt row's own text sits behind the prompt.
//
// Zero, since the prompt is at column one and the caret follows the text the reader has typed
// rather than the prompt in front of it. It is named rather than deleted because the caret
// arithmetic in caret.go offsets by it, and a constant of zero there reads as "this was decided"
// rather than "this was forgotten".
const FieldIndent = 0

// TwiddleIndent is how far in the figure sits on the twiddle row.
//
// Two columns, so the figure and the word beside it have a left edge of their own and are not
// read as part of the bar below.
const TwiddleIndent = 2

// statusPrefix is the text the changing bar's first field begins with.
//
// It is named because the colour sweep has to know where the Status value starts and ends inside
// a bar that is rendered as one string, and the prefix is the only thing in this file that says so.
const statusPrefix = "Status: "

// The rows of the footer, as positions in the screen-order slice stackLines returns.
//
// They are in screen order, so position zero is the topmost row. The prompt row is last, which is
// what makes it the last of the six.
const (
	// rowFirst and rowSecond are the two rows above the twiddle. They are named because the rows
	// they will hold are the footer's rows, and a row that belongs to the footer and is drawn as
	// part of the log is a row that scrolls.
	rowFirst = iota
	rowSecond
	rowTwiddle
	rowTopBar
	rowBottomBar
	rowPrompt

	// fullStackRows is every row the footer draws. It is named once so the count the footer
	// renders, the count the scroll region leaves for the log, and the count the tests assert
	// cannot drift apart.
	fullStackRows
)

// stackRowsToKeep returns the positions of the rows a terminal of this height draws, in screen
// order.
//
// On a terminal tall enough it is every row in order. Below that it is the floor: the prompt row
// and the bar carrying the session, since a reader who cannot type cannot use the interface and a
// bar that only reports is the first thing to lose.
//
// Which rows are shed on the way down, and in what order, is the reader's decision and is not
// settled; this is a floor rather than a ladder.
func stackRowsToKeep(height int) []int {
	if height >= fullStackRows {
		keep := make([]int, fullStackRows)
		for i := range keep {
			keep[i] = i
		}
		return keep
	}
	return []int{rowBottomBar, rowPrompt}
}

// footerScreenRow is the row of the terminal that footer row k is on.
//
// The stack is at the top of the screen, so the first row is row one and row k is one further
// down. There is no arithmetic against the terminal's height here, which is the point: a row
// named by a height is a row that moves when the height is read a second time, and a row that
// moves is a row the caret can end up off.
//
// What is left below the stack is the log's, and the count the footer draws is what says how
// much of that there is.
func footerScreenRow(screen *Screen, k int) int {
	return 1 + k
}

// promptScreenRow is the row of the terminal the prompt row is on.
//
// It is the last of the six, so it is the row the caret is placed on, and it is asked for here
// rather than counted up from a row a draw left the cursor on.
func promptScreenRow(screen *Screen) int { return footerScreenRow(screen, rowPrompt) }

// Bar is what the footer carries.
//
// It is a value rather than something read off the session, so the drawing can be tested with
// figures a test chose rather than with figures a session happened to have.
type Bar struct {
	// First and Second are the two leading rows of the footer, carried as fields so whatever fills
	// them is set in the same place as everything else and a row cannot be forgotten when a caller
	// fills the rest. A caller that leaves them empty draws them blank, which is what a Bar built
	// by hand for a test gets.
	First  string
	Second string

	// Top is the bar carrying what changes second by second.
	Top string

	// Bottom is the bar carrying the session, the capture modes and the settings.
	Bottom string

	// Field is the reader's own line, drawn after the prompt on the prompt row.
	Field string

	// Twiddle is the figure and the state word, on the twiddle row, and is empty when no turn
	// is running.
	Twiddle string
}

// DrawLog writes rows downward to the terminal, below the footer.
//
// It writes downward and never moves the viewport. Nothing here scrolls, clips or positions: the
// terminal owns the rows below the footer and the reader's own scrollback is the transcript. A
// row that would be wider than the terminal is cut rather than wrapped, since a wrapped row is
// two rows and the terminal and the log would then disagree about what was said.
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
// The palette is asked for a role per span rather than per row, since a row may carry several: a
// tool line is a tool colour over its own words. A row with no spans is written plainly, which is
// the common case and the one that must cost the least.
//
// The walk is in one piece: every byte of the row is written exactly once, either inside a span or
// outside one. Writing the text before each span and then jumping past the span loses the span's own
// text, so a row whose spans cover its middle arrives with a hole in it, which is worse than a row
// with no colour at all.
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
			// Spans arrive in whatever order the writer produced them, and an out-of-order one
			// would write its own text twice. Skipping it is better than a row that says the same
			// words in the wrong colours.
			continue
		}

		b.WriteString(text[written:start])
		b.WriteString(p.Sequence(span.Role))
		b.WriteString(text[start:end])
		written = end
	}
	b.WriteString(text[written:])

	// The reset carries the theme's base back, so a row ending mid-span leaves the next row on the
	// theme's background rather than on whatever the last colour was. It is written after the whole
	// row rather than inside the loop, since a reset before the tail would leave the tail on the
	// wrong background.
	b.WriteString(p.Reset())
	return b.String()
}

// spanBounds clamps one span to the text as written and as cut.
//
// A span is a byte range over the whole row, and a row cut from the tail has fewer bytes than it
// did, so a span past the cut ends at the cut rather than running past the end of what is written.
// A span beginning past the cut is dropped, which is the same as being empty.
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

// DrawStack writes the footer, from the first row down to the prompt.
//
// The stack is written at the top of the screen by position rather than wherever the cursor
// happens to be. The cursor at startup is on the shell's last line, since the shell printed
// something before the program ran, and a stack drawn from there lands under the reader's own
// scrollback rather than at row one.
//
// It is written as terminal rows rather than into a pinned region, so it is the reader's decision
// how often it is redrawn: a caller that redraws it on every keystroke will advance the screen by
// the footer's height each time. Start redraws only the row that changed, and Run draws it once.
func DrawStack(w *Screen, bar Bar, palette Palette) {
	if w.Height() < 1 {
		return
	}

	keep := stackRowsToKeep(w.Height())
	lines := stackLines(bar, palette)

	// The first row is addressed before anything is written, since the write is a line ending
	// downward and the terminal puts the first line where the cursor is.
	w.Write(escapePosition(footerScreenRow(w, keep[0]), 1))
	for _, at := range keep {
		w.Write(lines[at] + "\r\n")
	}
}

// DrawFooterRow rewrites one row of the footer where it stands.
//
// It is the case that stops a keystroke advancing the frame. The whole footer is written as
// terminal rows, so drawing it again for one character appended the footer's height and a line
// ending, and pushed the reader's own line down the screen by that much every time they typed. One
// row rewritten in place moves nothing.
//
// The row is addressed by arithmetic rather than by counting back up from wherever the cursor is,
// since the footer is written downward and leaves the cursor below it.
func DrawFooterRow(w *Screen, at int, bar Bar, palette Palette) {
	if at < 0 || at >= fullStackRows {
		return
	}
	w.Write(escapePosition(footerScreenRow(w, at), 1) + escapeEraseLine)
	w.Write(stackLines(bar, palette)[at])
}

// stackLines renders the stack as rows in screen order.
//
// The first row returned is the top of the stack and the last is the last of the six, because
// DrawStack writes downward and the terminal puts the first line it receives where the cursor is.
// The two reserved rows are first, then the twiddle, the two bars, and the prompt.
func stackLines(bar Bar, palette Palette) []string {
	lines := make([]string, fullStackRows)
	lines[rowFirst] = chromeLine(palette, bar.First)
	lines[rowSecond] = chromeLine(palette, bar.Second)
	lines[rowTwiddle] = chromeLine(palette, bar.Twiddle)
	lines[rowTopBar] = chromeLine(palette, bar.Top)
	lines[rowBottomBar] = chromeLine(palette, bar.Bottom)
	lines[rowPrompt] = chromeLine(palette, Prompt+bar.Field)
	return lines
}

// chromeLine wraps a stack row in the chrome role when colour is on.
//
// It takes a palette rather than reading one so the stack is testable with colour off, and so a row
// with no colour in it is the same row of text a reader would get with colour on and a terminal that
// ignores it.
func chromeLine(p Palette, line string) string {
	if !p.On() || line == "" {
		return line
	}
	return p.Sequence(RoleChrome) + line + p.Reset()
}

// sweepStatus colours the Status value of a rendered top bar, horizontally.
//
// The value is the first field on the bar, so it starts after the literal "Status: " and is as long
// as the value itself. A bar that does not begin that way is returned unchanged, since a sweep
// applied to the wrong span is a bar whose first field is unreadable.
//
// It is a function rather than a span on the bar because the bar renderers return one string and the
// colour has to change every step while a turn runs: a span computed once at render time would
// carry one colour for the length of the turn.
func sweepStatus(top, value string, step int) string {
	head := statusPrefix + value
	if !strings.HasPrefix(top, head) {
		return top
	}
	// The label is written outside the sweep and the value inside it: the reader looks for the
	// field by its name and the name is not part of the changing figure.
	return statusPrefix + SweepText(value, step) + top[len(head):]
}

// SweepText writes text with a hue that turns along the row and shifts with the step.
//
// The hue at column c is the step's rotation plus c degrees, taken modulo 360. Spreading the hue
// along the row rather than turning a single colour is what makes it read as a horizontal pattern:
// one figure in one colour reads as a light changing, several in a row read as something
// travelling.
func SweepText(text string, step int) string {
	var b strings.Builder
	for c, r := range []rune(text) {
		b.WriteString(directRGB(hueRGB(float64(sweepStepDegrees*step + c))))
		b.WriteRune(r)
	}
	b.WriteString(bareReset)
	return b.String()
}

// sweepStepDegrees is how far the pattern turns on one step.
//
// Thirty-six is a tenth of a full turn, so ten steps bring it back to where it started and the cycle
// is a reader can count rather than an arbitrary drift.
const sweepStepDegrees = 36

// directRGB writes the direct-colour sequence for three channels.
//
// It is the 24-bit form rather than the nearest of the 256 colour cube, because a sweep of adjacent
// hues is exactly the case a cube quantises into bands.
func directRGB(r, g, b uint8) string {
	return "\x1b[38;2;" + strconv.Itoa(int(r)) + ";" + strconv.Itoa(int(g)) +
		";" + strconv.Itoa(int(b)) + "m"
}

// sweepLightness and sweepSaturation are fixed while the hue turns along the row.
//
// Fixed because a rotating hue alone keeps every channel away from zero. Interpolation between
// colours has to floor a channel somewhere, and a floored channel is a dark band crossing the row,
// which reads as a figure in the pattern rather than as the pattern.
const (
	sweepLightness  = 0.6
	sweepSaturation = 0.9
)

// hueRGB converts a hue in degrees to channels at the fixed lightness and saturation.
//
// It is the standard conversion, written out rather than taken from a table, and it is spelled in
// fractions of one so the arithmetic can be checked by reading: the chroma is the saturation scaled
// by how far the lightness is from a half, the second channel is the chroma scaled by how far the hue
// is from the middle of its sixty degree sector, and both are lifted by the remainder that puts the
// darkest channel at the lightness.
func hueRGB(hue float64) (uint8, uint8, uint8) {
	hue = math.Mod(hue, 360)
	if hue < 0 {
		hue += 360
	}

	chroma := (1 - math.Abs(2*sweepLightness-1)) * sweepSaturation
	second := chroma * (1 - math.Abs(math.Mod(hue/60, 2)-1))
	mid := sweepLightness - chroma/2

	var r, g, b float64
	switch sector := hue / 60; {
	case sector < 1:
		r, g, b = chroma, second, 0
	case sector < 2:
		r, g, b = second, chroma, 0
	case sector < 3:
		r, g, b = 0, chroma, second
	case sector < 4:
		r, g, b = 0, second, chroma
	case sector < 5:
		r, g, b = second, 0, chroma
	default:
		r, g, b = chroma, 0, second
	}

	return channel(r + mid), channel(g + mid), channel(b + mid)
}

// channel turns a fraction of one into eight bits.
//
// It rounds rather than truncates, so a channel at the top of its range reaches 255 rather than 254
// and a sweep whose brightest channel is its red does not end a step short of red.
func channel(v float64) uint8 {
	n := v*255 + 0.5
	if n < 0 {
		return 0
	}
	if n > 255 {
		return 255
	}
	return uint8(n)
}

// Field is one bar field, a name and a figure.
type Field struct {
	// Name is what the reader reads.
	Name string

	// Value is the figure, empty where there is none.
	Value string
}

// RenderBar renders a bar from its fields, at the width of what it carries.
//
// A bar is never cut. The footer rows are written downward without being clipped, so a bar wider
// than the terminal overflows its row rather than losing the end of itself, and what a narrow
// reader loses is a field rather than half of one.
//
// The width is a floor rather than a cap, and at any width the whole field list is joined, so a
// bar is the same text on every terminal. A caller that has a row it has already measured draws
// its own cut, rather than asking the bar to drop a field it wanted to keep.
func RenderBar(fields []Field, width int) string {
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		parts = append(parts, f.Name+": "+orNone(f.Value))
	}
	return joinWithin(parts, " | ", width)
}

// joinWithin joins fields, and never drops one.
//
// The width is the bar's floor rather than a width to cut to, so the whole list is joined at every
// width and the separator is what carries the reading. A loop that treated the width as a target
// would drop every field after the first at a small one, and the bar would stop saying what it
// carried, which is the opposite of what a floor is for.
//
// The width is kept as a parameter because it is the bar's own property rather than the
// terminal's, and a bar that shrank to fit a wide terminal would be a bar the reader had to scan
// to find the field they are watching.
func joinWithin(parts []string, sep string, width int) string {
	return strings.Join(parts, sep)
}
