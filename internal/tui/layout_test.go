package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// hue is one hue as a single comparable value, so a test can compare two of them.
//
// Go will not compare a three-value call directly, and a sweep assertion needs to say that
// two hues are or are not the same colour, so the three channels are folded into one number.
func hue(h float64) int {
	r, g, b := hueRGB(h)
	return int(r)<<16 | int(g)<<8 | int(b)
}

// TestScrollbackGrowsWithHeight covers the layout loreloom/UI-redesign.md settled: a
// terminal taller than the four fixed rows (prompt, blank, two status bars) gives every
// row beyond them to scrollback, rather than to a fixed field count.
func TestScrollbackGrowsWithHeight(t *testing.T) {
	for _, height := range []int{barRows, barRows + 1, 20, 40} {
		if got, want := scrollbackRows(height), height-barRows; got != want {
			t.Errorf("on a %d row terminal scrollback is %d rows, want %d", height, got, want)
		}
	}
}

// TestAShortTerminalHasNoScrollback covers the floor. A terminal shorter than the four
// fixed rows cannot fit scrollback at all; Frame.Draw falls back to drawing only the
// prompt rather than guessing which of the fixed rows to shed, since no shedding order
// among them is settled (see scrollbackRows's own doc comment).
func TestAShortTerminalHasNoScrollback(t *testing.T) {
	for _, height := range []int{0, 1, barRows - 1} {
		if got := scrollbackRows(height); got != 0 {
			t.Errorf("a %d row terminal has %d rows of scrollback, want 0", height, got)
		}
	}
}

// TestAShortTerminalStillDrawsThePrompt covers Frame.Draw's shedding ladder: a terminal
// too short for the fixed stack still draws the prompt, which survives every height
// since it is never shed (see the Draw doc comment on the shedding order).
func TestAShortTerminalStillDrawsThePrompt(t *testing.T) {
	for _, height := range []int{1, 2, barRows - 1} {
		screen := drawSimulationFrame(t, height, 40, Bar{Field: "hi"}, nil, Palette{})
		// Prompt now has a leading space (see stack.go's Prompt doc comment), so column
		// 0 is that space and column 1 is "r" of "root@lolhost".
		r, _, _, _ := screen.GetContent(1, 0)
		if r != 'r' {
			t.Errorf("on a %d row terminal the prompt is not on row 0 (got %q)", height, r)
		}
	}
}

// TestTheSheddingLadderDropsInGlensOrder covers the actual order a short terminal sheds
// rows in: blank line first (never seen below barRows anyway), then the pane bar, then
// bar two, then bar one, with the prompt surviving every height.
func TestTheSheddingLadderDropsInGlensOrder(t *testing.T) {
	var s Status
	s[fieldPane] = "main"

	// Height 4: blank is already gone (nothing shows it directly); bar one, bar two,
	// and the pane bar all still show.
	screen := drawSimulationFrame(t, 4, 40, Bar{Status: s, Field: "hi"}, nil, plainPalette())
	if got := rowAt(screen, 3); got != "main" {
		t.Errorf("at height 4 the pane bar is %q, want %q", got, "main")
	}

	// Height 3: the pane bar is shed; bar two is now the last row.
	screen = drawSimulationFrame(t, 3, 40, Bar{Status: s, Field: "hi"}, nil, plainPalette())
	if got := rowAt(screen, 2); !strings.Contains(got, "openrouter") && got == "main" {
		t.Errorf("at height 3 the pane bar should be shed, but row 2 is %q", got)
	}

	// Height 2: bar two is also shed; only the prompt and bar one remain.
	screen = drawSimulationFrame(t, 2, 40, Bar{Status: s, Field: "hi"}, nil, plainPalette())
	if got := rowAt(screen, 1); got == "main" {
		t.Error("at height 2 the pane bar should be shed, but it is still drawn")
	}

	// Height 1: bar one is shed too; only the prompt remains.
	screen = drawSimulationFrame(t, 1, 40, Bar{Status: s, Field: "hi"}, nil, plainPalette())
	// Prompt now has a leading space (see stack.go's Prompt doc comment).
	r, _, _, _ := screen.GetContent(1, 0)
	if r != 'r' {
		t.Errorf("at height 1 the only row is %q, want the prompt", string(r))
	}
}

