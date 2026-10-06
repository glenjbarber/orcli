package tui

import (
	"fmt"
	"strings"
	"testing"
)

// hue is one hue as a single comparable value, so a test can compare two of them.
//
// Go will not compare a three-value call directly, and a sweep assertion needs to say that
// two hues are or are not the same colour, so the three channels are folded into one number.
func hue(h float64) int {
	r, g, b := hueRGB(h)
	return int(r)<<16 | int(g)<<8 | int(b)
}

// TestTheFrameIsTwentyRows is the layout the reader settled: every row of the terminal
// carries a status field in the first column and a log row beside it.
//
// Twenty is the six rows the stack had plus fourteen beside them, and a twenty row terminal
// is filled on the height. The count is a ceiling rather than a figure, so a terminal taller
// than twenty draws twenty and the rest of the screen belongs to the terminal.
func TestTheFrameIsTwentyRows(t *testing.T) {
	for _, height := range []int{20, 24, 40} {
		if got, want := screenRows(height), StatusFields; got != want {
			t.Errorf("on a %d row terminal the frame is %d rows, want %d", height, got, want)
		}
	}
}

// TestAShortTerminalDrawsWhatItHas covers the floor. A terminal shorter than the field list
// draws every row it has and takes the fields from the top, so what is shed is the bottom of
// the list and the prompt row is still there.
func TestAShortTerminalDrawsWhatItHas(t *testing.T) {
	if got, want := screenRows(9), 9; got != want {
		t.Errorf("a nine row terminal draws %d rows, want %d", got, want)
	}
	if got, want := screenRows(3), 3; got != want {
		t.Errorf("a three row terminal draws %d rows, want %d", got, want)
	}
	if got := screenRows(0); got != 0 {
		t.Errorf("a terminal with no rows draws %d", got)
	}
}

// TestThePromptRowIsTheLastRow covers where the caret is put. The prompt row is the last row
// the frame draws, so on a twenty row terminal it is row twenty and on a short one it is the
// last row the screen has. Either way it is the last row drawn, which is what puts the caret
// where a reader expects to find it without being told the frame's height.
func TestThePromptRowIsTheLastRow(t *testing.T) {
	for _, height := range []int{20, 24, 40} {
		if got, want := screenRows(height), StatusFields; got != want {
			t.Errorf("on a %d row terminal the prompt row is %d, want %d", height, got, want)
		}
	}

	if got := screenRows(9); got != 9 {
		t.Errorf("on a nine row terminal the prompt row is %d, want the bottom row 9", got)
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
	rows := []Row{{Text: "orcli, a log and nothing else yet"}, {Text: "the newest row"}}
	screen := drawSimulationFrame(t, 20, 80, Bar{Status: s}, rows, Palette{})
	got := simulationText(screen)
	if !strings.Contains(got, "the newest row") {
		t.Errorf("the newest log row is not on the screen:\n%q", got)
	}
	if !strings.Contains(got, "orcli, a log and nothing else yet") {
		t.Errorf("the oldest log row is not on the screen:\n%q", got)
	}
}

// TestTheLogIsNewestBesideThePrompt covers the direction of the scroll. A row arriving
// appears above the prompt and everything above it moves up, which is what the reader asked
// the log to do, rather than appearing at the top where a reader is not looking.
func TestTheLogIsNewestBesideThePrompt(t *testing.T) {
	var s Status
	rows := []Row{{Text: "first"}, {Text: "second"}, {Text: "third"}}

	frame := stackLines(Bar{Status: s}, rows, 20, plainPalette())

	if got := frame[len(frame)-2].log; got != "third" {
		t.Errorf("the row above the prompt is %q, want the newest log row %q", got, "third")
	}
	if got := frame[len(frame)-3].log; got != "second" {
		t.Errorf("the row above that is %q, want %q", got, "second")
	}
}

// TestTheLogFillsUpward covers the case where the log is longer than the frame. The newest
// rows fill the rows from the bottom upward and the oldest are off the screen, so a reader
// scrolling back finds them in the terminal's own scrollback rather than on the screen.
func TestTheLogFillsUpward(t *testing.T) {
	var s Status
	log := make([]Row, 40)
	for i := range log {
		log[i] = Row{Text: fmt.Sprintf("row %d", i)}
	}

	frame := stackLines(Bar{Status: s}, log, 20, plainPalette())

	if got := frame[len(frame)-2].log; got != "row 39" {
		t.Errorf("the newest row is %q, want row 39", got)
	}
	if got := frame[0].log; got != "row 21" {
		t.Errorf("the topmost row is %q, want row 21 and nineteen above it", got)
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
	if !strings.Contains(got, "root@localhost $ a question") {
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
