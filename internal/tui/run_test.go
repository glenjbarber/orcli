package tui

import (
	"bytes"
	"strings"
	"testing"
)

// drawnAt builds a Screen writing to a buffer at a chosen size.
func drawnAt(rows, cols int) (*Screen, *bytes.Buffer) {
	var b bytes.Buffer
	return NewScreen(&b, WindowSize{Rows: rows, Cols: cols}), &b
}

// plainPalette builds a palette with colour off, which is the default and the case every
// assertion about text is made against.
func plainPalette() Palette { return NewPalette(false, GroundDark, nil) }

// TestRowsAreWrittenInOrder is the property the log exists for. A reader comparing two
// tool results is comparing their order, so a draw that reorders them misreports what
// happened.
func TestRowsAreWrittenInOrder(t *testing.T) {
	screen, out := drawnAt(20, 80)
	rows := []Row{
		{Text: "the question"},
		{Text: "the answer"},
		{Text: "the tool ran"},
	}

	DrawLog(screen, rows, plainPalette())

	got := out.String()
	first := strings.Index(got, "the question")
	second := strings.Index(got, "the answer")
	third := strings.Index(got, "the tool ran")

	if first < 0 || second < 0 || third < 0 {
		t.Fatalf("a row is missing from the output:\n%s", got)
	}
	if !(first < second && second < third) {
		t.Errorf("rows are out of order:\n%s", got)
	}
}

// TestEachRowIsOneLine is the other half of the same property. A row that wrapped would
// leave the terminal holding more lines than the log has rows, and the log and the screen
// would stop agreeing about what was said.
func TestEachRowIsOneLine(t *testing.T) {
	screen, out := drawnAt(20, 80)
	DrawLog(screen, []Row{{Text: "one"}, {Text: "two"}, {Text: "three"}}, plainPalette())

	if got, want := strings.Count(out.String(), "\r\n"), 3; got != want {
		t.Errorf("the draw wrote %d line endings, want %d", got, want)
	}
}

// TestAnEscapeDoesNotReachTheTerminal is the property that keeps a model from steering
// the terminal through its own reply. The row is filtered before it is written, so what
// arrives is the words and nothing else.
func TestAnEscapeDoesNotReachTheTerminal(t *testing.T) {
	screen, out := drawnAt(20, 80)
	DrawLog(screen, []Row{{Text: "before\x1b[2J\x1b[1;1Hafter"}}, plainPalette())

	got := out.String()
	if strings.ContainsRune(got, 0x1b) {
		t.Errorf("an escape reached the terminal: %q", got)
	}
	if !strings.Contains(got, "beforeafter") {
		t.Errorf("the words did not survive: %q", got)
	}
}

// TestAClearScreenArrivesAsNothing is the same property with the case that catches a
// filter dropping only the escape byte. A clear-screen that lost only its ESC arrives at
// the reader as the letters 2J, which is visible nonsense rather than nothing.
func TestAClearScreenArrivesAsNothing(t *testing.T) {
	screen, out := drawnAt(20, 80)
	DrawLog(screen, []Row{{Text: "\x1b[2J"}}, plainPalette())

	if got := out.String(); strings.Contains(got, "2J") {
		t.Errorf("the parameter bytes survived the filter: %q", got)
	}
}

// TestARowWiderThanTheTerminalIsCut covers the reason the width measurement exists. A byte
// count reports a box-drawing rule three times too wide, and a row allowed to wrap breaks
// the one-row-per-line contract everything else assumes.
func TestARowWiderThanTheTerminalIsCut(t *testing.T) {
	screen, out := drawnAt(20, 10)
	DrawLog(screen, []Row{{Text: "a row that is much longer than the terminal is"}}, plainPalette())

	line, _, _ := strings.Cut(out.String(), "\r\n")
	if got := DisplayWidth(line); got > 10 {
		t.Errorf("the row is %d columns on a ten column terminal", got)
	}
}

// TestTheEndOfARowIsKept covers which half survives a cut. The marker leads a row and the
// end is what is being composed, so a long line keeps its tail rather than its head.
func TestTheEndOfARowIsKept(t *testing.T) {
	screen, out := drawnAt(20, 12)
	DrawLog(screen, []Row{{Text: "the beginning and the end"}}, plainPalette())

	got := out.String()
	if !strings.Contains(got, "the end") {
		t.Errorf("the tail of the row was cut rather than the head: %q", got)
	}
	if strings.Contains(got, "the beginning") {
		t.Errorf("the head survived a cut that should have kept the tail: %q", got)
	}
}

