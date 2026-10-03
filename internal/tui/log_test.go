package tui

import (
	"strings"
	"testing"
)

// TestAppendKeepsRowsInOrder checks the one property the log exists for: rows
// come out in the order they went in, because a reader comparing two tool results
// is comparing their order.
func TestAppendKeepsRowsInOrder(t *testing.T) {
	var l Log

	l.Append(Row{Text: "first"})
	l.Append(Row{Text: "second"})
	l.Append(Row{Text: "third"})

	rows := l.Rows()
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	for i, want := range []string{"first", "second", "third"} {
		if rows[i].Text != want {
			t.Errorf("row %d is %q, want %q", i, rows[i].Text, want)
		}
	}
}

// TestAppendReportsItsIndex checks that the index a caller gets back refers to
// the row it just added, since that is what a caller uses to refer to it later.
func TestAppendReportsItsIndex(t *testing.T) {
	var l Log

	for i, want := range []string{"a", "b", "c"} {
		if got := l.Append(Row{Text: want}); got != i {
			t.Errorf("append %d reported index %d, want %d", i, got, i)
		}
	}
}

// TestRowsIsACopy checks that the slice handed out is not the log's own, since a
// caller that appended to it would corrupt the transcript while the painter reads
// it.
func TestRowsIsACopy(t *testing.T) {
	var l Log
	l.Append(Row{Text: "kept"})

	rows := l.Rows()
	rows[0] = Row{Text: "changed"}

	if got := l.Rows()[0].Text; got != "kept" {
		t.Errorf("the log was changed through the slice it handed out: %q", got)
	}
}

// TestAppendPastTheBoundDropsWholeRows covers the bound, and the count that says
// so.
//
// A reader told nothing is looking at a transcript with holes in it and no way to
// know where they are, so the figure has to be reported rather than the silence
// left to be noticed.
func TestAppendPastTheBoundDropsWholeRows(t *testing.T) {
	var l Log

	for range LogBound + 10 {
		l.Append(Row{Text: "a row of the log"})
	}

	if got, want := l.Len(), LogBound; got != want {
		t.Errorf("the log holds %d rows, want %d", got, want)
	}
	if got, want := l.Folded(), 10; got != want {
		t.Errorf("Folded is %d, want %d: dropped rows have to be reported", got, want)
	}
}

// TestTruncateKeepsTheCount covers `/clear`. A command that reported nothing about
// how much it removed would read as a quiet failure.
func TestTruncateKeepsTheCount(t *testing.T) {
	var l Log
	for range 5 {
		l.Append(Row{Text: "a row"})
	}

	l.Truncate()

	if got := l.Len(); got != 0 {
		t.Errorf("the log holds %d rows after Truncate, want 0", got)
	}
	if got, want := l.Folded(), 5; got != want {
		t.Errorf("Folded is %d, want %d", got, want)
	}
}

// TestLevelIsCarriedByTheRow checks the property the gutter depends on: a row
// keeps its level after it is written, so a level that moved while the turn ran
// cannot re-attribute a row the reader has already read.
func TestLevelIsCarriedByTheRow(t *testing.T) {
	var l Log

	l.Append(Row{Text: "the main answer", Level: 0})
	l.Append(Row{Text: "a delegate answer", Level: 3})

	rows := l.Rows()
	if got, want := rows[0].Level, 0; got != want {
		t.Errorf("row 0 has level %d, want %d", got, want)
	}
	if got, want := rows[1].Level, 3; got != want {
		t.Errorf("row 1 has level %d, want %d", got, want)
	}
}

// TestSpansNameARoleRatherThanAColour checks that a row carries no escape of its
// own, which is what lets it be copied and searched without knowing about colour.
func TestSpansNameARoleRatherThanAColour(t *testing.T) {
	row := Row{
		Text:  "git status",
		Spans: []Span{{Start: 0, End: 3, Role: RoleToolGit}},
	}
	rendered := PlainRow(row)

	if rendered.Text != row.Text {
		t.Errorf("plaintext changed the row: %q", rendered.Text)
	}
	if len(rendered.Spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(rendered.Spans))
	}
	if got, want := rendered.Spans[0].Role, RoleToolGit; got != want {
		t.Errorf("span role is %d, want %d", got, want)
	}
}

// TestConcurrentAppendAndRead is the reason the log takes a lock: a turn appends
// while the painter reads, and a reader who is scrollback rather than a viewport
// makes the two concurrent by design.
//
// It is worth having under the race detector and quiet otherwise. What it guards
// is not a wrong answer but a torn read, which is a corruption rather than a
// wrong figure.
func TestConcurrentAppendAndRead(t *testing.T) {
	var l Log

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 2000 {
			l.Append(Row{Text: "a row", Level: i % 4})
		}
	}()

	for {
		select {
		case <-done:
			if got := l.Len(); got != 2000 {
				t.Errorf("the log holds %d rows, want 2000", got)
			}
			return
		default:
			for _, row := range l.Rows() {
				if row.Text != "a row" {
					t.Fatalf("a row was read torn: %q", row.Text)
				}
			}
		}
	}
}

// TestPlainRowIsIdempotent checks that a row can be passed through twice without
// changing, since the copy path and the draw path may both reach for it and a
// second pass that mangled the row would make the outcome depend on which ran.
func TestPlainRowIsIdempotent(t *testing.T) {
	const in = "a\x1b[2Jb\tc\x1b]0;t\x07d"
	once := PlainRow(Row{Text: in}).Text
	twice := PlainRow(Row{Text: once}).Text

	if once != twice {
		t.Errorf("a second pass changed the row: %q then %q", once, twice)
	}
}

// TestPlainRowKeepsTheFigures checks that the box-drawing and braille characters
// the interface draws with survive, since they are text and a reader selecting a
// row out of the log should get the figure rather than a question mark.
func TestPlainRowKeepsTheFigures(t *testing.T) {
	const row = "─ │ ⠋ ⠙ ⠹ ⠸ ⠼ ⠴ ⠦ ⠧ ⠇ ⠏ ⧉"
	if got := PlainRow(Row{Text: row}).Text; got != row {
		t.Errorf("row is %q, want %q", got, row)
	}
}

// TestSearchableTextIsThePlainText checks the property the search path rests on:
// what a reader matches against is what a reader selects and copies, so a match
// cannot report a column the text does not have.
func TestSearchableTextIsThePlainText(t *testing.T) {
	row := Row{Text: "the answer \x1b[2Jis here", Level: 2}
	plain := PlainRow(row)

	if !strings.Contains(plain.Text, "the answer is here") {
		t.Errorf("searchable text is %q, want the escape gone", plain.Text)
	}
}
