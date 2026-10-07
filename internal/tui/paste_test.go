package tui

import (
	"context"
	"testing"
)

// TestSingleLinePasteInsertsPlainTextUnaffected is the regression case: a
// paste with no embedded newline carries nothing to collapse, so it must
// land exactly as plain typing would, at the caret, with no summary and no
// tracked block.
func TestSingleLinePasteInsertsPlainTextUnaffected(t *testing.T) {
	e := NewEditor()
	e.Insert('a')
	e.Insert('b')
	e.Left()

	e.InsertPastedText("XY")

	if got, want := e.Text(), "aXYb"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if e.HasPastedBlock() {
		t.Errorf("a single-line paste tracked a pasted block")
	}
	if got, want := e.SubmitText(), "aXYb"; got != want {
		t.Errorf("SubmitText() = %q, want %q", got, want)
	}
}

// TestMultiLinePasteCollapsesToASummary is the core of the fix: a paste that
// contains embedded newlines must not be treated as several lines each
// ending in Enter. It collapses to a one-line summary instead, and the real
// text is retrievable afterwards.
func TestMultiLinePasteCollapsesToASummary(t *testing.T) {
	e := NewEditor()
	e.InsertPastedText("one\ntwo\nthree")

	if got := e.Text(); got != pastedSummary(3) {
		t.Fatalf("field shows %q, want the collapsed summary %q", got, pastedSummary(3))
	}
	collapsed, ok := e.PastedBlockCollapsed()
	if !ok || !collapsed {
		t.Fatalf("PastedBlockCollapsed() = (%v, %v), want (true, true)", collapsed, ok)
	}
	if got, want := e.SubmitText(), "one\ntwo\nthree"; got != want {
		t.Errorf("SubmitText() = %q, want the real text %q", got, want)
	}
}

// TestMultiLinePasteDoesNotTriggerSubmit covers the bug directly: pasting a
// multi-line block must never reach the log as if Enter had been pressed,
// which is what the old newline-to-Enter conversion in SetPasteHandler did
// partway through a paste.
func TestMultiLinePasteDoesNotTriggerSubmit(t *testing.T) {
	s := New(Options{Model: "stealth/space-bunny-alpha"})
	before := s.Log().Len()

	// SetPasteHandler, after the fix, only ever calls InsertPastedText: it
	// no longer walks the pasted runes looking for '\r'/'\n' to turn into
	// act(ctx, KeyEnter, 0). Calling InsertPastedText directly, as the
	// handler does, and then checking the log never moved (nothing was
	// submitted) is the regression test for that removed code path.
	s.Editor().InsertPastedText("first line\nsecond line\nthird line")

	if got := s.Log().Len(); got != before {
		t.Fatalf("a multi-line paste wrote %d row(s) to the log", got-before)
	}
	if got, want := s.Editor().Text(), pastedSummary(3); got != want {
		t.Errorf("field shows %q, want the collapsed summary %q", got, want)
	}
}

// TestSubmitSendsTheRealTextNotThePlaceholder covers the submit path end to
// end: Enter on a line holding a still-collapsed block must hand whichever
// path a pasted, non-command line now reaches - ask, since submit sends a
// plain question straight to the model rather than through the runner - and
// the history, the real multi-line text, never the placeholder string.
func TestSubmitSendsTheRealTextNotThePlaceholder(t *testing.T) {
	s := New(Options{Model: "stealth/space-bunny-alpha"})
	var got string
	l := &interfaceLoop{
		session: s,
		runner: func(ctx context.Context, line string) (Result, error) {
			t.Fatalf("runner was called with %q, want a plain pasted line sent to ask instead", line)
			return Result{}, nil
		},
		ask: func(_ context.Context, question string, level int, _ bool) error {
			got = question
			return nil
		},
		group: newGroup(),
	}

	s.Editor().InsertPastedText("alpha\nbeta\ngamma")
	l.act(context.Background(), KeyEnter, 0)
	if err := l.group.Close(); err != nil {
		t.Fatalf("group.Close: %v", err)
	}

	if want := "alpha\nbeta\ngamma"; got != want {
		t.Fatalf("ask received %q, want the real text %q", got, want)
	}

	entries := s.History().Entries()
	if len(entries) == 0 || entries[0] != "alpha\nbeta\ngamma" {
		t.Errorf("history head is %q, want the real text, in %v", entries, entries)
	}
}