// TestBoxDrawingIsOneColumn covers the specific miscount the width code exists to avoid.
// The figure is three bytes and one column, and a byte count reports it three times too
// wide.
func TestBoxDrawingIsOneColumn(t *testing.T) {
	if got := DisplayWidth(rule); got != 1 {
		t.Errorf("a rule is %d columns, want 1", got)
	}
}

// TestColourOffWritesNoSequence covers the case a reader with colour turned off is in. The
// palette already returns nothing, so this asserts the draw asks it rather than building
// sequences of its own.
func TestColourOffWritesNoSequence(t *testing.T) {
	screen, out := drawnAt(20, 80)
	DrawLog(screen, []Row{{
		Text:  "a coloured row",
		Spans: []Span{{Start: 0, End: 8, Role: RoleHeading}},
	}}, plainPalette())

	if got := out.String(); strings.ContainsRune(got, 0x1b) {
		t.Errorf("colour was written while the palette is off: %q", got)
	}
}

// TestASpanIsWrittenInItsRole covers the other half: with colour on, the role decides the
// sequence and the words are unchanged. A row that said something different with colour on
// than off would be a row whose text depends on the reader's terminal.
//
// The assertion strips the sequences before comparing the words, since a span in the
// middle of a row puts a sequence between two words that are adjacent in the plain text.
func TestASpanIsWrittenInItsRole(t *testing.T) {
	screen, out := drawnAt(20, 80)
	palette := NewPalette(true, GroundDark, nil)

	DrawLog(screen, []Row{{
		Text:  "a heading",
		Spans: []Span{{Start: 2, End: 9, Role: RoleHeading}},
	}}, palette)

	got := out.String()
	if words := stripSequences(got); !strings.Contains(words, "a heading") {
		t.Errorf("the words did not survive the colouring: %q", words)
	}
	if !strings.Contains(got, palette.Sequence(RoleHeading)) {
		t.Errorf("the role's sequence was not written: %q", got)
	}
}

// TestTheTextAfterTheLastSpanIsWritten covers the tail of a row whose spans stop short of
// its end. A row that dropped the words after the last span would be a row losing text
// because it was coloured, which is the one thing the spans must never do.
func TestTheTextAfterTheLastSpanIsWritten(t *testing.T) {
	screen, out := drawnAt(20, 80)
	palette := NewPalette(true, GroundDark, nil)

	DrawLog(screen, []Row{{
		Text:  "a heading and a tail",
		Spans: []Span{{Start: 0, End: 8, Role: RoleHeading}},
	}}, palette)
	if words := stripSequences(out.String()); !strings.Contains(words, "a heading and a tail") {
		t.Errorf("the text did not survive intact: %q", words)
	}
}

