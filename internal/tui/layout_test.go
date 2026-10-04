package tui

import (
	"strings"
	"testing"
)

// TestTheStackIsFiveRows is the layout the reader settled, from the top of the stack
// downward: the rule, the prompt row, a blank, the top bar, and the bottom bar.
//
// Read from the bottom of the screen upward: the two bars, a blank, the prompt row, and
// the rule. There is no rule at the bottom, since a footer that ends in a rule is a closed
// box and this one sits against the bottom edge where the shell prompt will be when the
// program exits.
//
// The prompt row carries either the prompt and the reader's own text or the figure and
// the state word, never both.
func TestTheStackIsFiveRows(t *testing.T) {
	const width = 80
	screen, out := drawnAt(24, width)

	DrawStack(screen, Bar{
		Top:    "Status: idle",
		Bottom: "Provider: openrouter.ai",
		Field:  "please read the new preferences in foo.md",
	}, plainPalette())

	lines := stackRowsOnly(out.String())
	if got, want := len(lines), 5; got != want {
		t.Fatalf("the stack is %d rows, want %d:\n%s", got, want, out.String())
	}

	for i, want := range []struct {
		name   string
		has    string
		blank  bool
		isRule bool
	}{
		{name: "the rule", isRule: true},
		{name: "the prompt row", has: "root@localhost $ "},
		{name: "the blank row", blank: true},
		{name: "the top bar", has: "Status:"},
		{name: "the bottom bar", has: "Provider:"},
	} {
		line := lines[i]
		switch {
		case want.blank && line != "":
			t.Errorf("%s is %q, want it blank", want.name, line)
		case want.isRule && !isRuleRow(line, width):
			t.Errorf("%s is %q, want a rule across %d columns", want.name, line, width)
		case want.has != "" && !strings.Contains(line, want.has):
			t.Errorf("%s does not carry %q: %q", want.name, want.has, line)
		}
	}

	if got := lines[1]; !strings.Contains(got, "please read the new preferences") {
		t.Errorf("the prompt row does not carry what was typed: %q", got)
	}
}

// TestThePromptRowCarriesTheFigureInPlaceOfThePrompt covers the reader's decision. The
// `root@localhost $ ` text is dropped while a turn runs and the figure takes its place,
// on the same row, so a reader watching a turn start does not find their field has moved
// upwards.
func TestThePromptRowCarriesTheFigureInPlaceOfThePrompt(t *testing.T) {
	figure := strings.Repeat(" ", TwiddleIndent) + Twiddle(0) + "  working"

	screen, out := drawnAt(24, 80)
	DrawStack(screen, Bar{Field: "a question", Twiddle: figure}, plainPalette())

	lines := stackRowsOnly(out.String())
	row := lines[1]

	if strings.Contains(row, "root@localhost") {
		t.Errorf("the prompt row carries the prompt and the figure at once: %q", row)
	}
	if !strings.Contains(row, Twiddle(0)) {
		t.Errorf("the prompt row does not carry the figure: %q", row)
	}
	if !strings.Contains(row, "working") {
		t.Errorf("the prompt row does not carry the state word: %q", row)
	}
	if !strings.HasPrefix(row, strings.Repeat(" ", TwiddleIndent)) {
		t.Errorf("the figure is not indented by %d: %q", TwiddleIndent, row)
	}
}

// TestTheRuleSpansTheWidth covers the reader's decision that a rule is a divider rather
// than a left border. A rule is as wide as the terminal, counted in display columns,
// because the figure is three bytes to the column and a byte count would draw a rule a
// third of the width it should be.
func TestTheRuleSpansTheWidth(t *testing.T) {
	for _, width := range []int{1, 2, 7, 80, 130} {
		screen, out := drawnAt(24, width)
		DrawStack(screen, Bar{}, plainPalette())

		rules := 0
		for _, line := range stackRowsOnly(out.String()) {
			if !strings.HasPrefix(line, rule) {
				continue
			}
			rules++
			if got := DisplayWidth(line); got != width {
				t.Errorf("at %d columns the rule is %d wide, want %d", width, got, width)
			}
		}
		if rules != 1 {
			t.Errorf("at %d columns there are %d rules, want 1:\n%s",
				width, rules, out.String())
		}
	}
}

// TestAShortTerminalKeepsThePrompt covers the floor. A terminal too short for the whole
// stack keeps the prompt row and the blank under it, since a reader who cannot type cannot
// use the interface.
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
	if strings.Contains(got, "Provider:") {
		t.Errorf("a three row terminal kept a bar it had no room for: %q", got)
	}
}

