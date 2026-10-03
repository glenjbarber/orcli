package tui

import (
	"strings"
	"testing"
)

// TestDisplayWidthCountsColumnsAndNotBytes is the property that makes this file
// exist. A box-drawing rule is three bytes and one column, so a byte count reports the
// divider this interface draws three times too wide, and a layout built on it wraps a
// row that was exactly as wide as the terminal.
func TestDisplayWidthCountsColumnsAndNotBytes(t *testing.T) {
	const rule = "────────────────────────────────────────"

	if got := DisplayWidth(rule); got != len([]rune(rule)) {
		t.Errorf("a rule of %d columns measured %d", len([]rune(rule)), got)
	}
}

// TestDisplayWidthCountsAKanjiAsTwo checks the case a rune count gets wrong. A
// terminal gives an East Asian wide character two columns, so a line of Japanese
// measures half again as long as its rune count says and a fold built on runes cuts it
// in the wrong place.
func TestDisplayWidthCountsAKanjiAsTwo(t *testing.T) {
	if got, want := DisplayWidth("世界"), 4; got != want {
		t.Errorf("two wide characters measured %d columns, want %d", got, want)
	}
	if got, want := DisplayWidth("a世b"), 4; got != want {
		t.Errorf("a mixed string measured %d columns, want %d", got, want)
	}
}

// TestDisplayWidthCountsAMarkAsZero covers the other direction. A mark is drawn on top of
// the character before it, so counting it as a column pushes every column after it one
// to the right.
//
// The string is built from runes rather than typed as a literal, because a literal
// written with a precomposed accent is one rune measuring one column and the test would
// pass without ever reaching the mark table.
func TestDisplayWidthCountsAMarkAsZero(t *testing.T) {
	marked := "e" + string(rune(0x301)) // e with a combining acute accent.

	if got, want := DisplayWidth(marked), 1; got != want {
		t.Errorf("e with a combining acute measured %d columns, want %d", got, want)
	}
	// a, then the marked e, then b: three drawn characters, three columns.
	if got, want := DisplayWidth("a"+marked+"b"), 3; got != want {
		t.Errorf("a marked string measured %d columns, want %d", got, want)
	}
}

// TestDisplayWidthIsZeroForTheEmptyString covers the boundary a fold divides by. A
// width of zero for nothing is what lets a caller write budget minus used without having
// to ask whether anything was written.
func TestDisplayWidthIsZeroForTheEmptyString(t *testing.T) {
	if got := DisplayWidth(""); got != 0 {
		t.Errorf("the empty string measured %d columns, want 0", got)
	}
}

// TestTablesAreAscendingAndDisjoint guards the early return in the scan. The scan stops
// at the first range whose start is above the rune, which is only correct while a table
// is in order and does not overlap, and a table edited by hand drifts.
func TestTablesAreAscendingAndDisjoint(t *testing.T) {
	for name, table := range map[string][][2]rune{
		"markRanges": markRanges,
		"wideRanges": wideRanges,
	} {
		for i := 1; i < len(table); i++ {
			prev, cur := table[i-1], table[i]
			if cur[0] <= prev[1] {
				t.Errorf("%s: range %d starts at %X, which overlaps or precedes the one before it at %X",
					name, i, cur[0], prev[0])
			}
		}
	}
}

// TestIsWideAgreesWithItsOwnTable checks the scan against the table it scans, so a rune
// on a boundary is not accepted or refused by accident.
//
// The rune outside each range is only checked where it is outside every range, which is
// the gap between one range's end and the next range's start. Where two ranges are
// adjacent the boundary belongs to the later one and checking it against the earlier
// would report a table that is correct.
func TestIsWideAgreesWithItsOwnTable(t *testing.T) {
	for _, span := range wideRanges {
		if !isWide(span[0]) || !isWide(span[1]) {
			t.Errorf("a range end in [%X, %X] was refused", span[0], span[1])
		}
	}

	for i := 1; i < len(wideRanges); i++ {
		gap := wideRanges[i-1][1] + 1
		next := wideRanges[i][0]
		if gap < next && isWide(gap) {
			t.Errorf("%X is between two ranges and isWide says yes", gap)
		}
	}

	// Outside the whole table.
	if isWide(0) || isWide('a') || isWide('-') || isWide(' ') {
		t.Error("a rune outside every range was reported as wide")
	}
}

// TestCutColumnLeavesAWholeStringInside covers the case a caller does not have to ask
// about. A string that fits comes back whole with nothing reported dropped, so the
// caller can report a count without checking whether there was one.
func TestCutColumnLeavesAWholeStringInside(t *testing.T) {
	got, dropped := CutColumn("short", 40)
	if got != "short" {
		t.Errorf("got %q, want the string whole", got)
	}
	if dropped != 0 {
		t.Errorf("dropped %d columns from a string that fits, want 0", dropped)
	}
}

