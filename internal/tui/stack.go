package tui

import (
	"math"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// The frame, as it is now: the program owns the screen, and the screen is five fixed rows
// below a scrollback that takes whatever height is left.
//
// Bottom to top: the pane bar, bar two (the longer-term fields - provider, model,
// verbosity, mouse, and whether the viewport is at the live edge), bar one (the live,
// fast-changing fields - state, the sweeping figure, queue depth; see
// barOneFields/barTwoFields), one blank separator line, and the prompt - this order
// confirmed by Glen (2026-10-06). Everything above those five rows is scrollback: the
// tail of the log, newest row just above the prompt, oldest pushed off the top as it
// grows. This is loreloom/UI-redesign.md's shape (2026-10-06) with one addition: that
// document names only the prompt, blank line, and two bars, and Glen added the pane bar
// as a fifth fixed row rather than cutting it (adr-0000019/0000020 are carried forward,
// not superseded). It is not the field-column-beside-every-log-row shape the ten ADRs
// this does supersede (see staged/adr-index.txt) describe.
//
// # Why the log is not below the stack any more
//
// Before this, and before the one-row-per-field shape before that, six rows were a footer's,
// drawn once and left alone, and the log was a region below them that the terminal scrolled.
// That arrangement needed the program to know where the region started and ended, and it got
// it wrong: the scroll region took a row count and wrote the first row as one, so the six
// rows the reader was typing into were inside the region and a long log went over them.
//
// A frame with one owner has no seam to get wrong. The program holds the log, and decides
// for itself what a row shows, so nothing depends on where the terminal thinks its region
// begins.
//
// # What this costs
//
// The reader's own scrollback is not the transcript any more. It is whatever the terminal
// kept from before the program started, and the transcript is drawn by the program from rows
// it holds. A reader who scrolls back past the program's first row finds their shell, not the
// conversation.

// Prompt is what the reader's line is drawn behind.
//
// This is the string loreloom/UI-redesign.md gives (2026-10-06), confirmed against a
// rendered mockup: a space on each side of the dollar sign. adr-0000010 settled on
// `root@lolhost @ ` instead, deliberately; that disagreement is recorded in
// staged/adr-index.txt rather than resolved there, and is resolved here in the string's
// favour because the mockup built from this string is the one that was confirmed.
const Prompt = "root@lolhost $ "

// barRows is how many fixed rows sit below the scrollback: the prompt, one blank
// separator line, the two status bars loreloom/UI-redesign.md describes, and the pane
// bar. loreloom/UI-redesign.md itself names only four; Glen extended it to five
// (2026-10-06) to give the pane bar (adr-0000019/0000020) a row of its own rather than
// cutting it, dropping it into bar two, or leaving it homeless.
const barRows = 5

// barOneFields and barTwoFields choose which Status fields render on each status bar.
//
// loreloom/UI-redesign.md names bar two's fields explicitly - provider, model,
// verbosity, mouse, scrollback - and says nothing about bar one beyond "rapidly
// changing statistics." Glen confirmed bar one's list and the bars' order
// (2026-10-06): state, the sweeping figure, and the queue depth, with bar one
// above bar two.
var barOneFields = []int{fieldState, fieldFigure, fieldQueue}

// barTwoFields is four of the five fields loreloom/UI-redesign.md names for bar two.
// The fifth, scrollback-on, is Session.AtLiveEdge - see renderBarTwo.
var barTwoFields = []int{fieldProvider, fieldModel, fieldVerbosity, fieldMouse}

// StatusFields is how many named status fields exist, whether or not a given redesign
// of the frame renders all of them. It no longer bounds how many rows the frame draws
// (see barRows and scrollbackRows for that) - it only sizes the Status array below, so a
// field added to the fieldXxx list and the array that holds its value cannot drift apart.
const StatusFields = 20

// FieldIndent is how far in the prompt row's own text sits behind the prompt.
//
// Zero, since the prompt is at column one and the caret follows the text the reader has
// typed rather than the prompt in front of it. It is named rather than deleted because the
// caret arithmetic in caret.go offsets by it, and a constant of zero there reads as "this was
// decided" rather than "this was forgotten".
const FieldIndent = 0

// statusPrefix is the text the changing bar's first field begins with.
//
// It is named because the colour sweep has to know where the Status value starts and ends
// inside a bar that is rendered as one string, and the prefix is the only thing in this file
// that says so.
const statusPrefix = "Status: "

// The fields, in the order they read down the screen.
//
// The order is what the reader settled: who the session is, what is running, what the turn
// is doing, what the reader may act on, and what the session is holding. Each is a named
// constant rather than an index, since a field is a thing the reader looks for down a column
// and an index is a thing the code counts with.
//
// The name beside each is what the row reads, and the two cannot drift apart because the name
// is written here once and the value is written at the place that fills it.
const (
	// fieldCwd is the directory the session is typed into.
	fieldCwd = iota

	// fieldSession is which session the field is typed into.
	fieldSession

	// fieldProvider is the endpoint host.
	fieldProvider

	// fieldModel is the model answering.
	fieldModel

	// fieldKey reports whether a credential is set up.
	fieldKey

	// fieldState is the state the client is in, and is the one field that changes second by
	// second without the reader asking.
	fieldState

	// fieldFigure is the figure beside the state, and is the field that sweeps.
	fieldFigure

	// fieldApproval reports whether programs run without asking.
	fieldApproval

	// fieldVerbosity is how much the model is asked to answer with.
	fieldVerbosity

	// fieldCognito reports that nothing is recorded.
	fieldCognito

	// fieldColor reports whether colour is on.
	fieldColor

	// fieldMouse reports whether mouse reporting is on.
	fieldMouse

	// fieldCopy reports whether a copy is available.
	fieldCopy

	// fieldBell reports whether the bell is rung on a reply.
	fieldBell

	// fieldPane is which pane is shown.
	fieldPane

	// fieldWorkers is how many workers are running.
	fieldWorkers

	// fieldQueue is how many prompts are queued.
	fieldQueue

	// fieldHeld is how many rows the log holds.
	fieldHeld

	// fieldFolded is how many rows the log has dropped.
	fieldFolded

	// fieldLevels is how many conversation levels exist.
	fieldLevels

	// fieldPrompt is the prompt row, and it is the row the caret is on.
	fieldPrompt
)

// statusCount is how many fields exist, named so a loop and an array agree.
//
// It is one more than fieldPrompt, since the prompt row is a field like the rest and is the
// last of them, and it is what makes the count twenty rather than nineteen.
//
// It is written as StatusFields rather than as the field index, and the two agreeing is the
// check: a field added above the prompt without the count moving is a frame one row taller
// than the height the reader asked for.
const statusCount = StatusFields

// The name each field reads as, in the same order as the fields above.
//
// It is a function rather than an array so the name sits beside the constant it belongs to,
// which is the only way the two cannot drift apart. A reader seeing a field on the screen and
// a name in the source is looking at the same thing written down twice, and the second
// writing is the one that can go stale.
func fieldName(k int) string {
	switch k {
	case fieldCwd:
		return "cwd"
	case fieldSession:
		return "session"
	case fieldProvider:
		return "provider"
	case fieldModel:
		return "model"
	case fieldKey:
		return "key"
	case fieldState:
		return "state"
	case fieldFigure:
		return "figure"
	case fieldApproval:
		return "approval"
	case fieldVerbosity:
		return "verbosity"
	case fieldCognito:
		return "cognito"
	case fieldColor:
		return "color"
	case fieldMouse:
		return "mouse"
	case fieldCopy:
		return "copy"
	case fieldBell:
		return "bell"
	case fieldPane:
		return "pane"
	case fieldWorkers:
		return "workers"
	case fieldQueue:
		return "queue"
	case fieldHeld:
		return "held"
	case fieldFolded:
		return "folded"
	case fieldLevels:
		return "levels"
	case fieldPrompt:
		return ""
	default:
		return ""
	}
}


// Status is what each field says, field by field.
//
// It is a fixed-size array rather than a slice because the count is fixed, and a caller
// setting one field and leaving the rest blank would be a caller that forgot a field. The
// zero value is a valid status with every field blank, which is what a frame drawn before the
// session has read anything looks like.
//
// A value is whatever the field has to say, so a provider is its whole name and a count is
// its whole figure. The one character rule is gone: a field that could not say what it meant
// in one character said nothing, and every field here has something to say.
type Status [statusCount]string

// barRow is one row of the frame: the fields in the leading columns and the log beside them.
//
// The log is empty on the prompt row, since that row carries the reader's own line rather than
// a log row, and a prompt with a log row behind it is a prompt the reader cannot read.
type barRow struct {
	// fields is the name and the value, padded to the name column.
	fields string
	spans  []Span

	// log is the log row shown beside the fields, which is the reader's own line on the
	// prompt row.
	log string
}

// scrollbackRows is how many rows of log the frame draws above the fixed bottom rows.
//
// It is height minus barRows, floored at zero. A terminal shorter than barRows has no
// scrollback at all; what a terminal that short should shed first among the prompt,
// blank line, two bars, and pane bar is not settled by loreloom/UI-redesign.md or by
// Glen's own extension of it to five rows (it is silent on shedding, the way
// adr-0000016 settled it for the design this one supersedes), so the fallback below
// draws only the prompt rather than guessing a shedding order nobody has decided.
func scrollbackRows(height int) int {
	if height < barRows {
		return 0
	}
	return height - barRows
}

// Bar is what the frame carries that is not a field value.
//
// A row is a set of field values and a log row, and the only thing neither of those carries
// is the reader's own line, since a field is a value about the session and the field the
// reader is typing is not that.
type Bar struct {
	// Status is the value of each field, field by field.
	Status Status

	// Field is the reader's own line, drawn on the prompt row.
	Field string
}

// Frame is the screen-sized primitive that draws the status rows and transcript.
// tview owns terminal setup and input; this primitive owns the cell layout.
type Frame struct {
	*tview.Box
	bar     Bar
	log     []Row
	palette Palette
	caret   int
	step    int
	scroll  int
	paste   func(string)
}

// NewFrame builds the custom tview primitive used for the interface screen.
func NewFrame() *Frame { return &Frame{Box: tview.NewBox()} }

// SetContent replaces the values the next Draw will put on the screen.
func (f *Frame) SetContent(bar Bar, log []Row, palette Palette) {
	f.bar = bar
	f.log = append(f.log[:0], log...)
	f.palette = palette
}

// SetCaret records the prompt caret's rune offset.
func (f *Frame) SetCaret(caret int) { f.caret = caret }

// SetSweepStep sets the animation position for the changing figure field.
func (f *Frame) SetSweepStep(step int) { f.step = step }

// SetScroll records how many rows back from the live edge the viewport sits.
func (f *Frame) SetScroll(scroll int) { f.scroll = scroll }

// SetPasteHandler installs the handler for text pasted into the prompt.
func (f *Frame) SetPasteHandler(handler func(string)) { f.paste = handler }

// PasteHandler gives tview the prompt's paste handler.
func (f *Frame) PasteHandler() func(string, func(tview.Primitive)) {
	if f.paste == nil {
		return nil
	}
	return func(text string, _ func(tview.Primitive)) { f.paste(text) }
}

// Draw paints the frame into tcell's cell grid.
func (f *Frame) Draw(screen tcell.Screen) {
	x, y, width, height := f.GetRect()
	base := f.palette.BaseStyle()
	chrome := frameStyle(f.palette, RoleChrome)
	for row := 0; row < height; row++ {
		for col := 0; col < width; col++ {
			screen.SetContent(x+col, y+row, ' ', nil, base)
		}
	}

	if height < 1 {
		return
	}

	// A terminal too short for the full stack sheds the blank line, then the pane bar,
	// then bar two, then bar one, in that order - Glen's shedding order (2026-10-06).
	// The prompt always survives, since the reader is typing into it. Each surviving row
	// draws top to bottom starting at the terminal's own first row, in their usual
	// relative order; there is no scrollback at these heights (see scrollbackRows).
	if height < barRows {
		row := 0
		drawCellText(screen, x, y+row, width, Prompt+f.bar.Field, chrome)
		f.showCaret(screen, x, y+row, width)
		row++

		if height >= 2 {
			f.drawBarOne(screen, x, y+row, width, chrome)
			row++
		}
		if height >= 3 {
			drawCellText(screen, x, y+row, width, renderBarTwo(f.bar.Status, f.scroll <= 0), chrome)
			row++
		}
		if height >= 4 {
			drawCellText(screen, x, y+row, width, renderPaneBar(f.bar.Status, width), chrome)
		}
		return
	}

	backlog := scrollbackRows(height)
	end := len(f.log) - f.scroll
	if end < 0 {
		end = 0
	}
	if end > len(f.log) {
		end = len(f.log)
	}
	shown := make([]Row, 0, backlog)
	for i := end - 1; i >= 0 && len(shown) < backlog; i-- {
		shown = append(shown, PlainRow(f.log[i]))
	}
	// shown is newest first; it fills the scrollback upward from just above the prompt.
	for i, row := range shown {
		at := backlog - 1 - i
		text, offset, cut := cutTail(row.Text, width)
		if cut {
			drawCellText(screen, x, y+at, width, ellipsis, base)
			drawStyledCellText(screen, x+DisplayWidth(ellipsis), y+at, width-DisplayWidth(ellipsis), text, offset, row.Spans, f.palette, base)
			continue
		}
		drawStyledCellText(screen, x, y+at, width, text, offset, row.Spans, f.palette, base)
	}

	promptRow := backlog
	barOneRow := backlog + 2
	barTwoRow := backlog + 3
	paneBarRow := backlog + 4

	drawCellText(screen, x, y+promptRow, width, Prompt+f.bar.Field, chrome)
	f.showCaret(screen, x, y+promptRow, width)

	f.drawBarOne(screen, x, y+barOneRow, width, chrome)

	drawCellText(screen, x, y+barTwoRow, width, renderBarTwo(f.bar.Status, f.scroll <= 0), chrome)
	drawCellText(screen, x, y+paneBarRow, width, renderPaneBar(f.bar.Status, width), chrome)
}

// renderPaneBar builds the pane bar's text, truncated to width with an ellipsis rather
// than scrolled or folded. adr-0000020 itself describes a silent truncation with no
// mark that more exist past the edge; Glen asked to keep the ellipsis instead
// (2026-10-06), so this bar truncates the way every other row in this file already does
// (see cutTail's callers) rather than matching 0000020's text literally - a deliberate,
// confirmed departure from that record, not an oversight.
//
// There is only ever one pane today (fieldPane is hardcoded "main" in run.go's status
// builder; 0000007's multiplexer was never built), so this draws that one name honestly
// rather than fabricating a pane list to truncate.
func renderPaneBar(status Status, width int) string {
	return CutColumnFromEnd(status[fieldPane], width)
}

// showCaret places the terminal cursor on the prompt row at the reader's caret offset.
func (f *Frame) showCaret(screen tcell.Screen, x, y, width int) {
	field := []rune(f.bar.Field)
	caret := min(f.caret, len(field))
	column := DisplayWidth(Prompt + string(field[:caret]))
	if width > 0 {
		column = min(column, width-1)
	}
	screen.ShowCursor(x+column, y)
}

// drawBarOne draws bar one at row y, with the figure segment swept. Factored out of
// Draw's main path so the shedding ladder below can draw the same row at whatever
// height it survives to, rather than repeating the sweep arithmetic twice.
func (f *Frame) drawBarOne(screen tcell.Screen, x, y, width int, chrome tcell.Style) {
	prefix, figure, suffix := renderBarOne(f.bar.Status)
	drawCellText(screen, x, y, width, prefix, chrome)
	figureX := DisplayWidth(prefix)
	drawSweepText(screen, x+figureX, y, width-figureX, figure, f.step, f.palette)
	suffixX := figureX + DisplayWidth(figure)
	if suffixX < width {
		drawCellText(screen, x+suffixX, y, width-suffixX, suffix, chrome)
	}
}

// renderBarOne builds the live, fast-changing status bar.
//
// It returns the text before the figure, the figure's own text (drawn separately so Draw
// can sweep it), and the text after. The field list is barOneFields; see its doc comment
// for why these three and not others.
func renderBarOne(status Status) (prefix, figure, suffix string) {
	parts := make([]string, 0, len(barOneFields))
	figureIndex := -1
	for _, k := range barOneFields {
		if k == fieldFigure {
			figureIndex = len(parts)
		}
		parts = append(parts, fieldName(k)+":"+status[k])
	}
	if figureIndex < 0 {
		return strings.Join(parts, " · "), "", ""
	}
	prefix = strings.Join(parts[:figureIndex], " · ")
	if prefix != "" {
		prefix += " · " + fieldName(fieldFigure) + ":"
	} else {
		prefix = fieldName(fieldFigure) + ":"
	}
	figure = status[fieldFigure]
	if figureIndex+1 < len(parts) {
		suffix = " · " + strings.Join(parts[figureIndex+1:], " · ")
	}
	return prefix, figure, suffix
}

// renderBarTwo builds the longer-term status bar: the four Status fields in barTwoFields,
// plus whether the viewport is at the live edge, which is DESIGN.md §5's fact (read fresh
// each draw, not a stored setting - see Session.AtLiveEdge) standing in for the
// scrollback-on field loreloom/UI-redesign.md names.
//
// Provider and model are written bare, as a reader would say them; the rest are named,
// matching the mockup confirmed this session (`openrouter · claude-sonnet-5 ·
// verbosity:0 · mouse:on · scroll:on`).
func renderBarTwo(status Status, atLiveEdge bool) string {
	parts := []string{status[fieldProvider], status[fieldModel]}
	for _, k := range barTwoFields {
		if k == fieldProvider || k == fieldModel {
			continue
		}
		parts = append(parts, fieldName(k)+":"+status[k])
	}
	parts = append(parts, "scroll:"+onOff(atLiveEdge, "live", "back"))
	return strings.Join(parts, " · ")
}

func drawSweepText(screen tcell.Screen, x, y, width int, text string, step int, p Palette) {
	col := 0
	for _, r := range text {
		w := runewidth(r)
		if w == 0 {
			if col > 0 {
				screen.SetContent(x+col-1, y, r, nil, p.BaseStyle())
			}
			continue
		}
		if col+w > width {
			break
		}
		red, green, blue := hueRGB(float64(sweepStepDegrees*step + col))
		style := p.BaseStyle().Foreground(tcell.NewRGBColor(int32(red), int32(green), int32(blue)))
		screen.SetContent(x+col, y, r, nil, style)
		col += w
	}
}

func frameStyle(p Palette, role Role) tcell.Style {
	if !p.On() {
		return tcell.StyleDefault
	}
	fg, _, attrs := p.Style(role).Decompose()
	_, bg, _ := p.BaseStyle().Decompose()
	return tcell.StyleDefault.Foreground(fg).Background(bg).Attributes(attrs)
}

func cutTail(text string, width int) (string, int, bool) {
	cut := CutColumnFromEnd(text, width)
	if cut == text {
		return text, 0, false
	}
	if cut == ellipsis {
		return "", len(text), true
	}
	tail := strings.TrimPrefix(cut, ellipsis)
	return tail, len(text) - len(tail), true
}

func drawStyledCellText(screen tcell.Screen, x, y, width int, text string, offset int, spans []Span, palette Palette, base tcell.Style) {
	col := 0
	byteOffset := offset
	for _, r := range text {
		if col >= width {
			break
		}
		style := base
		for _, span := range spans {
			if byteOffset >= span.Start && byteOffset < span.End {
				style = frameStyle(palette, span.Role)
				break
			}
		}
		runeWidth := runewidth(r)
		if runeWidth == 0 {
			if col > 0 {
				screen.SetContent(x+col-1, y, r, nil, style)
			}
		} else if col+runeWidth <= width {
			screen.SetContent(x+col, y, r, nil, style)
			col += runeWidth
		}
		byteOffset += len(string(r))
	}
}

func drawCellText(screen tcell.Screen, x, y, width int, text string, style tcell.Style) {
	col := 0
	for _, r := range text {
		if col >= width {
			break
		}
		w := runewidth(r)
		if w < 1 {
			screen.SetContent(x+col-1, y, r, nil, style)
			continue
		}
		if col+w > width {
			break
		}
		screen.SetContent(x+col, y, r, nil, style)
		col += w
	}
}

func runewidth(r rune) int {
	return DisplayWidth(string(r))
}

func stripCSI(text string) string {
	var b strings.Builder
	for i := 0; i < len(text); {
		if text[i] == 0x1b && i+1 < len(text) && text[i+1] == '[' {
			i += 2
			for i < len(text) && (text[i] < 0x40 || text[i] > 0x7e) {
				i++
			}
			if i < len(text) {
				i++
			}
			continue
		}
		b.WriteByte(text[i])
		i++
	}
	return b.String()
}

// sweepStatus colours the Status value of a rendered top bar, horizontally.
//
// The value is the first field on the bar, so it starts after the literal "Status: " and is as
// long as the value itself. A bar that does not begin that way is returned unchanged, since a
// sweep applied to the wrong span is a bar whose first field is unreadable.
//
// It is a function rather than a span on the bar because the bar renderers return one string
// and the colour has to change every step while a turn runs: a span computed once at render
// time would carry one colour for the length of the turn.
func sweepStatus(top, value string, step int) string {
	head := statusPrefix + value
	if !strings.HasPrefix(top, head) {
		return top
	}
	// The label is written outside the sweep and the value inside it: the reader looks for
	// the field by its name and the name is not part of the changing figure.
	return statusPrefix + SweepText(value, step) + top[len(head):]
}

// SweepText writes text with a hue that turns along the row and shifts with the step.
//
// The hue at column c is the step's rotation plus c degrees, taken modulo 360. Spreading the
// hue along the row rather than turning a single colour is what makes it read as a horizontal
// pattern: one figure in one colour reads as a light changing, several in a row read as
// something travelling.
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
// Thirty-six is a tenth of a full turn, so ten steps bring it back to where it started and the
// cycle is a reader can count rather than an arbitrary drift.
const sweepStepDegrees = 36

// directRGB writes the direct-colour sequence for three channels.
//
// It is the 24-bit form rather than the nearest of the 256 colour cube, because a sweep of
// adjacent hues is exactly the case a cube quantises into bands.
func directRGB(r, g, b uint8) string {
	return "\x1b[38;2;" + strconv.Itoa(int(r)) + ";" + strconv.Itoa(int(g)) +
		";" + strconv.Itoa(int(b)) + "m"
}

// sweepLightness and sweepSaturation are fixed while the hue turns along the row.
//
// Fixed because a rotating hue alone keeps every channel away from zero. Interpolation
// between colours has to floor a channel somewhere, and a floored channel is a dark band
// crossing the row, which reads as a figure in the pattern rather than as the pattern.
const (
	sweepLightness  = 0.6
	sweepSaturation = 0.9
)

// hueRGB converts a hue in degrees to channels at the fixed lightness and saturation.
//
// It is the standard conversion, written out rather than taken from a table, and it is spelled
// in fractions of one so the arithmetic can be checked by reading: the chroma is the saturation
// scaled by how far the lightness is from a half, the second channel is the chroma scaled by
// how far the hue is from the middle of its sixty degree sector, and both are lifted by the
// remainder that puts the darkest channel at the lightness.
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
// It rounds rather than truncates, so a channel at the top of its range reaches 255 rather than
// 254 and a sweep whose brightest channel is its red does not end a step short of red.
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
// A bar is never cut. The frame rows are written without being clipped, so a bar wider than the
// terminal overflows its row rather than losing the end of itself, and what a narrow reader
// loses is a field rather than half of one.
//
// The width is a floor rather than a cap, and at any width the whole field list is joined, so
// a bar is the same text on every terminal. A caller that has a row it has already measured
// draws its own cut, rather than asking the bar to drop a field it wanted to keep.
func RenderBar(fields []Field, width int) string {
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		parts = append(parts, f.Name+": "+orNone(f.Value))
	}
	return joinWithin(parts, " | ", width)
}

// joinWithin joins fields, and never drops one.
//
// The width is the bar's floor rather than a width to cut to, so the whole list is joined at
// every width and the separator is what carries the reading. A loop that treated the width as a
// target would drop every field after the first at a small one, and the bar would stop saying
// what it carried, which is the opposite of what a floor is for.
//
// The width is kept as a parameter because it is the bar's own property rather than the
// terminal's, and a bar that shrank to fit a wide terminal would be a bar the reader had to
// scan to find the field they are watching.
func joinWithin(parts []string, sep string, width int) string {
	return strings.Join(parts, sep)
}