// TestTheStackDoesNotChangeHeight covers the reason the prompt row carries the figure
// rather than a row of its own appearing. A footer that grows when a turn starts is a
// footer that moves the field under the reader's hands.
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
		Twiddle: strings.Repeat(" ", TwiddleIndent) + "  working",
	}, plainPalette())
	running := len(stackRowsOnly(other.String()))

	if idle != running {
		t.Errorf("the stack is %d rows when idle and %d while running, want the same",
			idle, running)
	}
}

// TestAKeystrokeDoesNotRewriteTheFooter covers the fault that made typing advance the
// screen. The field is part of the footer, so a footer compared whole is different on
// every keystroke, and drawing it whole each time appends another copy of itself. A change
// to the field alone has to rewrite one row.
func TestAKeystrokeDoesNotRewriteTheFooter(t *testing.T) {
	old := Bar{Top: "Status: idle", Bottom: "Session 1", Field: ""}
	next := Bar{Top: "Status: idle", Bottom: "Session 1", Field: "a"}

	if !sameFooterExceptField(old, next) {
		t.Errorf("a change to the field alone was not recognised as one")
	}

	// A change to a bar or to the figure is not a keystroke, and the rows around the
	// prompt row have to move with it.
	moved := Bar{Top: "Status: working", Bottom: "Session 1", Field: "a"}
	if sameFooterExceptField(old, moved) {
		t.Errorf("a change to the top bar was taken for a keystroke")
	}
	figured := Bar{Top: "Status: idle", Bottom: "Session 1", Twiddle: "working"}
	if sameFooterExceptField(old, figured) {
		t.Errorf("a change to the figure was taken for a keystroke")
	}
}

// TestThePromptRowIsRewrittenInPlace covers the one-row redraw a keystroke uses. It has to
// clear the row before writing it, since a question replaced by a shorter one would
// otherwise leave the tail of the old one on screen beside the new.
func TestThePromptRowIsRewrittenInPlace(t *testing.T) {
	screen, out := drawnAt(24, 80)
	palette := plainPalette()

	l := &interfaceLoop{
		session: New(Options{Model: "m"}),
		screen:  screen,
	}
	l.editor.Insert('a')
	l.drawPromptRow(Bar{Field: l.fieldRow()}, palette)

	got := out.String()
	if !strings.Contains(got, escapeEraseLine) {
		t.Errorf("the row was not cleared before being written: %q", got)
	}
	if !strings.Contains(got, "root@localhost $ a") {
		t.Errorf("the row does not carry the prompt and the typed text: %q", got)
	}

	// One line ending, since one row was written. A draw of the whole footer would
	// have written five.
	if n := strings.Count(got, "\r\n"); n != 1 {
		t.Errorf("the redraw wrote %d rows, want 1:\n%q", n, got)
	}
}

// TestTheCaretIsNotPlacedWhileATurnRuns covers the row carrying the figure. There is no
// text to type into, so a caret placed there would be a caret on a row the reader is not
// typing into, and one row off from the prompt.
func TestTheCaretIsNotPlacedWhileATurnRuns(t *testing.T) {
	screen, out := drawnAt(24, 80)

	l := &interfaceLoop{
		session: New(Options{Model: "m"}),
		screen:  screen,
	}
	l.editor.Insert('a')

	l.placeCaret(true)

	if got := out.String(); got != "" {
		t.Errorf("a caret was placed on a row carrying the figure: %q", got)
	}
}

// TestTheCaretFollowsTheTypedText covers the ordinary case, and the column arithmetic that
// puts it after the reader's own text rather than at the end of the prompt.
func TestTheCaretFollowsTheTypedText(t *testing.T) {
	screen, out := drawnAt(24, 80)

	l := &interfaceLoop{
		session: New(Options{Model: "m"}),
		screen:  screen,
	}
	l.editor.Insert('a')

	l.placeCaret(false)

	got := out.String()
	if !strings.HasPrefix(got, "\r") {
		t.Errorf("the caret did not return to the start of the row: %q", got)
	}

	// Column is the prompt plus what was typed, both measured in display columns.
	want := DisplayWidth(Prompt) + 1
	if !strings.Contains(got, "\x1b["+itoa(want)+"C") {
		t.Errorf("the caret is not after the prompt and the typed text:\n%q", got)
	}
}
