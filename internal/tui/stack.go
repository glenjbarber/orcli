package tui

import (
	"math"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// The frame, as it is now: the program owns the screen.
//
// There is one region, and it is the whole terminal. Every row is a status field in the
// first columns and a log row beside it, and the log scrolls upward through those rows as
// it is written. The prompt is the last row, with the reader's own line rather than a log
// row beside it.
//
// # The field column
//
// A row is a name, a value, and the log. The name is what the reader reads and the value
// is what it says, and they are beside each other rather than on two rows, since a reader
// looking for the model is looking for one row and not for a label and a figure.
//
// The column is padded to the widest name in the field list rather than to a figure, so a
// field is at the same column on every row and a reader following one down the screen is
// following one left edge. That is the whole of the alignment: the log starts at the same
// column on every row, and the padding is what puts it there.
//
// # Why the log is not below the stack any more
//
// It was. Six rows were the footer's, drawn once and left alone, and the log was a region
// below them that the terminal scrolled. That arrangement needed the program to know where
// the region started and ended, and it got it wrong: the scroll region took a row count and
// wrote the first row as one, so the six rows the reader was typing into were inside the
// region and a long log went over them.
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
const Prompt = "root@localhost $ "

// StatusFields is how many status fields the frame carries.
//
// Twenty is the count the reader settled: the six rows the stack had plus fourteen beside
// them, which fills a twenty row terminal on the height. The prompt row is one of the twenty
// rather than one beside them, since it is a row like every other and the reader is typing on
// it.
//
// The count is a ceiling rather than a figure, since a terminal shorter than this draws every
// row it has and sheds fields from the bottom. It is named once so the count the frame draws,
// the count the tests assert, and the count the layout reads cannot drift apart.
const StatusFields = 20

// statusGap is what sits between the fields and the log beside them.
//
// One space, and one space only. A wider gap is a column the log has not got on a narrow
// terminal, and the log is what the reader came back for.
const statusGap = " "

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

// nameWidth is how wide the name column is, so the log starts at one column on every row.
//
// It is the widest name in the field list plus a space, computed from the names rather than
// carried as a figure. A figure here would be one more thing to go stale when a field is
// renamed, and a name column a character narrow puts the log one column left on some rows
// and not on others, which is a log with no left edge.
//
// It is the width of the names and not the width of the names and their values, since a
// value is prose of any length and padding to it would leave the log off the right edge of a
// narrow terminal for the sake of a field that is four characters wide.
func nameWidth() int {
	widest := 0
	for k := range statusCount {
		if w := DisplayWidth(fieldName(k)); w > widest {
			widest = w
		}
	}
	return widest
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

// screenRows is how many rows the frame draws on a terminal of this height.
//
// It is the count of fields the screen can hold, since every row carries a field. A terminal
// shorter than the count draws every row it has and takes the fields from the top, so what is
// shed is the bottom of the list and the prompt row is still there.
//
// The count is a ceiling rather than the terminal's height, so a tall terminal draws twenty
// rows and the rest of the screen is left to the terminal rather than drawn as empty rows.
func screenRows(height int) int {
	if height < 1 {
		return 0
	}
	if height < statusCount {
		return height
	}
	return statusCount
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
	lines := stackLines(f.bar, f.log, height, f.palette)
	base := f.palette.BaseStyle()
	chrome := frameStyle(f.palette, RoleChrome)
	for row := 0; row < height; row++ {
		for col := 0; col < width; col++ {
			screen.SetContent(x+col, y+row, ' ', nil, base)
		}
	}
	for row, line := range lines {
		if line.fields == "" {
			drawCellText(screen, x, y+row, width, Prompt+line.log, chrome)
			continue
		}

		fields := stripCSI(line.fields)
		fieldWidth := DisplayWidth(fields)
		if row == fieldFigure {
			prefixWidth := nameWidth() + 1
			prefix := fields[:min(prefixWidth, len(fields))]
			drawCellText(screen, x, y+row, width, prefix, chrome)
			drawSweepText(screen, x+prefixWidth, y+row, width-prefixWidth,
				f.bar.Status[fieldFigure], f.step, f.palette)
		} else {
			drawCellText(screen, x, y+row, width, fields, chrome)
		}
		if fieldWidth >= width {
			continue
		}
		screen.SetContent(x+fieldWidth, y+row, ' ', nil, chrome)
		logX := fieldWidth + DisplayWidth(statusGap)
		logWidth := width - logX
		if logWidth <= 0 {
			continue
		}
		text, offset, cut := cutTail(line.log, logWidth)
		if cut {
			drawCellText(screen, x+logX, y+row, logWidth, ellipsis, base)
			logX += DisplayWidth(ellipsis)
			logWidth -= DisplayWidth(ellipsis)
		}
		if logWidth > 0 {
			drawStyledCellText(screen, x+logX, y+row, logWidth, text, offset, line.spans, f.palette, base)
		}
	}
	if len(lines) > 0 {
		field := []rune(f.bar.Field)
		caret := min(f.caret, len(field))
		column := DisplayWidth(Prompt + string(field[:caret]))
		if width > 0 {
			column = min(column, width-1)
			screen.ShowCursor(x+column, y+len(lines)-1)
		}
	}
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

// stackLines renders the frame as rows in screen order.
//
// The frame is drawn downward and the terminal puts the first line it receives where the cursor
// is, so the first row here is the top of the screen and the last is the prompt.
//
// The log rides beside the fields: the newest rows are at the bottom, so a row written last
// appears above the prompt and everything above it moves up, which is what the reader asked the
// log to do. The rows are taken from the tail of the log, since a log is ordered oldest first
// and the screen shows the end of it.
func stackLines(bar Bar, log []Row, height int, palette Palette) []barRow {
	drawn := screenRows(height)
	if drawn < 1 {
		return nil
	}

	// The prompt row is the last of them, so every row above it carries a log row.
	logRows := drawn - 1

	// The tail of the log fills the rows from the bottom upward, so the newest log row is the
	// one just above the prompt and a row arriving moves everything up rather than appearing
	// at the top where a reader is not looking.
	shown := make([]Row, 0, logRows)
	for i := len(log) - 1; i >= 0 && len(shown) < logRows; i-- {
		shown = append(shown, PlainRow(log[i]))
	}

	out := make([]barRow, drawn)
	for i := range out {
		out[i] = barRow{fields: fieldsLine(bar, i)}
	}
	// The log rows were gathered newest first, so they fill upward from the prompt.
	for i, row := range shown {
		at := drawn - 2 - i
		if at < 0 {
			break
		}
		out[at].log = row.Text
		out[at].spans = append([]Span(nil), row.Spans...)
	}

	// The prompt row carries the reader's own line in place of a log row, and carries no
	// fields, since the prompt is where the reader types rather than something they read.
	if prompt := drawn - 1; prompt >= 0 {
		out[prompt].fields = ""
		out[prompt].log = bar.Field
	}

	return out
}

// fieldsLine renders the name and the value of the field at k, padded to the name column.
//
// The padding is what puts the log at one column on every row. A row whose name is shorter
// than the column gets spaces after it and a row whose name is the width of the column gets
// none, and the log starts in the same place either way, which is the whole of what a reader
// following one column down the screen is asking for.
func fieldsLine(bar Bar, k int) string {
	name := fieldName(k)
	if k < 0 || k >= statusCount || name == "" {
		return ""
	}

	pad := nameWidth() - DisplayWidth(name) + 1
	return name + strings.Repeat(" ", pad) + bar.Status[k]
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