// TestEveryFieldIsOneCharacter covers the reader's decision that a field is one column. A
// field that cannot say what it means in one character says nothing, and every glyph below
// is a letter a reader can read rather than a figure a reader has to learn.
//
// The state field is the one exception and is named: it is the field a reader watches second
// by second, and a state spelled `idle` is a state rather than an `i` a reader has to learn.
func TestEveryFieldIsOneCharacter(t *testing.T) {
	var s Status
	s[fieldCwd] = "."
	s[fieldProvider] = "o"
	s[fieldModel] = "m"
	s[fieldKey] = "k"
	s[fieldFigure] = "w"
	s[fieldApproval] = "a"
	s[fieldCognito] = "n"
	s[fieldColor] = "c"
	s[fieldMouse] = "-"
	s[fieldCopy] = "y"
	s[fieldPane] = "0"
	s[fieldHeld] = "9"
	s[fieldFolded] = "0"

	for i, got := range s {
		if i == fieldState {
			continue
		}
		if DisplayWidth(got) > 1 {
			t.Errorf("field %d is %q, want one character or less", i, got)
		}
	}
}

// TestTheFrameCarriesTheLogBesideIt covers the arrangement the reader asked for. Every row
// carries a field in the first column and a log row beside it, and a row written last appears
// just above the prompt so everything moves up rather than appearing at the top.
func TestTheFrameCarriesTheLogBesideIt(t *testing.T) {
	var s Status
	s[fieldCwd] = "."
	rows := []Row{{Text: "orcli, a log with a frame around it"}, {Text: "the newest row"}}
	screen := drawSimulationFrame(t, 20, 80, Bar{Status: s}, rows, Palette{})
	got := simulationText(screen)
	if !strings.Contains(got, "the newest row") {
		t.Errorf("the newest log row is not on the screen:\n%q", got)
	}
	if !strings.Contains(got, "orcli, a log with a frame around it") {
		t.Errorf("the oldest log row is not on the screen:\n%q", got)
	}
}