// TestASpanInTheMiddleKeepsTheWholeRow covers the case the fold has to get right. A row
// whose spans cover only part of it must arrive with every word, since a hole in the
// middle of a reply is text the reader never saw and a copy never carried.
func TestASpanInTheMiddleKeepsTheWholeRow(t *testing.T) {
	palette := NewPalette(true, GroundDark, nil)
	row := Row{
		Text: "the quick brown fox",
		Spans: []Span{
			{Start: 4, End: 9, Role: RoleEmphasis},
			{Start: 14, End: 17, Role: RoleCode},
		},
	}

	if got, want := stripSequences(RowText(palette, row, 80)), "the quick brown fox"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestAThemeIsWrittenAsTheBase covers the reset as much as the base. A bare reset alone
// would clear the theme and leave the rest of the row in the terminal's colours rather
// than on the theme's background.
func TestAThemeIsWrittenAsTheBase(t *testing.T) {
	screen, out := drawnAt(20, 80)
	theme, err := NewTheme("test", "#ffffff", "#000000")
	if err != nil {
		t.Fatalf("NewTheme: %v", err)
	}
	palette := NewPalette(true, GroundDark, &theme)

	DrawLog(screen, []Row{{
		Text:  "a themed row",
		Spans: []Span{{Start: 0, End: 1, Role: RoleChrome}},
	}}, palette)

	got := out.String()
	if !strings.Contains(got, palette.Base()) {
		t.Errorf("the base was not written before the row: %q", got)
	}
	if !strings.Contains(got, palette.Reset()) {
		t.Errorf("the row does not end with the reset: %q", got)
	}
}

// TestASpanPastTheCutIsDropped covers the clamp. A row cut from the tail has fewer bytes
// than the span was measured against, and a span running past the end would write past
// what is on the screen.
func TestASpanPastTheCutIsDropped(t *testing.T) {
	palette := NewPalette(true, GroundDark, nil)
	row := Row{
		Text:  "a row longer than the width",
		Spans: []Span{{Start: 10, End: 40, Role: RoleHeading}},
	}

	got := RowText(palette, row, 6)
	if DisplayWidth(stripSequences(got)) > 6 {
		t.Errorf("the row is wider than the cut after the spans were written: %q", got)
	}
}

// TestTheStackIsNineRows is the layout the reader approved, from the bottom of the screen
// upward: a blank row, a separator, the bottom bar, the top bar, a separator, the field,
// the twiddle, the active row, and the task row.
//
// There is no prompt. The field carries only what has been typed, and it and the twiddle
// sit at column five.
func TestTheStackIsNineRows(t *testing.T) {
	screen, out := drawnAt(24, 80)

	DrawStack(screen, Bar{
		Top:    "Status: idle",
		Bottom: "Provider: openrouter.ai",
		Field:  "please read the new preferences in foo.md",
	}, plainPalette())

	lines := stackRowsOnly(out.String())
	if got, want := len(lines), 9; got != want {
		t.Fatalf("the stack is %d rows, want %d:\n%s", got, want, out.String())
	}

	for i, want := range []struct {
		name   string
		has    string
		blank  bool
		isRule bool
	}{
		{name: "the bottom row", blank: true},
		{name: "the second row from the bottom", isRule: true},
		{name: "the bottom bar", has: "Provider:"},
		{name: "the top bar", has: "Status:"},
		{name: "the fifth row from the bottom", isRule: true},
		{name: "the field", has: "please read the new preferences"},
		{name: "the twiddle row", blank: true},
		{name: "the active row", blank: true},
		{name: "the task row", blank: true},
	} {
		line := lines[i]
		switch {
		case want.blank && line != "":
			t.Errorf("%s is %q, want it blank", want.name, line)
		case want.isRule && line != rule:
			t.Errorf("%s is %q, want a separator", want.name, line)
		case want.has != "" && !strings.Contains(line, want.has):
			t.Errorf("%s does not carry %q: %q", want.name, want.has, line)
		}
	}
}

// TestTheFieldSitsAtColumnFive covers the indentation. A reader's eye follows the field
// and the figure without jumping between rows, and both have the same left edge for it.
func TestTheFieldSitsAtColumnFive(t *testing.T) {
	screen, out := drawnAt(24, 80)
	DrawStack(screen, Bar{
		Field:   "the field",
		Twiddle: "thinking",
	}, plainPalette())

	for _, line := range stackRowsOnly(out.String()) {
		if !strings.Contains(line, "the field") && !strings.Contains(line, "thinking") {
			continue
		}
		if !strings.HasPrefix(line, strings.Repeat(" ", FieldIndent)) {
			t.Errorf("the row is not indented to column five: %q", line)
		}
	}
}

// TestThePromptIsGone covers the removal. There was a `root@localhost $ ` at the head of
// the field, and a reader at a known place on the bottom row does not need to be told
// which account they are.
func TestThePromptIsGone(t *testing.T) {
	if Prompt != "" {
		t.Errorf("Prompt is %q, want it empty", Prompt)
	}

	screen, out := drawnAt(24, 80)
	DrawStack(screen, Bar{Field: "a question"}, plainPalette())

	if got := out.String(); strings.Contains(got, "root@localhost") {
		t.Errorf("the prompt is still in the frame:\n%s", got)
	}
}

// TestTheStackDoesNotChangeHeight covers the reason all nine rows are drawn whether or not
// anything is running. A footer that grows when a turn starts is a footer that moves the
// field under the reader's hands.
func TestTheStackDoesNotChangeHeight(t *testing.T) {
	quiet, out := drawnAt(24, 80)
	DrawStack(quiet, Bar{
		Bottom: "Provider: openrouter.ai",
		Field:  "a question",
	}, plainPalette())
	idle := len(stackRowsOnly(out.String()))

	busy, other := drawnAt(24, 80)
	DrawStack(busy, Bar{
		Bottom:  "Provider: openrouter.ai",
		Field:   "a question",
		Twiddle: "⠋  thinking",
		Active:  "active: working",
		Tasks:   "tasks: 2 running",
	}, plainPalette())
	running := len(stackRowsOnly(other.String()))

	if idle != running {
		t.Errorf("the stack is %d rows when idle and %d while running, want the same",
			idle, running)
	}
}

// TestTheScrollRegionEndsAboveTheStack is the piece with no precedent in the tree. The
// region has to end where the stack begins, or the first row written scrolls the field
// off the screen.
//
// Forty rows with a nine row stack leaves thirty-one for the log.
func TestTheScrollRegionEndsAboveTheStack(t *testing.T) {
	screen, out := drawnAt(40, 80)
	screen.SetScrollRegion(logRows(40))

	if got, want := out.String(), "\x1b[1;31r"; got != want {
		t.Errorf("the scroll region is %q, want %q", got, want)
	}
}

// TestTheScrollRegionFollowsAResize covers the recomputation. A region left at the old
// size clips the log to a height the screen no longer has, which is a log that stops
// growing with no way to see why.
func TestTheScrollRegionFollowsAResize(t *testing.T) {
	screen, out := drawnAt(40, 80)
	screen.SetSize(WindowSize{Rows: 20, Cols: 80})

	if got, want := out.String(), "\x1b[1;11r"; !strings.HasSuffix(got, want) {
		t.Errorf("after a resize the region is %q, want it to end with %q", got, want)
	}
}

// TestAShortTerminalKeepsTheField covers the one thing the ladder has to guarantee. The
// field is held to the end; everything above it goes first.
func TestAShortTerminalKeepsTheField(t *testing.T) {
	screen, out := drawnAt(3, 80)
	DrawStack(screen, Bar{Bottom: "Provider: -", Field: "a question"}, plainPalette())

	got := out.String()
	if !strings.Contains(got, "a question") {
		t.Errorf("a three row terminal lost the field: %q", got)
	}
	if strings.Contains(got, "Provider:") {
		t.Errorf("a three row terminal kept a bar it had no room for: %q", got)
	}
}

// TestTheTwiddleMovesInPlace covers the one in-place redraw in the frame. It has to move
// the cursor up and clear its row, since two braille cells do not cover a row and a figure
// drawn over the last one leaves a smear rather than a turn.
func TestTheTwiddleMovesInPlace(t *testing.T) {
	screen, out := drawnAt(24, 80)
	DrawTwiddle(screen, plainPalette(), 0, "thinking")

	got := out.String()
	if !strings.Contains(got, escapeMoveUp) {
		t.Errorf("the twiddle did not move the cursor: %q", got)
	}
	if !strings.Contains(got, escapeEraseLine) {
		t.Errorf("the twiddle did not clear its row: %q", got)
	}
	if !strings.Contains(got, Twiddle(0)) {
		t.Errorf("the figure was not written: %q", got)
	}
}

// TestTheTwiddleCarriesTheStateWord covers the second half of the twiddle row. The word is
// a visual echo of the Status field on the top bar, so a reader watching the figure knows
// what it is reporting without a second thing to learn.
func TestTheTwiddleCarriesTheStateWord(t *testing.T) {
	screen, out := drawnAt(24, 80)
	DrawTwiddle(screen, plainPalette(), 0, "thinking")

	if got := out.String(); !strings.Contains(got, "  thinking") {
		t.Errorf("the state word is not beside the figure: %q", got)
	}
}

// TestTheTwiddleWritesNothingWhenIdle covers the blank row. A figure that is not moving is
// a figure a reader has to learn to ignore, so nothing is written at all rather than a
// stopped figure.
func TestTheTwiddleWritesNothingWhenIdle(t *testing.T) {
	screen, out := drawnAt(24, 80)
	DrawTwiddle(screen, plainPalette(), 0, "")

	if got := out.String(); got != "" {
		t.Errorf("an idle twiddle wrote %q, want nothing", got)
	}
}

// TestTheTwiddleIsTwoCellsWithOppositeHues covers the decision that makes two cells read as
// one figure rather than two spinners. The cells are half a period apart and the hue
// follows the character index, so the two are always opposite hues.
func TestTheTwiddleIsTwoCellsWithOppositeHues(t *testing.T) {
	for step := range 10 {
		figure := Twiddle(step)
		if got := len([]rune(figure)); got != 2 {
			t.Errorf("step %d drew %d cells, want 2", step, got)
		}
		if TwiddleHue(step) == TwiddleHue(step+5) {
			t.Errorf("step %d and step %d share a hue, want them opposite", step, step+5)
		}
	}
}

// TestARunRefusesWhatItCannotDraw covers the two refusals. A silent draw writes nothing and
// a reader concludes the program is idle, so both are named.
func TestARunRefusesWhatItCannotDraw(t *testing.T) {
	if err := Run(nil, NewScreen(&bytes.Buffer{}, WindowSize{Rows: 24, Cols: 80})); err == nil {
		t.Error("Run drew with no session, want a refusal")
	}
	if err := Run(New(Options{Model: "m"}), nil); err == nil {
		t.Error("Run drew with no terminal, want a refusal")
	}
}

// TestARunWritesTheLogAndTheStack covers the ordinary path end to end, since Run is the one
// call a program makes and nothing else exercises it.
func TestARunWritesTheLogAndTheStack(t *testing.T) {
	screen, out := drawnAt(40, 80)
	s := New(Options{Model: "stealth/space-bunny-alpha", Provider: "openrouter.ai"})

	if err := Run(s, screen); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "orcli") {
		t.Errorf("the log was not written:\n%s", got)
	}
	if !strings.Contains(got, "openrouter.ai") {
		t.Errorf("the bottom bar is missing:\n%s", got)
	}
	if !strings.Contains(got, string(StateIdle)) {
		t.Errorf("the Status field is missing the state:\n%s", got)
	}
	if !strings.Contains(got, "\x1b[1;31r") {
		t.Errorf("the scroll region was not set:\n%s", got)
	}
}

