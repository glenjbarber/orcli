package tui

import (
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// The frame, as it is now: the program owns the screen, and the screen is five fixed rows
// below a scrollback that takes whatever height is left.
//
// Bottom to top: the pane bar, bar two (hostname, credits, cost, context, input and
// output tokens, autosave, stealth and approval), bar one (the live,
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
//
// Glen confirmed a further adjustment to this same string in this session
// (2026-10-07): a leading space before the host name, so the prompt row's own text does
// not start flush against the terminal's left edge. The space on each side of the dollar
// sign from the 2026-10-06 mockup is unchanged; this only adds the one before "root".
const Prompt = " root@lolhost $ "

// barRows is how many fixed rows sit below the scrollback: the prompt, one blank
// separator line, the two status bars loreloom/UI-redesign.md describes, and the pane
// bar. loreloom/UI-redesign.md itself names only four; Glen extended it to five
// (2026-10-06) to give the pane bar (adr-0000019/0000020) a row of its own rather than
// cutting it, dropping it into bar two, or leaving it homeless.
//
// The separator line stays one row. Glen confirmed in this session (2026-10-07) that it
// should carry two leading spaces rather than being left to the frame's own blank fill
// (see Draw's separatorRow write); that is a change to what the row draws, not to how
// many rows the stack has, so barRows is still five.
const barRows = 5

// barOneFields and barTwoFields choose which Status fields render on each status bar.
//
// loreloom/UI-redesign.md names bar two's fields explicitly - provider, model,
// verbosity, mouse, scrollback - and says nothing about bar one beyond "rapidly
// changing statistics." Glen confirmed bar one's list and the bars' order
// (2026-10-06): state, the sweeping figure, and the queue depth, with bar one
// above bar two.
var barOneFields = []int{fieldState, fieldFigure, fieldQueue}

// barTwoFields is the user-confirmed field order from the active status-bar task.
var barTwoFields = []int{fieldHost, fieldCredits, fieldCost, fieldContext, fieldInput, fieldOutput, fieldAutosave, fieldStealth, fieldApproval}

// StatusFields is how many named status fields exist, whether or not a given redesign
// of the frame renders all of them. It no longer bounds how many rows the frame draws
// (see barRows and scrollbackRows for that) - it only sizes the Status array below, so a
// field added to the fieldXxx list and the array that holds its value cannot drift apart.
const StatusFields = 31

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

	// fieldPaneState is whether a worker is running or has just finished at the
	// shown pane: "running", "done", or "" for neither. It is not printed on any
	// bar - fieldName gives it no text - and exists only to tell the pane bar
	// which colour to draw in, which is a second configurable pair of colours
	// kept apart from the plain on/off Color toggle (see config.PaneActiveColor
	// and config.PaneDoneColor).
	fieldPaneState

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

	// fieldPreset is the /level preset in force, "-" when none is.
	//
	// Named fieldPreset rather than fieldLevel so it cannot be mistaken, reading
	// this file, for fieldLevels just above it: that one counts conversation
	// levels, and this one names a /level reply-style preset. See preset.go's
	// doc comment for the same distinction from the command's side.
	fieldPreset

	// fieldPrompt is the prompt row, and it is the row the caret is on.
	fieldPrompt
	fieldHost
	fieldCredits
	fieldCost
	fieldContext
	fieldInput
	fieldOutput
	fieldAutosave
	fieldStealth
)