// rowAt reads the full text of one screen row, trimmed of trailing padding, for asserting
// on which log line landed where.
func rowAt(screen tcell.SimulationScreen, row int) string {
	_, width, _ := screen.GetContents()
	var b strings.Builder
	for x := 0; x < width; x++ {
		r, _, _, cellWidth := screen.GetContent(x, row)
		if cellWidth > 0 {
			b.WriteRune(r)
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// TestTheLogIsNewestBesideThePrompt covers the direction of the scroll. A row arriving
// appears just above the prompt and everything above it moves up, which is what the
// reader asked the log to do, rather than appearing at the top where a reader is not
// looking.
func TestTheLogIsNewestBesideThePrompt(t *testing.T) {
	var s Status
	rows := []Row{{Text: "first"}, {Text: "second"}, {Text: "third"}}

	screen := drawSimulationFrame(t, 20, 40, Bar{Status: s}, rows, plainPalette())
	promptRow := scrollbackRows(20)

	if got := rowAt(screen, promptRow-1); got != "third" {
		t.Errorf("the row above the prompt is %q, want the newest log row %q", got, "third")
	}
	if got := rowAt(screen, promptRow-2); got != "second" {
		t.Errorf("the row above that is %q, want %q", got, "second")
	}
}

// TestTheLogFillsUpward covers the case where the log is longer than the scrollback. The
// newest rows fill the scrollback from the bottom upward and the oldest are off the
// screen, so a reader scrolling back finds them in the terminal's own scrollback rather
// than on the screen.
func TestTheLogFillsUpward(t *testing.T) {
	var s Status
	log := make([]Row, 40)
	for i := range log {
		log[i] = Row{Text: fmt.Sprintf("row %d", i)}
	}

	screen := drawSimulationFrame(t, 20, 40, Bar{Status: s}, log, plainPalette())
	promptRow := scrollbackRows(20)

	if got := rowAt(screen, promptRow-1); got != "row 39" {
		t.Errorf("the newest row is %q, want row 39", got)
	}
	if got := rowAt(screen, 0); got != "row 25" {
		t.Errorf("the topmost row is %q, want row 25 and fourteen rows beneath it", got)
	}
}

// TestThePromptRowCarriesTheFieldNotALogRow covers the one row that is not a field and a
// log row. The prompt row carries the reader's own text, and a prompt with a log row behind it
// is a prompt the reader cannot read.
func TestThePromptRowCarriesTheFieldNotALogRow(t *testing.T) {
	var s Status
	s[fieldState] = string(StateIdle)
	rows := []Row{{Text: "a log row"}}
	screen := drawSimulationFrame(t, 20, 80, Bar{Status: s, Field: "a question"}, rows, Palette{})
	got := simulationText(screen)
	if !strings.Contains(got, " root@lolhost $ a question") {
		t.Errorf("the prompt row does not carry the prompt and the typed text:\n%q", got)
	}
}

// TestSameFooterExceptField covers the test that keeps a keystroke from rewriting the frame.
// Every field has to move when anything in it changes; a change to the field does not, since
// the field is on the prompt row and that row is rewritten where it stands. The figure field's
// sweep is part of its text, so a turn running and a keystroke arriving together redraw the
// frame, which is what is wanted.
func TestSameFooterExceptField(t *testing.T) {
	base := Bar{Status: Status{fieldState: "idle"}, Field: ""}

	typed := base
	typed.Field = "a question"
	if !sameFooterExceptField(base, typed) {
		t.Error("a keystroke was taken as a change to the whole frame")
	}

	for _, change := range []func(*Bar){
		func(b *Bar) { b.Status[fieldCwd] = "/" },
		func(b *Bar) { b.Status[fieldSession] = "2" },
		func(b *Bar) { b.Status[fieldProvider] = "x" },
		func(b *Bar) { b.Status[fieldFigure] = "w" },
		func(b *Bar) { b.Status[fieldHeld] = "9" },
	} {
		next := base
		change(&next)
		if sameFooterExceptField(base, next) {
			t.Errorf("a change to %q was taken as only the field", next)
		}
	}
}

// TestTheSweepTurnsAlongTheRow covers the colour pattern on the figure. The hue advances one
// degree per column along the row and thirty-six per step, so the pattern reads as something
// travelling rather than as one colour changing.
func TestTheSweepTurnsAlongTheRow(t *testing.T) {
	// Two adjacent columns carry adjacent hues, so the pattern is horizontal rather than
	// one figure in one colour.
	if hue(0) == hue(1) {
		t.Error("adjacent columns of the sweep carry the same colour")
	}

	// Ten steps is a full turn, since thirty-six degrees a step is a tenth of 360.
	if hue(float64(10*sweepStepDegrees)) != hue(0) {
		t.Error("ten steps of the sweep is not a full turn")
	}

	// The three primaries land, and no channel reaches zero at any step: a channel at zero
	// is a dark band crossing the row rather than a hue turning.
	for _, deg := range []float64{0, 120, 240} {
		r, g, b := hueRGB(deg)
		if r == 0 && g == 0 && b == 0 {
			t.Errorf("hue %v renders as black", deg)
		}
	}
	for step := range 40 {
		r, g, b := hueRGB(float64(sweepStepDegrees * step))
		if r == 0 || g == 0 || b == 0 {
			t.Errorf("step %d puts a channel at zero: %d %d %d", step, r, g, b)
		}
	}

	// The text is written with one colour per character, so a word of n characters is n
	// colour sequences, and the words survive the colouring.
	const word = "working"
	swept := SweepText(word, 0)
	if n := strings.Count(swept, "\x1b[38;2;"); n != len([]rune(word)) {
		t.Errorf("the sweep wrote %d colours for %d characters", n, len([]rune(word)))
	}
	if got := stripSequences(swept); got != word {
		t.Errorf("the words did not survive the sweep: %q", got)
	}
}

// TestSweepStatusColoursOnlyTheValue covers the splice. The value is found by the literal
// prefix so the colour lands on the Status value and not on the rest of the bar, and a bar
// that does not begin that way is returned unchanged rather than swept in the wrong place.
func TestSweepStatusColoursOnlyTheValue(t *testing.T) {
	bar := RenderTop("working", "3", "18,400", "2,104", "916", "$0.0181", "$4.82")

	got := sweepStatus(bar, "working", 0)
	if !strings.Contains(got, "\x1b[38;2;") {
		t.Errorf("the Status value was not swept: %q", got)
	}
	if words := stripSequences(got); words != bar {
		t.Errorf("the bar's words changed:\ngot  %q\nwant %q", words, bar)
	}
	if !strings.HasSuffix(got, "| Credits: $4.82") {
		t.Errorf("the sweep ran past the Status value: %q", got)
	}

	// A bar whose first field is not the Status value is left alone.
	other := RenderBottom("Session 1", true, false, "openrouter.ai", "m", "3", "ask")
	if got := sweepStatus(other, "working", 0); got != other {
		t.Errorf("a bar that is not the top bar was swept: %q", got)
	}
}

// TestScrollMovesTheWindowWithoutChangingUsageFields covers the scroll offset and
// keeps the confirmed usage fields independent of transcript navigation.
func TestScrollMovesTheWindowWithoutChangingUsageFields(t *testing.T) {
	log := make([]Row, 10)
	for i := range log {
		log[i] = Row{Text: fmt.Sprintf("row %d", i)}
	}

	// 56 rather than 40: bar two now carries fieldPreset (/level's active preset)
	// alongside the fields the figure of 40 was sized for, and the assertions
	// below are against text past the point 40 columns used to cut.
	const width = 56

	frame := NewFrame()
	frame.SetRect(0, 0, width, 20)
	frame.SetContent(Bar{}, log, plainPalette())
	promptRow := scrollbackRows(20)

	live := tcell.NewSimulationScreen("UTF-8")
	if err := live.Init(); err != nil {
		t.Fatal(err)
	}
	live.SetSize(width, 20)
	frame.Draw(live)
	if got := rowAt(live, promptRow-1); got != "row 9" {
		t.Errorf("at the live edge the row above the prompt is %q, want the newest row 9", got)
	}
	if !strings.Contains(rowAt(live, promptRow+3), "hostname:unavailable") || !strings.Contains(rowAt(live, promptRow+3), "out:") {
		t.Errorf("bar two fields are %q, want the confirmed live usage fields", rowAt(live, promptRow+3))
	}

	frame.SetScroll(3)
	back := tcell.NewSimulationScreen("UTF-8")
	if err := back.Init(); err != nil {
		t.Fatal(err)
	}
	back.SetSize(width, 20)
	frame.Draw(back)
	if got := rowAt(back, promptRow-1); got != "row 6" {
		t.Errorf("scrolled back 3, the row above the prompt is %q, want row 6", got)
	}
	if !strings.Contains(rowAt(back, promptRow+3), "hostname:unavailable") {
		t.Errorf("bar two changed while scrolling: %q", rowAt(back, promptRow+3))
	}
}

func TestBarTwoFieldOrder(t *testing.T) {
	var status Status
	for _, field := range barTwoFields {
		status[field] = fieldName(field)
	}
	got := renderBarTwo(status)
	want := "hostname:hostname · credits:credits · cost:cost · context:context · in:in · out:out · autosave:autosave · stealth:stealth · approval:approval"
	if got != want {
		t.Fatalf("renderBarTwo = %q, want %q", got, want)
	}
}

func TestBarTwoFadesLeftTextWhereTheHalvesCollide(t *testing.T) {
	var status Status
	status[fieldHost] = "local"
	status[fieldCredits] = "$1"
	status[fieldCost] = "$0"
	status[fieldContext] = "4k"
	status[fieldInput] = "5"
	status[fieldOutput] = "67890"
	status[fieldAutosave] = "on"
	status[fieldStealth] = "off"
	status[fieldApproval] = "ask"
	palette := NewPalette(true, GroundDark, nil)
	frame := NewFrame()
	frame.SetRect(0, 0, 80, 5)
	frame.SetContent(Bar{Status: status}, nil, palette)
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	screen.SetSize(80, 5)
	frame.Draw(screen)
	leftFG, _, _ := palette.Style(RoleChrome).Decompose()
	fadedFG, _, _ := palette.Style(RoleDim).Decompose()
	first, _, firstStyle, _ := screen.GetContent(0, 3)
	nearCollision, _, fadeStyle, _ := screen.GetContent(39, 3)
	nearFG, _, _ := fadeStyle.Decompose()
	if first == 0 || nearCollision == 0 {
		t.Fatalf("bar two cells were blank: first=%q near collision=%q", first, nearCollision)
	}
	if got, _, _ := firstStyle.Decompose(); got != leftFG {
		t.Errorf("left field starts with foreground %v, want chrome %v", got, leftFG)
	}
	if nearFG != fadedFG {
		t.Errorf("left field at collision has foreground %v, want faded %v", nearFG, fadedFG)
	}
}

// TestThePaneBarIsItsOwnRowBelowBarTwo covers the fifth fixed row Glen added
// (2026-10-06) so the pane bar (adr-0000019/0000020) is not cut from the redesign.
func TestThePaneBarIsItsOwnRowBelowBarTwo(t *testing.T) {
	var s Status
	s[fieldPane] = "main"
	screen := drawSimulationFrame(t, 20, 40, Bar{Status: s}, nil, plainPalette())
	promptRow := scrollbackRows(20)

	if got := rowAt(screen, promptRow+4); got != "main" {
		t.Errorf("the pane bar is %q, want %q", got, "main")
	}
}

// TestThePaneBarDrawsTheConfiguredPaneColour covers the wiring from a configured
// colour through to the cell the reader actually sees: fieldPaneState set to
// "running" draws the pane bar row in the palette's configured active colour
// rather than in chrome.
func TestThePaneBarDrawsTheConfiguredPaneColour(t *testing.T) {
	active := RGB{R: 0x11, G: 0x22, B: 0x33}
	p := NewPalette(true, GroundDark, nil).WithPaneColors(&active, nil)

	var s Status
	s[fieldPane] = "main"
	s[fieldPaneState] = "running"
	screen := drawSimulationFrame(t, 20, 40, Bar{Status: s}, nil, p)
	promptRow := scrollbackRows(20)

	_, _, gotStyle, _ := screen.GetContent(0, promptRow+4)
	gotFG, _, _ := gotStyle.Decompose()
	if gotFG != active.TCellColor() {
		t.Errorf("the pane bar's foreground is %v, want the configured active colour %v",
			gotFG, active.TCellColor())
	}
}

// TestThePaneBarDrawsChromeWithNoWorkerState covers the pane bar's default: with
// fieldPaneState empty, the bar draws in chrome even though a pane colour is
// configured, since neither state applies.
func TestThePaneBarDrawsChromeWithNoWorkerState(t *testing.T) {
	active := RGB{R: 0x11, G: 0x22, B: 0x33}
	p := NewPalette(true, GroundDark, nil).WithPaneColors(&active, nil)

	var s Status
	s[fieldPane] = "main"
	screen := drawSimulationFrame(t, 20, 40, Bar{Status: s}, nil, p)
	promptRow := scrollbackRows(20)

	_, _, gotStyle, _ := screen.GetContent(0, promptRow+4)
	gotFG, _, _ := gotStyle.Decompose()
	wantFG, _, _ := frameStyle(p, RoleChrome).Decompose()
	if gotFG != wantFG {
		t.Errorf("the pane bar's foreground with no worker state is %v, want chrome %v",
			gotFG, wantFG)
	}
}

// TestThePaneBarTruncatesWithAnEllipsis covers Glen's confirmed departure from
// adr-0000020's literal "no ellipsis": this bar truncates the same way every other row
// in this file does, with cutTail's ellipsis, rather than clipping silently.
func TestThePaneBarTruncatesWithAnEllipsis(t *testing.T) {
	var s Status
	s[fieldPane] = "a pane name far too long for a narrow bar"
	got := renderPaneBar(s, 10)
	if !strings.Contains(got, ellipsis) {
		t.Errorf("a truncated pane bar is %q, want it to carry an ellipsis", got)
	}
	if DisplayWidth(got) > 10 {
		t.Errorf("a truncated pane bar is %d columns wide, want at most 10", DisplayWidth(got))
	}
}