// TestARedirectedRunWritesWords covers what a reader who pipes this gets. Escape sequences
// into a pipe are noise rather than a transcript, so the redirected path writes the rows
// and nothing else.
func TestARedirectedRunWritesWords(t *testing.T) {
	var out bytes.Buffer
	rows := []Row{{Text: "a row"}, {Text: "another row"}}

	if err := WriteLines(&out, rows); err != nil {
		t.Fatalf("WriteLines: %v", err)
	}

	got := out.String()
	if strings.ContainsRune(got, 0x1b) {
		t.Errorf("an escape was written into a redirect: %q", got)
	}
	if want := "a row\nanother row\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestTheBottomBarCarriesIdentityAndApproval covers the field order the reader settled. The
// bottom bar is the one nearer the bottom of the screen. Session names which session the
// field is typed into, the two bracketed words name the ways the terminal is taking input,
// and the settings follow: Provider, Model, Approval, Verbosity.
//
// Approval is last of the settings because Approval says whether programs run without
// asking, which is the last thing a reader should lose. Verbosity goes first.
func TestTheBottomBarCarriesIdentityAndApproval(t *testing.T) {
	got := RenderBottom("Session 1", true, false,
		"openrouter.ai", "stealth/space-bunny-alpha", "3", "ask")

	for _, want := range []string{
		"Session 1",
		"[Mouse]",
		"Provider: openrouter.ai",
		"Model: stealth/space-bunny-alpha",
		"Approval: ask",
		"Verbosity: 3",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the bottom bar does not carry %q: %q", want, got)
		}
	}

	// The bracketed words are not names and values. `Mouse: on` reads as a preference a
	// reader could be trying to set and `[Mouse]` reads as a thing happening now.
	if strings.Contains(got, "Mouse:") {
		t.Errorf("the mouse is a name and value rather than a bracketed word: %q", got)
	}

	if strings.Index(got, "Approval") > strings.Index(got, "Verbosity") {
		t.Errorf("Approval is not ahead of Verbosity: %q", got)
	}
}