// TestCutColumnCutsAtARuneBoundary is the property that keeps a replacement character off
// the reader's screen. A rune cut in half is what a byte-wise cut produces, and the copy
// path would carry those bytes into whatever the reader pasted them into.
func TestCutColumnCutsAtARuneBoundary(t *testing.T) {
	const wide = "世界世界世界"

	// A five column budget cannot hold three wide characters, so the cut lands before
	// the third.
	got, dropped := CutColumn(wide, 5)
	if got != "世界" {
		t.Errorf("cut to 5 columns gave %q, want two wide characters", got)
	}
	if want := DisplayWidth(wide) - DisplayWidth(got); dropped != want {
		t.Errorf("dropped %d columns, want %d", dropped, want)
	}

	for _, r := range got {
		if r == '�' {
			t.Fatal("the cut row carries a replacement character")
		}
	}
}

// TestCutColumnOnNothingLeavesNothing covers a zero budget, which is what a terminal
// narrower than its own gutter produces. Returning the width rather than zero tells the
// caller how much was there.
func TestCutColumnOnNothingLeavesNothing(t *testing.T) {
	got, dropped := CutColumn("anything", 0)
	if got != "" {
		t.Errorf("got %q for a zero budget, want nothing", got)
	}
	if dropped == 0 {
		t.Error("a zero budget reported nothing dropped, want the full width")
	}
}

// TestCutColumnFromEndKeepsTheTail covers the reason this is not the other way round.
// The case is a tool line whose end is the command it ran, and the beginning of a long
// path is the same directory as the beginning of every other row.
func TestCutColumnFromEndKeepsTheTail(t *testing.T) {
	const in = "internal/tools/exec.go and a great deal more after it"

	got := CutColumnFromEnd(in, 24)
	if !strings.HasPrefix(got, ellipsis) {
		t.Errorf("the cut row %q does not say it was cut", got)
	}
	if !strings.Contains(got, "after it") {
		t.Errorf("the cut row %q kept the head rather than the tail", got)
	}
	if gotWidth := DisplayWidth(got); gotWidth > 24 {
		t.Errorf("the cut row is %d columns, over the 24 budget: %q", gotWidth, got)
	}
}

// TestCutColumnFromEndFillsTheBudget is the case a wide character exposes.
//
// A wide character one column too wide for the space left would otherwise leave a gap, and
// a row five columns wide in a six column budget reads as broken rather than tight. One
// column of slack is allowed, since that is the width of a wide character that would not
// fit.
func TestCutColumnFromEndFillsTheBudget(t *testing.T) {
	const wide = "世界世界世界世界世界"

	for columns := 1; columns <= 12; columns++ {
		got := CutColumnFromEnd(wide, columns)
		gotWidth := DisplayWidth(got)

		if gotWidth > columns {
			t.Errorf("at %d columns the cut row is %d columns, over budget: %q",
				columns, gotWidth, got)
		}
		if columns > displayWidth(ellipsis) && gotWidth < columns-1 {
			t.Errorf("at %d columns the cut row is %d columns, leaving a gap: %q",
				columns, gotWidth, got)
		}
	}
}

// TestCutColumnFromEndLeavesAFittingStringUnmarked is the case that makes the marker
// mean something. A row that was not cut must not carry one, or every row in the log
// looks like every other row.
func TestCutColumnFromEndLeavesAFittingStringUnmarked(t *testing.T) {
	const in = "short"

	if got := CutColumnFromEnd(in, 40); got != in {
		t.Errorf("got %q, want the string whole and unmarked", got)
	}
}

// TestCutColumnFromEndWithNoRoomForContent covers a budget one column wide. The marker
// alone is what fits, and it is the part that carries the meaning: the reader learns the
// row was cut even when there is no room to show what.
func TestCutColumnFromEndWithNoRoomForContent(t *testing.T) {
	if got, want := CutColumnFromEnd("anything at all", 1), ellipsis; got != want {
		t.Errorf("got %q for a one column budget, want %q", got, want)
	}
	if got, want := CutColumnFromEnd("anything at all", 0), ""; got != want {
		t.Errorf("got %q for a zero budget, want %q", got, want)
	}
}

// TestFitLeavesRoomForTheGutter covers the reason a row is not folded to the full width.
// The level and the copy handle lead every row, so a row folded to the terminal width
// would push its own markers off the right edge.
func TestFitLeavesRoomForTheGutter(t *testing.T) {
	if got, want := fit(80, 4), 76; got != want {
		t.Errorf("fit(80, 4) = %d, want %d", got, want)
	}
	if got, want := fit(80, 0), 80; got != want {
		t.Errorf("fit(80, 0) = %d, want %d", got, want)
	}
}

// TestFitOnATerminalNarrowerThanItsGutter covers the degenerate case. One column is still
// worth drawing, since a row with no text is a blank line and a blank line in a log
// reads as a gap rather than as content.
func TestFitOnATerminalNarrowerThanItsGutter(t *testing.T) {
	for _, c := range [][2]int{{2, 4}, {4, 4}, {0, 4}, {3, 4}} {
		if got := fit(c[0], c[1]); got != 1 {
			t.Errorf("fit(%d, %d) = %d, want 1", c[0], c[1], got)
		}
	}
}
