package tui

import (
	"strings"
	"testing"
)

// TestSplitRowsMakesOneRowPerLine is the rule a listing depends on. A Result.Text is one
// string and a row is one line, so a handler that answers with a listing builds it with
// newlines in it, and writing the whole thing as one row ran the entries together with the
// breaks stripped out by the filter that keeps control bytes out of a row.
func TestSplitRowsMakesOneRowPerLine(t *testing.T) {
	got := splitRows("alpha.md\nsubdir/\nzeta.txt")
	want := []string{"alpha.md", "subdir/", "zeta.txt"}

	if len(got) != len(want) {
		t.Fatalf("the listing is %d rows, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d is %q, want %q", i, got[i], want[i])
		}
	}
}

// TestSplitRowsWithNoBreakIsOneRow covers the ordinary case, since a command that answers
// with a sentence has no break in it and must not become two rows.
func TestSplitRowsWithNoBreakIsOneRow(t *testing.T) {
	got := splitRows("the provider is not set up")
	if len(got) != 1 || got[0] != "the provider is not set up" {
		t.Errorf("got %q, want one row carrying the whole sentence", got)
	}
}

// TestSplitRowsKeepsNoBlankForATrailingBreak covers the break a listing ends with. A newline
// after the last entry is how a listing is written, and a row for it would be a blank row the
// reader did not ask for.
func TestSplitRowsKeepsNoBlankForATrailingBreak(t *testing.T) {
	got := splitRows("one\ntwo\n")
	if len(got) != 2 {
		t.Errorf("got %q, want two rows and no blank for the trailing break", got)
	}
}

// TestSplitRowsKeepsABlankInTheMiddle covers the blank line that is part of a listing. The
// Cloudflare output puts one between the diff and the instructions, and dropping it would run
// the instructions onto the diff.
func TestSplitRowsKeepsABlankInTheMiddle(t *testing.T) {
	got := splitRows("the diff\n\nthe instructions")
	if len(got) != 3 {
		t.Fatalf("got %q, want three rows with a blank in the middle", got)
	}
	if got[1] != "" {
		t.Errorf("the middle row is %q, want it empty", got[1])
	}
}

// TestSplitRowsTreatsAPairOfBreaksAsOneEnd covers the one case a naive split gets wrong.
// A carriage return and a newline beside each other is how a program writing to a terminal
// ends a line, and counting them as two breaks made a blank row between every entry of a
// listing written on Windows.
func TestSplitRowsTreatsAPairOfBreaksAsOneEnd(t *testing.T) {
	got := splitRows("alpha.md\r\nsubdir/")
	if len(got) != 2 {
		t.Fatalf("got %q, want the pair of breaks to make two rows", got)
	}
	if got[0] != "alpha.md" || got[1] != "subdir/" {
		t.Errorf("got %q, want the two entries", got)
	}
}

// TestSplitRowsOnNothingIsNothing covers the empty result, since a command that answered with
// nothing must not write a row saying nothing.
func TestSplitRowsOnNothingIsNothing(t *testing.T) {
	if got := splitRows(""); len(got) != 0 {
		t.Errorf("got %q, want no rows at all", got)
	}
}

// TestAWriteResultMakesARowPerLine is the same rule at the place it is used, rather than in
// the helper alone. A command that answers with a listing has to reach the log as a listing,
// which is what makes /test worth having for a frame test: the rows it produces are one per
// entry and a reader can count them.
func TestAWriteResultMakesARowPerLine(t *testing.T) {
	s := New(Options{Model: "stealth/space-bunny-alpha"})
	before := s.Log().Len()

	l := &interfaceLoop{session: s}
	l.writeResult("alpha.md\nsubdir/\nzeta.txt")

	rows := s.Log().Rows()
	if got := len(rows) - before; got != 3 {
		t.Fatalf("the listing wrote %d rows, want 3", got)
	}
	for i, want := range []string{"alpha.md", "subdir/", "zeta.txt"} {
		if got := rows[before+i].Text; got != want {
			t.Errorf("row %d is %q, want %q", i, got, want)
		}
	}
}

// TestAWriteResultOnNothingWritesNoRow covers the empty case at the call site, since a blank
// row in the log is a row a reader scrolls past for nothing.
func TestAWriteResultOnNothingWritesNoRow(t *testing.T) {
	s := New(Options{Model: "stealth/space-bunny-alpha"})
	before := s.Log().Len()

	l := &interfaceLoop{session: s}
	l.writeResult("")

	if got := s.Log().Len(); got != before {
		t.Errorf("an empty result wrote %d rows", got-before)
	}
}

// TestARowCarriesNoBreak covers the filter and the split agreeing. A newline is stripped out
// of a row by the filter that keeps control bytes out of what is written, so the split has
// to happen before the row is written rather than after, and a row that still carried one
// would be written as a line ending the draw did not ask for.
func TestARowCarriesNoBreak(t *testing.T) {
	row := Row{Text: "alpha.md"}
	if strings.ContainsRune(PlainRow(row).Text, '\n') {
		t.Error("a row kept a newline through the filter")
	}
}