// TestTheBottomBarLeavesOutWhatIsOff covers the two bracketed words again. A bar carrying
// [Mouse] while the mouse is off would be naming something that is not happening.
func TestTheBottomBarLeavesOutWhatIsOff(t *testing.T) {
	got := RenderBottom("Session 1", false, false,
		"openrouter.ai", "stealth/space-bunny-alpha", "3", "ask")

	for _, unwanted := range []string{"[Mouse]", "[Copy]"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("the bottom bar carries %q while it is off: %q", unwanted, got)
		}
	}
}

// TestTheTopBarCarriesTheChangingFields covers the other bar. It is nearer the log and
// carries what changes second by second, in the order Status, Reasoning, Context, In, Out,
// Cost, Credits.
func TestTheTopBarCarriesTheChangingFields(t *testing.T) {
	got := RenderTop("working", "3", "18,400", "2,104", "916", "$0.0181", "$4.82")

	for _, want := range []string{
		"Status: working",
		"Reasoning: 3",
		"Context: 18,400",
		"In: 2,104",
		"Out: 916",
		"Cost: $0.0181",
		"Credits: $4.82",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the top bar does not carry %q: %q", want, got)
		}
	}
	if strings.Index(got, "Status") > strings.Index(got, "Reasoning") {
		t.Errorf("Status is not the first field on the top bar: %q", got)
	}
}

