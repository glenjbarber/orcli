package tui

import (
	"strings"
	"testing"
)

// TestCompleteFillsTheWordBeforeTheCaret covers the ordinary case. The completer is the
// one already in the table, and its trailing space is what lets a reader type an
// argument next without pressing tab again.
//
// The prefix carries one name. Most of the table's names have an alias or share a
// first few letters with a neighbour, so a prefix that looks unambiguous is often not,
// and the ones here were picked by asking the completer rather than by reading.
func TestCompleteFillsTheWordBeforeTheCaret(t *testing.T) {
	e := NewEditor()
	for _, r := range "/colo" {
		e.Insert(r)
	}

	e.Complete()

	if got, want := e.Text(), "/color "; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := e.Caret(), len([]rune("/color ")); got != want {
		t.Errorf("the caret is at %d, want %d", got, want)
	}
}

// TestCompleteWorksOnABareName covers the two spellings the completer accepts. The
// dispatcher strips the slash and the completer puts it back, so a field holding the
// name alone has to complete too.
func TestCompleteWorksOnABareName(t *testing.T) {
	e := NewEditor()
	for _, r := range "colo" {
		e.Insert(r)
	}

	e.Complete()

	if got, want := e.Text(), "color "; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestCompleteKeepsWhatIsAfterTheWord covers a reader completing in the middle of a line.
// The argument they had already typed is theirs, and a completer that dropped it would
// be a completer taking something away.
func TestCompleteKeepsWhatIsAfterTheWord(t *testing.T) {
	e := NewEditor()
	for _, r := range "/color on here" {
		e.Insert(r)
	}
	e.Home()
	for range "/colo" {
		e.Right()
	}

	e.Complete()

	if !strings.HasSuffix(e.Text(), " on here") {
		t.Errorf("what was after the word was lost: %q", e.Text())
	}
	if strings.Count(e.Text(), "color") != 1 {
		t.Errorf("the completion duplicated what was there: %q", e.Text())
	}
}

// TestCompleteLeavesAnAmbiguousPrefixAlone covers what the completer can and cannot do.
// A prefix matching several names has no single completion, and this interface has no
// way to ask the reader which one they meant.
//
// The attribute entry is the case in point: it carries the alias attribution, so a
// prefix of seven characters matches two names and the field is left alone.
func TestCompleteLeavesAnAmbiguousPrefixAlone(t *testing.T) {
	e := NewEditor()
	for _, r := range "/attrib" {
		e.Insert(r)
	}

	e.Complete()

	if got, want := e.Text(), "/attrib"; got != want {
		t.Errorf("got %q, want %q: an ambiguous prefix was resolved", got, want)
	}
}

// TestCompleteLeavesAWordThatMatchesNothingAlone covers the reader who mistyped.
func TestCompleteLeavesAWordThatMatchesNothingAlone(t *testing.T) {
	e := NewEditor()
	for _, r := range "/zzzz" {
		e.Insert(r)
	}

	e.Complete()

	if got, want := e.Text(), "/zzzz"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestCompleteDoesNothingAtTheStart covers an empty word, which is the field before the
// reader has typed anything.
func TestCompleteDoesNothingAtTheStart(t *testing.T) {
	e := NewEditor()
	e.Complete()

	if !e.Empty() {
		t.Errorf("completing nothing produced %q", e.Text())
	}
}

// TestTheCommandTheDispatcherRunsIsInTheTable is a finding rather than an editor test.
// The dispatcher runs /cloudflare, so a reader who wants Cloudflare support has to be
// able to reach it by tab and find it in the help. It was runnable and neither, which is
// the state a command living only in the dispatcher is in.
func TestTheCommandTheDispatcherRunsIsInTheTable(t *testing.T) {
	if _, listed := Lookup("cloudflare"); !listed {
		t.Error("/cloudflare is run by the dispatcher and is not in the command table")
	}
}

// TestACompletedNameIsInTheTable is the same property from the completer's side. A name
// the completer offers that the dispatcher cannot run is a reader pressing tab and being
// given a name that then refuses.
func TestACompletedNameIsInTheTable(t *testing.T) {
	for _, name := range Names() {
		if _, found := Lookup(name); !found {
			t.Errorf("%q is offered by the completer and not in the table", name)
		}
	}
}
