package tui

import (
	"strconv"
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

// TestTheStackIsSixRows is the layout the reader settled, from the top of the stack downward:
// two reserved rows, the twiddle row, the bar carrying what changes second by second, the bar
// carrying the session, and the prompt.
//
// The two leading rows carry placeholder text for now and are drawn so the rows they will
// occupy are the footer's rather than the log's.
func TestTheStackIsSixRows(t *testing.T) {
	screen, out := drawnAt(24, 80)

	DrawStack(screen, Bar{
		First:   ReservedRowText,
		Second:  ReservedRowText,
		Top:     "Status: idle",
		Bottom:  "Provider: openrouter.ai",
		Field:   "please read the new preferences in foo.md",
		Twiddle: "  working",
	}, plainPalette())

	lines := stackRowsOnly(out.String())
	if got, want := len(lines), 6; got != want {
		t.Fatalf("the stack is %d rows, want %d:\n%s", got, want, out.String())
	}

	for i, want := range []struct {
		name string
		has  string
	}{
		{name: "statusbar1", has: ReservedRowText},
		{name: "statusbar2", has: ReservedRowText},
		{name: "statusbar3", has: "working"},
		{name: "statusbar4", has: "Status:"},
		{name: "statusbar5", has: "Provider:"},
		{name: "the prompt row", has: "root@localhost $ "},
	} {
		line := lines[i]
		if want.has == "" && line != "" {
			t.Errorf("%s is %q, want it empty", want.name, line)
		}
		if want.has != "" && !strings.Contains(line, want.has) {
			t.Errorf("%s does not carry %q: %q", want.name, want.has, line)
		}
	}

	if got := lines[5]; !strings.Contains(got, "please read the new preferences") {
		t.Errorf("the prompt row does not carry what was typed: %q", got)
	}
}

// TestTheReservedRowsCarryThePlaceholder covers the two leading rows. They carry a literal
// rather than nothing, so the rows are the footer's on a reader's screen and not the log's,
// and the text reads as a placeholder so a reader does not act on it.
func TestTheReservedRowsCarryThePlaceholder(t *testing.T) {
	screen, out := drawnAt(24, 80)
	DrawStack(screen, Bar{
		First:  ReservedRowText,
		Second: ReservedRowText,
	}, plainPalette())

	lines := stackRowsOnly(out.String())
	for i, name := range []string{"statusbar1", "statusbar2"} {
		if got := lines[i]; got != ReservedRowText {
			t.Errorf("%s is %q, want the placeholder %q", name, got, ReservedRowText)
		}
	}
	if n := len(lines); n != 6 {
		t.Errorf("the stack is %d rows, want 6", n)
	}
}

// TestARowWithNothingInItIsStillDrawn covers the other half. A Bar built by hand for a test
// leaves the two rows empty, and they are drawn as empty rows rather than omitted, since a
// row that is the footer's and is drawn as part of the log is a row that scrolls.
func TestARowWithNothingInItIsStillDrawn(t *testing.T) {
	screen, out := drawnAt(24, 80)
	DrawStack(screen, Bar{}, plainPalette())

	lines := stackRowsOnly(out.String())
	for i, name := range []string{"statusbar1", "statusbar2"} {
		if lines[i] != "" {
			t.Errorf("%s is %q, want it drawn and empty", name, lines[i])
		}
	}
	if n := len(lines); n != 6 {
		t.Errorf("an empty footer is %d rows, want 6", n)
	}
}

// TestThePromptRowIsTheSixthRow covers where the caret is put. The prompt row is the last
// of the six, and the six are at the top of the screen, so the caret is on row six whatever
// the terminal's height is.
//
// It is a figure that does not depend on the height on purpose. A row named by a height is a
// row that moves when the height is read a second time, and a row that moves is a row the
// caret can end up off, which is the failure the absolute position exists to prevent.
func TestThePromptRowIsTheSixthRow(t *testing.T) {
	for _, height := range []int{24, 40, 80} {
		screen, _ := drawnAt(height, 80)

		if got := promptScreenRow(screen); got != 6 {
			t.Errorf("on a %d row terminal the prompt row is %d, want row 6", height, got)
		}
	}
}

// TestEveryFooterRowIsNamedOnce covers the arithmetic every caller shares. The footer's rows
// are the first rows of the screen in order, so the first is row one and each one follows the
// one above it.
//
// The `+1` is for counting rows from one, and it is the term that is easiest to drop: with it
// gone, every footer row is one above where it is and the caret sits on the row above the
// reader's own.
func TestEveryFooterRowIsNamedOnce(t *testing.T) {
	screen, _ := drawnAt(24, 80)
	drawn := len(stackRowsToKeep(24))

	for k := range drawn {
		got := footerScreenRow(screen, k)
		want := 1 + k
		if got != want {
			t.Errorf("footer row %d is at screen row %d, want %d", k, got, want)
		}
		if got < 1 || got > 24 {
			t.Errorf("footer row %d is at screen row %d, which is off the screen", k, got)
		}
	}

	// The rows must be consecutive and increasing, since they are written downward, and the
	// first must be row one.
	for k := 1; k < drawn; k++ {
		if footerScreenRow(screen, k) != footerScreenRow(screen, k-1)+1 {
			t.Errorf("footer row %d does not follow footer row %d", k, k-1)
		}
	}
	if got := footerScreenRow(screen, 0); got != 1 {
		t.Errorf("the first footer row is at screen row %d, want row 1", got)
	}
}

// TestTheScrollRegionLeavesTheFooterAlone covers what holds the footer in place. A log row
// long enough to scroll the screen pushes the footer up with everything else, and every
// figure the drawing code computes about where the footer is then describes a row the reader
// is no longer looking at.
func TestTheScrollRegionLeavesTheFooterAlone(t *testing.T) {
	screen, out := drawnAt(24, 80)

	got := LogRows(24)
	if got+drawnFooterRows(24) != 24 {
		t.Errorf("the log has %d rows and the footer %d, which is not the screen's 24",
			got, drawnFooterRows(24))
	}

	screen.SetScrollRegion(got)
	if want := "\x1b[1;" + itoa(got) + "r"; out.String() != want {
		t.Errorf("the scroll region is %q, want %q", out.String(), want)
	}
}

// drawnFooterRows is how many rows the footer draws at this height.
func drawnFooterRows(height int) int { return len(stackRowsToKeep(height)) }

// TestTheStackIsDrawnAtTheTop covers where the rows are put. The stack is addressed by
// position before the first line is written, since the cursor at startup is on the shell's
// last line and a stack drawn from there lands under the reader's own scrollback rather than
// at row one.
func TestTheStackIsDrawnAtTheTop(t *testing.T) {
	screen, out := drawnAt(24, 80)

	DrawStack(screen, Bar{Top: "Status: idle"}, plainPalette())

	if got := out.String(); !strings.HasPrefix(got, escapePosition(1, 1)) {
		t.Errorf("the stack does not begin at row one: %q", got)
	}
}

// TestAShortTerminalKeepsThePrompt covers the floor. A terminal too short for the whole stack
// keeps the prompt row and the session bar, since a reader who cannot type cannot use the
// interface.
//
// This is a floor rather than a ladder. Which rows are shed on the way down, and in what
// order, is the reader's decision and is not settled.
func TestAShortTerminalKeepsThePrompt(t *testing.T) {
	screen, out := drawnAt(3, 80)
	DrawStack(screen, Bar{Bottom: "Provider: -", Field: "a question"}, plainPalette())

	got := out.String()
	if !strings.Contains(got, "a question") {
		t.Errorf("a three row terminal lost the prompt: %q", got)
	}
	if strings.Contains(got, "Status:") {
		t.Errorf("a three row terminal kept a bar it had no room for: %q", got)
	}
}

// TestSameFooterExceptField covers the test that keeps a keystroke from rewriting the footer.
// The rows above the prompt row have to move when anything in them changes; a change to the
// field does not, since the field is on the prompt row and that row is rewritten where it
// stands. The twiddle row's sweep is part of its text, so a turn running and a keystroke
// arriving together redraw the footer, which is what is wanted.
func TestSameFooterExceptField(t *testing.T) {
	base := Bar{Top: "Status: idle", Bottom: "Provider: -", Field: ""}

	typed := base
	typed.Field = "a question"
	if !sameFooterExceptField(base, typed) {
		t.Error("a keystroke was taken as a change to the whole footer")
	}

	for _, change := range []func(*Bar){
		func(b *Bar) { b.First = "one" },
		func(b *Bar) { b.Second = "two" },
		func(b *Bar) { b.Top = "Status: working" },
		func(b *Bar) { b.Bottom = "Session 2" },
		func(b *Bar) { b.Twiddle = "  working" },
	} {
		next := base
		change(&next)
		if sameFooterExceptField(base, next) {
			t.Errorf("a change to %q was taken as only the field", next)
		}
	}
}

// TestDrawFooterRowRewritesOneRow covers the case that stops a keystroke advancing the
// frame. The whole footer is written as terminal rows, so drawing it again for one character
// appended the footer's height and pushed the reader's own line down the screen every time
// they typed.
func TestDrawFooterRowRewritesOneRow(t *testing.T) {
	screen, out := drawnAt(24, 80)

	DrawFooterRow(screen, rowPrompt, Bar{Field: "a question"}, plainPalette())

	got := out.String()
	if !strings.HasPrefix(got, escapePosition(promptScreenRow(screen), 1)) {
		t.Errorf("the prompt row is not reached by position:\n%q", got)
	}
	if !strings.Contains(got, escapeEraseLine) {
		t.Errorf("the prompt row did not clear itself first:\n%q", got)
	}
	if !strings.Contains(got, Prompt+"a question") {
		t.Errorf("the prompt row does not carry the prompt and the typed text:\n%q", got)
	}
	if strings.Contains(got, "\r\n") {
		t.Errorf("rewriting one row ended a line:\n%q", got)
	}
	if strings.Contains(got, "\x1b[1A") {
		t.Errorf("the prompt row was reached by moving up:\n%q", got)
	}
}

// TestTheCaretIsNotMovedBackwards covers the reader's decision. The row is addressed with a
// cursor-position sequence and the column is reached by advancing forward from it, so the
// caret never travels up through the frame to find the row it belongs to.
func TestTheCaretIsNotMovedBackwards(t *testing.T) {
	screen, out := drawnAt(24, 80)

	l := &interfaceLoop{
		session: New(Options{Model: "stealth/space-bunny-alpha"}),
		screen:  screen,
	}
	l.editor.Reset()
	l.editor.Insert('a')
	l.placeCaret()

	got := out.String()
	if strings.Contains(got, "\x1b[1A") {
		t.Errorf("the caret was moved up through the frame:\n%q", got)
	}
	if !strings.HasPrefix(got, escapePosition(promptScreenRow(screen), 1)) {
		t.Errorf("the caret is not placed on the prompt row by position:\n%q", got)
	}
	if !strings.Contains(got, "\x1b[") || !strings.Contains(got, "C") {
		t.Errorf("the caret column is not reached by advancing forward:\n%q", got)
	}
}

// TestTheCaretFollowsThePromptAndTheText covers the column. The caret is after the prompt
// and after what the reader typed, counted in display columns, so a field holding a wide
// character does not put the caret inside a glyph.
func TestTheCaretFollowsThePromptAndTheText(t *testing.T) {
	screen, out := drawnAt(24, 80)

	l := &interfaceLoop{
		session: New(Options{Model: "stealth/space-bunny-alpha"}),
		screen:  screen,
	}
	l.editor.Reset()
	l.editor.Insert('a')
	l.placeCaret()

	want := escapePosition(promptScreenRow(screen), 1) +
		"\r\x1b[" + itoa(DisplayWidth(Prompt)+1) + "C"
	if got := out.String(); got != want {
		t.Errorf("the caret is not after the prompt and the typed text:\ngot  %q\nwant %q",
			got, want)
	}
}

// TestTheSweepTurnsAlongTheRow covers the colour pattern the reader asked for on the Status
// value. The hue advances one degree per column along the row and thirty-six per step, so the
// pattern reads as something travelling rather than as one colour changing.
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

// itoa is here so a test can name a row without importing strconv for one call.
func itoa(n int) string { return strconv.Itoa(n) }