// statusCount sizes the fixed array of status values, including fields used by
// renderers and bookkeeping that are not all visible at once.
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
	case fieldPaneState:
		return ""
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
	case fieldPreset:
		return "level"
	case fieldPrompt:
		return ""
	case fieldHost:
		return "hostname"
	case fieldCredits:
		return "credits"
	case fieldCost:
		return "cost"
	case fieldContext:
		return "context"
	case fieldInput:
		return "in"
	case fieldOutput:
		return "out"
	case fieldAutosave:
		return "autosave"
	case fieldStealth:
		return "stealth"
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
			f.drawBarTwo(screen, x, y+row, width, chrome)
			row++
		}
		if height >= 4 {
			drawCellText(screen, x, y+row, width, renderPaneBar(f.bar.Status, width),
				paneBarStyle(f.palette, chrome, f.bar.Status[fieldPaneState]))
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
	// shown is newest first, in physical lines rather than log rows: a row with a
	// line break of its own folds into more than one entry here, each still
	// counted against backlog the way a single-line row always was. f.scroll
	// itself stays in log-row units (session.go's ScrollUp/ScrollDown are
	// untouched) - only how many physical lines one row costs changes.
	shown := make([]Row, 0, backlog)
fill:
	for i := end - 1; i >= 0; i-- {
		lines := foldRowLines(PlainRow(f.log[i]), width)
		for j := len(lines) - 1; j >= 0; j-- {
			shown = append(shown, lines[j])
			if len(shown) >= backlog {
				break fill
			}
		}
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
	separatorRow := backlog + 1
	barOneRow := backlog + 2
	barTwoRow := backlog + 3
	paneBarRow := backlog + 4

	drawCellText(screen, x, y+promptRow, width, Prompt+f.bar.Field, chrome)
	f.showCaret(screen, x, y+promptRow, width)

	// The separator row between the prompt and bar one. It was left to the base clear
	// above (every cell already a space) rather than drawn; Glen asked, in this session
	// (2026-10-07), that it carry two spaces explicitly, matching the indent he asked for
	// on the prompt row above it, rather than being blank only because nothing writes to
	// it. Two spaces at a width of two or more look identical to the blank fill they
	// replace - the row was already all spaces - so this is a deliberate row of content
	// rather than a visible change.
	drawCellText(screen, x, y+separatorRow, width, "  ", chrome)

	f.drawBarOne(screen, x, y+barOneRow, width, chrome)

	f.drawBarTwo(screen, x, y+barTwoRow, width, chrome)
	drawCellText(screen, x, y+paneBarRow, width, renderPaneBar(f.bar.Status, width),
		paneBarStyle(f.palette, chrome, f.bar.Status[fieldPaneState]))
}

// paneBarStyle picks the style the pane bar draws in: a configured colour for
// "running" or "done" when the palette has one, chrome otherwise - which is
// how the bar drew before Status carried colour when the pane is active.
// This is the renderPaneBar sibling adr-0000020's comment promised and the
// stack.go history above never built; it is built here, as a configurable
// pair of colours apart from the plain Color on/off toggle rather than as the
// fixed colour the ADR assumed.
func paneBarStyle(p Palette, chrome tcell.Style, state string) tcell.Style {
	if style, ok := p.PaneStyle(state); ok {
		return style
	}
	return chrome
}

// renderPaneBar builds the pane bar's text, truncated to width with an ellipsis rather
// than scrolled or folded. adr-0000020 itself describes a silent truncation with no
// mark that more exist past the edge; Glen asked to keep the ellipsis instead
// (2026-10-06), so this bar truncates the way every other row in this file already does
// (see cutTail's callers) rather than matching 0000020's text literally - a deliberate,
// confirmed departure from that record, not an oversight.
//
// The interface loop supplies the focused session's identity; the renderer has no
// separate pane selection to reconcile.
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

// drawBarOne draws bar one at row y, with the figure segment swept while a turn is
// running. Factored out of Draw's main path so the shedding ladder below can draw
// the same row at whatever height it survives to, rather than repeating the sweep
// arithmetic twice.
//
// The figure reads "idle" (literally [StateIdle]'s own value - the one word
// [twiddleWord] never returns) whenever nothing is running, including through a
// screen break, and that word is drawn in chrome like the rest of the bar rather
// than swept: a sweep with nothing turning behind it is a colour with no figure to
// explain it, and the reader is left wondering why one word in an otherwise plain
// line is still lit.
func (f *Frame) drawBarOne(screen tcell.Screen, x, y, width int, chrome tcell.Style) {
	prefix, figure, suffix := renderBarOne(f.bar.Status)
	drawCellText(screen, x, y, width, prefix, chrome)
	figureX := DisplayWidth(prefix)
	if figure == string(StateIdle) {
		drawCellText(screen, x+figureX, y, width-figureX, figure, chrome)
	} else {
		drawSweepText(screen, x+figureX, y, width-figureX, figure, f.step, f.palette)
	}
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

// renderBarTwo builds the confirmed status field sequence as a plain string, which
// also gives tests a stable way to check the ordering independently of screen width.
func renderBarTwo(status Status) string {
	parts := make([]string, 0, len(barTwoFields))
	for _, k := range barTwoFields {
		parts = append(parts, fieldName(k)+":"+status[k])
	}
	return strings.Join(parts, " · ")
}

// drawBarTwo lays the requested fields out from both edges. When the two halves
// meet, the left half fades toward the collision point before the right half is
// drawn over it, making the overlap visible without changing field order.
func (f *Frame) drawBarTwo(screen tcell.Screen, x, y, width int, chrome tcell.Style) {
	leftFields := []int{fieldHost, fieldCredits, fieldCost, fieldContext, fieldInput}
	rightFields := []int{fieldOutput, fieldAutosave, fieldStealth, fieldApproval}
	left := statusFieldsText(f.bar.Status, leftFields)
	right := statusFieldsText(f.bar.Status, rightFields)
	lw := DisplayWidth(left)
	// The two runs each own one half of the row. The right run starts at the
	// center even when its full text would not fit, so an overlong run can
	// collide visibly instead of silently erasing the entire left side.
	rightStart := width / 2
	drawCellText(screen, x, y, width, left, chrome)
	collision := lw - rightStart
	if collision > 0 && rightStart > 0 {
		fade := collision
		if fade > rightStart {
			fade = rightStart
		}
		start := rightStart - fade
		segment := sliceDisplayWidth(left, start, fade)
		drawFadedText(screen, x+start, y, fade, segment, chrome, f.palette.Style(RoleDim))
	}
	drawCellText(screen, x+rightStart, y, width-rightStart, right, chrome)
}

func statusFieldsText(status Status, fields []int) string {
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		value := status[field]
		if value == "" {
			value = "unavailable"
		}
		parts = append(parts, fieldName(field)+":"+value)
	}
	return strings.Join(parts, " · ")
}

func sliceDisplayWidth(text string, start, width int) string {
	var b strings.Builder
	col := 0
	for _, r := range text {
		rw := runewidth(r)
		if col >= start && col+rw <= start+width {
			b.WriteRune(r)
		}
		col += rw
	}
	return b.String()
}

func drawFadedText(screen tcell.Screen, x, y, width int, text string, from, to tcell.Style) {
	fromFG, bg, attrs := from.Decompose()
	toFG, _, _ := to.Decompose()
	fr, fg, fb := fromFG.RGB()
	tr, tg, tb := toFG.RGB()
	if fr < 0 || tr < 0 {
		if fromFG == tcell.ColorDefault || toFG == tcell.ColorDefault {
			to = from.Dim(true)
		}
		drawCellText(screen, x, y, width, text, to)
		return
	}
	col := 0
	for _, r := range text {
		if col >= width {
			break
		}
		rw := runewidth(r)
		if rw < 1 {
			continue
		}
		if col+rw > width {
			break
		}
		progress := float64(col) / float64(max(1, width-1))
		blend := func(a, b int32) int32 { return a + int32(float64(b-a)*progress) }
		style := tcell.StyleDefault.Foreground(tcell.NewRGBColor(blend(fr, tr), blend(fg, tg), blend(fb, tb))).Background(bg).Attributes(attrs)
		screen.SetContent(x+col, y, r, nil, style)
		col += rw
	}
}

// foldRowLines splits row into the physical lines a draw turns it into: one
// per line break row.Text itself carries, the way this already worked, and
// then, within each of those, one more split for any stretch still wider
// than width once word-wrapped. This is the "folded when they are drawn"
// promise [Row]'s own doc comment makes: a row is held and copied whole,
// with every line break the model wrote intact, and is only turned into
// more than one screen line here, at the one place that already knows how
// tall and wide the terminal's scrollback is.
//
// width is the draw width this fold's caller already measured for the
// scrollback column (Draw's own width, the same one every row is about to
// be drawn into) - a row folded to some other width would wrap at a
// boundary the screen it is about to be drawn on does not have. width <= 0
// skips the word-wrap pass entirely and returns the newline-only fold, which
// is what every caller before this pass existed already got; it exists so a
// caller with no real width in hand - a test exercising only the newline
// fold, say - is not forced to invent one.
//
// A row with no line break and no need to wrap returns a single-element
// slice holding row unchanged, so the common case costs nothing extra.
//
// Spans are byte ranges into the whole of row.Text, so each line's own spans
// are cut to that line's range and rebased to start at zero at both levels
// of the split, the same rebaseSpans rule cutTail's own callers already
// apply when a line is cut rather than folded.
func foldRowLines(row Row, width int) []Row {
	var physical []Row
	if !strings.Contains(row.Text, "\n") {
		physical = []Row{row}
	} else {
		lines := strings.Split(row.Text, "\n")
		physical = make([]Row, len(lines))
		offset := 0
		for i, text := range lines {
			start, end := offset, offset+len(text)
			physical[i] = Row{Text: text, Spans: rebaseSpans(row.Spans, start, end), Kind: row.Kind, Level: row.Level}
			offset = end + 1 // +1 skips the '\n' this line was split on.
		}
	}

	if width <= 0 {
		return physical
	}

	out := make([]Row, 0, len(physical))
	for _, pr := range physical {
		out = append(out, wrapRowLine(pr, width)...)
	}
	return out
}

// rebaseSpans is the one place a span is cut to a sub-range of the text it
// was measured against and rebased to start at zero within it - the rule
// both levels of foldRowLines's split apply, and the rule factored out here
// rather than written twice so a fix to it is a fix in one place rather
// than two that can drift apart.
func rebaseSpans(spans []Span, start, end int) []Span {
	var out []Span
	for _, sp := range spans {
		if sp.End <= start || sp.Start >= end {
			continue
		}
		s, e := max(sp.Start, start), min(sp.End, end)
		out = append(out, Span{Start: s - start, End: e - start, Role: sp.Role})
	}
	return out
}

// wrapRowLine splits one already-newline-free row into further rows if its
// own display width is over width, breaking only at a run of one or more
// spaces so a word is never broken mid-word. A row already within width is
// returned as the single element of a one-element slice, which costs
// nothing beyond the slice itself and is the common case for most replies.
//
// A row that is entirely one RoleCode span - a line inside a fenced code
// block, or a line that is nothing but a single inline code run - is left
// unwrapped, full stop, on purpose: reflowing code changes what it says.
// Breaking a line of Go, say, at whatever column the terminal happens to be
// does not produce two shorter lines of the same code, it produces a line
// that no longer parses and a continuation that looks like a second
// statement. Every reader of code this codebase already defers to - a
// terminal's own `less`, a browser's `<pre>`, this package's own cutTail -
// handles an overlong line by scrolling or truncating it horizontally
// rather than by reflowing it, and cutTail already does exactly that for
// any row too wide for the terminal regardless of role. So a whole-line
// code row is left for cutTail to truncate with its ellipsis, the same as
// before this pass existed, rather than wrapped here.
//
// Prose mixed with a smaller run of inline code - "run `go test ./...`
// before you push", say - is a different shape: the sentence around the
// code is still prose a reader scans left to right, and the fix for an
// overlong sentence is the fix for any overlong sentence, word-wrap. Only a
// row that is nothing but code, where wrapping would cut into the code
// itself with no prose on either side to break at instead, is withheld
// from this.
func wrapRowLine(row Row, width int) []Row {
	if DisplayWidth(row.Text) <= width || isWholeLineCode(row) {
		return []Row{row}
	}

	var out []Row
	text := row.Text
	offset := 0
	for {
		cut := wrapCut(text, width)
		if cut >= len(text) {
			out = append(out, Row{Text: text, Spans: rebaseSpans(row.Spans, offset, offset+len(text)), Kind: row.Kind, Level: row.Level})
			break
		}

		piece := text[:cut]
		out = append(out, Row{Text: piece, Spans: rebaseSpans(row.Spans, offset, offset+len(piece)), Kind: row.Kind, Level: row.Level})

		rest := text[cut:]
		trimmed := strings.TrimLeft(rest, " ")
		offset += cut + (len(rest) - len(trimmed))
		text = trimmed
		if text == "" {
			break
		}
	}
	return out
}

// isWholeLineCode reports whether row is entirely one RoleCode span - the
// shape ParseMarkdown gives a line inside a fenced code block, or a line
// that opens and closes with its own inline code run and nothing else - the
// one case wrapRowLine leaves unwrapped. A line that merely contains some
// inline code alongside plain prose has more than this one span, or a span
// that does not cover the whole line, and is word-wrapped like any other
// line of prose.
func isWholeLineCode(row Row) bool {
	if len(row.Spans) != 1 {
		return false
	}
	sp := row.Spans[0]
	return sp.Role == RoleCode && sp.Start == 0 && sp.End == len(row.Text)
}

// wrapCut finds where wrapRowLine should cut text for one more line within
// width columns: the byte offset of the space run nearest the width
// boundary, so the cut falls at a word break rather than mid-word. text
// itself, not yet cut, is returned via len(text) when text already fits.
//
// A single "word" wider than width on its own - a long URL with no spaces,
// say - has no space to break at within budget. Rather than loop forever
// offering the same unbroken word again, this falls back to CutColumn's own
// rune-safe cut at the width boundary, which is the one case this still
// breaks mid-word: there being no other option left, a broken word reads
// better than a line that never ends or a line that silently overruns the
// width this exists to respect.
func wrapCut(text string, width int) int {
	if DisplayWidth(text) <= width {
		return len(text)
	}

	fit, _ := CutColumn(text, width)
	cut := len(fit)
	if sp := strings.LastIndexByte(fit, ' '); sp >= 0 {
		return sp
	}
	if cut == 0 {
		// width itself cannot hold even one rune (e.g. a wide rune in a
		// one-column budget); advance by one rune's worth of bytes so the
		// caller always makes progress.
		if r, size := utf8.DecodeRuneInString(text); size > 0 {
			_ = r
			return size
		}
		return 1
	}
	return cut
}

// drawSweepText draws the twiddle, each column coloured from sweepColors for the
// palette's own ground, and scrolling left to right: the colour at column c on step
// is the one that sat at column c-1 on step-1, so a colour travels rightward one
// column per step rather than every column turning in place together.
func drawSweepText(screen tcell.Screen, x, y, width int, text string, step int, p Palette) {
	colors := sweepColors(p.Ground())
	n := len(colors)
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
		c := colors[(((col-step)%n)+n)%n]
		style := p.BaseStyle().Foreground(tcell.NewRGBColor(int32(c.R), int32(c.G), int32(c.B)))
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
