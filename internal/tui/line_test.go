package tui

import "testing"

// TestTheFieldStartsEmptyAndTheCaretAtTheStart covers the state a reader meets before
// typing, since an editor that opened with a caret at the end would put the reader in
// the middle of nothing.
func TestTheFieldStartsEmptyAndTheCaretAtTheStart(t *testing.T) {
	e := NewEditor()

	if !e.Empty() {
		t.Errorf("a new editor is not empty: %q", e.Text())
	}
	if got := e.Caret(); got != 0 {
		t.Errorf("the caret is at %d, want 0", got)
	}
}

// TestInsertIsWhereTheCaretIs covers the ordinary case. A field that appends regardless
// of where the caret is would make left and right meaningless.
func TestInsertIsWhereTheCaretIs(t *testing.T) {
	e := NewEditor()
	e.Insert('a')
	e.Insert('b')
	e.Left()
	e.Insert('c')

	if got, want := e.Text(), "acb"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := e.Caret(), 2; got != want {
		t.Errorf("the caret is at %d, want %d", got, want)
	}
}

// TestTheCaretIsCountedInRunes is the property that makes a field of anything but ASCII
// usable. A caret held as a byte offset lands inside a multi-byte character, and the
// slice bounds that follow from it are a panic rather than a wrong position.
func TestTheCaretIsCountedInRunes(t *testing.T) {
	e := NewEditor()
	for _, r := range "héllo wörld" {
		e.Insert(r)
	}

	if got, want := e.Caret(), len([]rune("héllo wörld")); got != want {
		t.Errorf("the caret is at %d, want %d", got, want)
	}

	e.Home()
	e.Right()
	if got := string([]rune(e.Text())[e.Caret()]); got != "é" {
		t.Errorf("one step from the start is %q, want the second rune", got)
	}
}

// TestBackspaceRemovesWhatIsBefore covers the ordinary case and its boundary. A caret at
// the start is a reader pressing a key that cannot mean anything there, and a field that
// lost a character it does not have is a field they have to notice and undo.
func TestBackspaceRemovesWhatIsBefore(t *testing.T) {
	e := NewEditor()
	for _, r := range "abc" {
		e.Insert(r)
	}

	e.Backspace()
	if got, want := e.Text(), "ab"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	e.Home()
	e.Backspace()
	if got, want := e.Text(), "ab"; got != want {
		t.Errorf("a backspace at the start changed the field to %q, want %q", got, want)
	}
	if got, want := e.Caret(), 0; got != want {
		t.Errorf("the caret is at %d, want %d", got, want)
	}
}

// TestDeleteRemovesWhatIsAfter is the other half, for the key that works the other way
// round.
func TestDeleteRemovesWhatIsAfter(t *testing.T) {
	e := NewEditor()
	for _, r := range "abc" {
		e.Insert(r)
	}

	e.Home()
	e.Delete()
	if got, want := e.Text(), "bc"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	e.End()
	e.Delete()
	if got, want := e.Text(), "bc"; got != want {
		t.Errorf("a delete at the end changed the field to %q, want %q", got, want)
	}
}

// TestTheArrowKeysStopAtTheEnds covers both boundaries. A caret that steps past the end
// is a caret the terminal clamps somewhere the reader cannot account for.
func TestTheArrowKeysStopAtTheEnds(t *testing.T) {
	e := NewEditor()
	for _, r := range "ab" {
		e.Insert(r)
	}

	e.Left()
	e.Left()
	e.Left()
	if got := e.Caret(); got != 0 {
		t.Errorf("three lefts from the end of two runes gave %d, want 0", got)
	}

	e.Right()
	e.Right()
	e.Right()
	if got := e.Caret(); got != 2 {
		t.Errorf("three rights from the start gave %d, want 2", got)
	}
}

// TestHomeAndEndGoToTheEnds covers the two keys a reader presses to get out of a long
// field, and they are the only way to reach either end without pressing an arrow enough
// times to count.
func TestHomeAndEndGoToTheEnds(t *testing.T) {
	e := NewEditor()
	for _, r := range "abcdef" {
		e.Insert(r)
	}

	e.Home()
	if got := e.Caret(); got != 0 {
		t.Errorf("home put the caret at %d, want 0", got)
	}

	e.End()
	if got := e.Caret(); got != 6 {
		t.Errorf("end put the caret at %d, want 6", got)
	}
}

// TestResetClearsTheFieldAndTheCaret covers what happens after a line is submitted. A
// field carrying the previous question is a field the reader has to clear by hand.
func TestResetClearsTheFieldAndTheCaret(t *testing.T) {
	e := NewEditor()
	for _, r := range "a question" {
		e.Insert(r)
	}

	e.Reset()

	if !e.Empty() {
		t.Errorf("the field is %q after a reset, want empty", e.Text())
	}
	if got := e.Caret(); got != 0 {
		t.Errorf("the caret is at %d after a reset, want 0", got)
	}
}