// TestTheTopBarKeepsStatusLongest covers the reason the order is what it is. The fields
// are dropped from the right, so the one field that changes second by second is the last
// to go on the bar that changes most.
func TestTheTopBarKeepsStatusLongest(t *testing.T) {
	// A width that fits Status and nothing else.
	narrow := RenderBar([]Field{
		{Name: "Status", Value: "working"},
		{Name: "Reasoning", Value: "3"},
	}, 20)
	if !strings.Contains(narrow, "Status") {
		t.Errorf("a narrow top bar lost Status: %q", narrow)
	}
	if strings.Contains(narrow, "Reasoning") {
		t.Errorf("a narrow top bar kept a field past Status: %q", narrow)
	}
}

// TestAFieldWithNoValueIsADash covers the reporting rule. A bar that shifts shape as values
// arrive is a bar no reader can scan.
func TestAFieldWithNoValueIsADash(t *testing.T) {
	got := RenderBottom("Session 1", false, false, "", "", "", "")

	for _, want := range []string{
		"Provider: -", "Model: -", "Approval: -", "Verbosity: -",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the bar does not carry %q: %q", want, got)
		}
	}
}

// TestTheStatusCarriesANoteRatherThanAFifthField covers the decision that put paused in
// Status. A bar that grows a comma is a bar no reader can scan.
func TestTheStatusCarriesANoteRatherThanAFifthField(t *testing.T) {
	got := RenderTop("paused (buffered 12 rows)", "", "", "", "", "", "")

	if !strings.Contains(got, "Status: paused (buffered 12 rows)") {
		t.Errorf("the note is not beside the state: %q", got)
	}
	if strings.Contains(got, "Note:") {
		t.Errorf("the note became a field of its own: %q", got)
	}
}

// TestABarDropsWholeFields covers the narrow terminal. A bar reading Cred | Con is better
// than one cut mid-word, since a cut field looks like a value the reader mistyped.
func TestABarDropsWholeFields(t *testing.T) {
	fields := []Field{
		{Name: "Credits", Value: "10"},
		{Name: "Cost", Value: "1"},
		{Name: "Context", Value: "20%"},
	}

	got := RenderBar(fields, 24)
	if strings.Contains(got, "Cre") && !strings.Contains(got, "Credits") {
		t.Errorf("a field was cut rather than dropped whole: %q", got)
	}
	if strings.Count(got, "|") != 1 {
		t.Errorf("got %q, want two fields to have survived in 24 columns", got)
	}
}

// TestABarWithNoWidthIsNotCut covers the caller that has not asked the terminal yet. A bar
// truncated to a width nobody asked for is a bar missing fields for no reason.
func TestABarWithNoWidthIsNotCut(t *testing.T) {
	got := RenderBar([]Field{{Name: "Credits", Value: "10"}, {Name: "Host", Value: "localhost"}}, 0)

	if !strings.Contains(got, "Host") {
		t.Errorf("a bar with no width lost a field: %q", got)
	}
}

// stripSequences removes the escapes from a rendered row, for measuring its width and
// comparing its words.
//
// A span in the middle of a row puts a sequence between two words that are adjacent in the
// plain text, so a test that compared the rendered bytes against the plain phrase would
// fail against a row that is drawn correctly.
func stripSequences(s string) string {
	var b strings.Builder
	runes := []rune(s)

	for i := 0; i < len(runes); i++ {
		if runes[i] == 0x1b {
			i += escapeLength(runes[i+1:])
			continue
		}
		b.WriteRune(runes[i])
	}
	return b.String()
}

// stackRowsOnly splits what DrawStack wrote into its rows, with the sequences removed.
//
// It is here so an assertion about the layout can name a row by where it sits rather than
// by searching the whole output for a string that might appear on any of them.
func stackRowsOnly(s string) []string {
	stripped := stripSequences(s)
	parts := strings.Split(stripped, "\r\n")

	// The split leaves a trailing empty element for the final line ending.
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}