// TestToggleExpandPastedBlockSwapsSummaryAndRealText covers the
// expand/collapse mechanism itself, independent of which key reaches it:
// toggling shows the real text, toggling again returns to the summary, and
// the block survives the round trip so it can still be submitted correctly.
func TestToggleExpandPastedBlockSwapsSummaryAndRealText(t *testing.T) {
	e := NewEditor()
	e.InsertPastedText("one\ntwo")

	if !e.ToggleExpandPastedBlock() {
		t.Fatalf("ToggleExpandPastedBlock() = false, want true with a block present")
	}
	if got, want := e.Text(), "one\ntwo"; got != want {
		t.Fatalf("expanded text = %q, want %q", got, want)
	}
	if collapsed, ok := e.PastedBlockCollapsed(); !ok || collapsed {
		t.Fatalf("PastedBlockCollapsed() = (%v, %v), want (false, true) once expanded", collapsed, ok)
	}

	if !e.ToggleExpandPastedBlock() {
		t.Fatalf("ToggleExpandPastedBlock() = false on the way back, want true")
	}
	if got, want := e.Text(), pastedSummary(2); got != want {
		t.Fatalf("re-collapsed text = %q, want %q", got, want)
	}
	if got, want := e.SubmitText(), "one\ntwo"; got != want {
		t.Errorf("SubmitText() after a round trip = %q, want %q", got, want)
	}
}

// TestToggleExpandPastedBlockWithNoBlockDoesNothing covers the key reaching
// the toggle on an ordinary line with nothing pasted on it.
func TestToggleExpandPastedBlockWithNoBlockDoesNothing(t *testing.T) {
	e := NewEditor()
	e.Insert('x')

	if e.ToggleExpandPastedBlock() {
		t.Fatalf("ToggleExpandPastedBlock() = true with no block present")
	}
	if got, want := e.Text(), "x"; got != want {
		t.Errorf("got %q, want %q unchanged", got, want)
	}
}

// TestTypingAroundACollapsedBlockLeavesItToggleable covers pasting, then
// typing both before and after the collapsed block on the same line: the
// block is still the one that toggles, at its own (shifted) position.
func TestTypingAroundACollapsedBlockLeavesItToggleable(t *testing.T) {
	e := NewEditor()
	e.Insert('a')
	e.InsertPastedText("one\ntwo")
	e.Insert('z')

	want := "a" + pastedSummary(2) + "z"
	if got := e.Text(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	if !e.ToggleExpandPastedBlock() {
		t.Fatalf("ToggleExpandPastedBlock() = false, want true")
	}
	if got, want := e.Text(), "aone\ntwoz"; got != want {
		t.Fatalf("expanded text = %q, want %q", got, want)
	}
	if got, want := e.SubmitText(), "aone\ntwoz"; got != want {
		t.Errorf("SubmitText() = %q, want %q", got, want)
	}
}

// TestEditingInsideACollapsedBlockDropsTracking covers the escape hatch: a
// hand-edit that reaches inside the block's own runes (here, backspacing
// into the summary) must drop tracking rather than desynchronize it, and
// must leave whatever text results exactly as the edit left it.
func TestEditingInsideACollapsedBlockDropsTracking(t *testing.T) {
	e := NewEditor()
	e.InsertPastedText("one\ntwo\nthree")

	e.Backspace()

	if e.HasPastedBlock() {
		t.Errorf("a backspace into the summary left a block tracked")
	}
	want := pastedSummary(3)
	want = want[:len(want)-1]
	if got := e.Text(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestCtrlTIsWiredToToggleExpandPastedBlock covers the placeholder key
// binding: KeyCtrlT reaches the toggle through act, same as any other
// editing key. This is the mechanism being reachable, not a confirmed
// choice of key (see run.go's act, case KeyCtrlT).
func TestCtrlTIsWiredToToggleExpandPastedBlock(t *testing.T) {
	s := New(Options{Model: "stealth/space-bunny-alpha"})
	l := &interfaceLoop{session: s}

	s.Editor().InsertPastedText("one\ntwo")
	l.act(context.Background(), KeyCtrlT, 0)

	if got, want := s.Editor().Text(), "one\ntwo"; got != want {
		t.Fatalf("after KeyCtrlT, text = %q, want the expanded real text %q", got, want)
	}
}
